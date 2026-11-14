# Let Neon Scale to Zero Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop PatrolHub from querying Neon every 5 minutes so the database can suspend between real use.

**Architecture:** A pure `domain.NextCloserRun` computes the next close-time run; `ClosingService.Tick` returns it in `TickResult.Next`; `job.Closer` sleeps until then (capped at 6 h, 30 min after a failed tick) instead of a 5-minute ticker. Session cleanup becomes daily and the `serve` pool drops idle connections after 1 minute.

**Tech Stack:** Go, pgx/pgxpool, scs/pgxstore. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-11-14-neon-scale-to-zero-design.md`

## Global Constraints

- No new dependencies; no migration; no business-rule change.
- Business logic stays pure in `internal/domain` (current time is a parameter, no `time.Now()`), table-driven tests.
- Date math in `Asia/Jakarta` (`config.Jakarta`).
- `make check` must pass before each commit.
- Values: closer runs at window close **+1 minute**; max sleep **6 hours**; retry after a failed tick **30 minutes**; session cleanup **24 hours**; pool `MaxConnIdleTime` **1 minute**.

## Review Focus

- `now` exactly at close+1 min: the next run must be the *following* day, not `now` (else a hot loop that keeps Neon awake). → Task 1 test case.
- `now` just before close+1 min (e.g. 06:00:30 with close 06:00): next run is today 06:01. → Task 1 test case.
- Closer sleeping a zero/negative duration (Next in the past, or zero `Next` when settings failed to load): must not spin. → Task 2 test case.
- A failing WhatsApp gateway must retry within 30 min, not 6 h (reports older than 24 h are dropped). → Task 2 test case.
- `window_close` after midnight vs. the current time already past it today (e.g. now 23:00, close 06:00 → tomorrow 06:01). → Task 1 test case.

---

### Task 1: `domain.NextCloserRun` and `TickResult.Next`

**Files:**
- Modify: `internal/domain/window.go` (add function after `ClosesAt`)
- Test: `internal/domain/window_test.go`
- Modify: `internal/service/closing.go` (`TickResult`, `Tick`)
- Test: `internal/service/closing_test.go`

**Interfaces:**
- Produces: `func NextCloserRun(now time.Time, closeAt Clock) time.Time` — the first instant strictly after `now` that is `closeAt` + 1 minute in Jakarta.
- Produces: `TickResult.Next time.Time` — set from `NextCloserRun(now, set.WindowClose)` right after settings load succeed (also when `go_live_date` is nil); zero when settings failed to load.

- [ ] **Step 1: Write the failing domain test** `TestNextCloserRun`, table-driven, close `Clock{6, 0}`, all times in `config.Jakarta` on 2026-11-14:

| now | want |
|---|---|
| 05:00 | 2026-11-14 06:01 |
| 06:00:30 | 2026-11-14 06:01 |
| 06:01 (exactly) | 2026-11-15 06:01 |
| 12:00 | 2026-11-15 06:01 |
| 23:00 | 2026-11-15 06:01 |
| 05:00 given as UTC (`22:00` UTC on 11-13) | 2026-11-14 06:01 Jakarta |

Compare with `got.Equal(want)`.

- [ ] **Step 2: Run** `go test ./internal/domain -run TestNextCloserRun` — Expected: FAIL (undefined: NextCloserRun).

- [ ] **Step 3: Implement `NextCloserRun`** in `window.go`: candidate = `DateOf(now).At(closeAt.Hour, closeAt.Min).Add(time.Minute)`; if `!candidate.After(now)`, use `DateOf(now).AddDays(1)` instead. Doc comment says why +1 minute (close boundary is inclusive in `ended`).

- [ ] **Step 4: Run** `go test ./internal/domain -run TestNextCloserRun` — Expected: PASS.

- [ ] **Step 5: Add `Next` to `TickResult` and set it in `Tick`.** Add to an existing closer test (e.g. `TestCloseCompleteReport`, using `afterWed` = 2026-10-08 06:05): `res.Next` equals `time.Date(2026, 10, 9, 6, 1, 0, 0, testNow.Location())`. Also in `newCloseEnv(t, "")` (not live) case if one exists, assert `Next` is non-zero; otherwise add a 3-line test `TestTickNotLiveStillSchedules`.

- [ ] **Step 6: Run** `set -a; . ./.env.example; set +a; go test ./internal/service -run 'TestClose|TestTick'` — Expected: PASS (needs the dev Postgres: `docker compose up -d`).

- [ ] **Step 7: Commit**

```bash
git add internal/domain/window.go internal/domain/window_test.go internal/service/closing.go internal/service/closing_test.go
git commit -m "feat: closer reports when it next needs to run"
```

### Task 2: `job.Closer` sleeps until the next run

**Files:**
- Modify: `internal/job/closer.go`
- Create: `internal/job/closer_test.go`

**Interfaces:**
- Consumes: `TickResult.Next` (Task 1).
- Produces: `Closer.MaxSleep time.Duration` (replaces `Interval`; default 6 h). `Closer.Interval` is removed — `main.go` never sets it, so no caller changes.
- Produces (unexported): `func nextSleep(now, next time.Time, failed bool, max time.Duration) time.Duration`.

- [ ] **Step 1: Write the failing test** `TestNextSleep`, table-driven, `max = 6h`, `now = 2026-11-14 12:00 Jakarta`:

| next | failed | want |
|---|---|---|
| now+2h | false | 2h |
| now+20h | false | 6h |
| zero `time.Time{}` | false | 6h |
| now−1m (past) | false | 1m (floor, never ≤0) |
| now+2h | true | 30m |
| now+10m | true | 10m (whichever is sooner) |

- [ ] **Step 2: Run** `go test ./internal/job -run TestNextSleep` — Expected: FAIL (undefined: nextSleep).

- [ ] **Step 3: Implement** `nextSleep` (consts `retryAfter = 30 * time.Minute`, `minSleep = time.Minute`): start with `max`; if `next` non-zero and sooner, use `next.Sub(now)`; if `failed`, cap at `retryAfter`; floor at `minSleep`. Change `tick` to return `failed bool` (true if either `Service.Tick` or `Reports.Tick` returned an error, keep the existing `slog.Error` lines) and the `TickResult`. Replace the ticker in `Run` with a `time.NewTimer(nextSleep(...))` loop; `select` on `ctx.Done()` stops the timer and returns. Keep the immediate first tick. Update the type comment ("every five minutes" → "after each window close, at least every MaxSleep").

- [ ] **Step 4: Run** `go test ./internal/job` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/job/closer.go internal/job/closer_test.go
git commit -m "feat: closer sleeps until the next window close"
```

