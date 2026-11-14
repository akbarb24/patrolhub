# Let Neon scale to zero

## Why

Neon free tier gives 100 compute hours a month. On 2026-11-14 PatrolHub had used 86.3:
the database never sleeps, because two in-process timers query it every 5 minutes and
Neon suspends only after 5 minutes without activity. Always-on at 0.25 CU is ~180 h/month,
so the quota runs out around 2026-11-17 and the database is suspended until the month resets.

## Goal / non-goals

- Goal: the database is awake only for real use (report window, admin, recap views) plus
  a short pass after each window close. Target well under 100 h/month.
- Non-goal: changing providers, Neon plan, Fly config, or any business rule.
- Anti-goal: a night that never closes, or a WhatsApp report that is never sent.

## Approaches

- **A. Let Neon sleep (chosen):** stop the background timers from polling every 5 minutes.
- B. Move to Supabase free Postgres: no compute-hour cap, but a migration, a new account,
  500 MB limit, and the waste stays.
- Rejected: Postgres on Fly (not free, self-run backups); paid Neon (not free).

## Design

1. **Closer sleeps until the next window close.** `ClosingService.Tick` also returns the
   next due time: the next `domain.ClosesAt(...)` after `now` for the loaded
   `window_close`, plus 1 minute. `job.Closer` sleeps until that time, capped at
   **6 hours** (so a changed `window_close`, a not-yet-set `go_live_date`, or a failed
   WhatsApp send is picked up within 6 h). It still ticks once at start-up.
   Effect: ~4 short wake-ups a day instead of 288.
   A tick that returns an error (DB hiccup, WhatsApp gateway down) retries after
   **30 minutes**, so a failed close or report is not delayed by 6 h. Report errors stop
   once the night is older than 24 h, so a long gateway outage cannot poll forever.
2. **Session cleanup once a day:** `pgxstore.NewWithCleanupInterval(pool, 24*time.Hour)`
   in `httpx/session.go`. Expired sessions are already rejected on read; cleanup only
   trims the table.
3. **Drop idle DB connections quickly:** `MaxConnIdleTime = 1 minute` on the `serve`
   pool (`pgxpool.ParseConfig` in `cmd/patrolhub/main.go`), so an idle open connection
   cannot keep Neon awake. Cost: a reconnect after idle, which a cold Neon start needs anyway.

Retention (daily) and `/healthz` (no DB) stay as they are.

## Accepted trade-offs

- Nights close a minute or so after `window_close` (was: within 5 minutes).
- If an admin moves `window_close` earlier within the closer's last sleep before the old
  close time, that one night closes at the old time + 1 min instead of the new one: late by
  (old − new), never more than 6 h. Reporters are unaffected (the page reads the window
  live and locks at the new time); only the closing, fines, recap row and WhatsApp report
  wait. Moving it later, or changing it earlier in the day, has no delay.
- The first request after the database slept waits for Neon's cold start (~0.5–1 s).

## Left to the builder

- Exact shape of the "next due" return value and a table-driven test for it (pure logic
  belongs in `internal/domain`).
- Verify on the Neon dashboard after deploy that the compute shows idle/suspended periods.
- Crawlers hitting the public recap can still wake the database; not addressed here.