### Task 3: Daily session cleanup, short pool idle time, docs

**Files:**
- Modify: `internal/httpx/session.go:17`
- Modify: `cmd/patrolhub/main.go:118` (the `serve` pool only; `seed.go`/`admin.go` are one-shot commands)
- Modify: `CLAUDE.md`, `fly.toml` comment on the closer interval

- [ ] **Step 1:** `sm.Store = pgxstore.NewWithCleanupInterval(pool, 24*time.Hour)` with a one-line comment: frequent cleanup would keep Neon from suspending.

- [ ] **Step 2:** Replace `pgxpool.New` in `serve` with `pgxpool.ParseConfig(cfg.DatabaseURL)` → `pc.MaxConnIdleTime = time.Minute` (comment: idle connections must not keep Neon awake) → `pgxpool.NewWithConfig(ctx, pc)`. Return the parse error unchanged.

- [ ] **Step 3: Update docs.** `CLAUDE.md`: in the Closer bullets replace "every 5 min" with "after each window close (+1 min), at least every 6 h, 30 min after a failure", and add one line under Fly.io: Neon free tier is 100 CU-hours/month; nothing in-process may query the DB on a short timer (closer, session cleanup, pool idle time), so the compute can suspend. `fly.toml`: comment "The closer (every 5 minutes)" → "The closer (after each window close)".

- [ ] **Step 4: Run** `set -a; . ./.env.example; set +a; make check` — Expected: exits 0, no test failures; `git status` shows no change to `web/static/css/app.css`.

- [ ] **Step 5: Commit**

```bash
git add internal/httpx/session.go cmd/patrolhub/main.go CLAUDE.md fly.toml
git commit -m "feat: let Neon suspend between real use"
```

### After deploy (manual, owner)

- `make deploy`, then in the Neon console check the compute shows *Idle/Suspended* within ~10 minutes of quiet, and watch compute hours over the next day (expect a few hours/day at most).
