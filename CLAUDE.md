# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

PatrolHub replaces an Excel sheet for a single RT's (neighborhood unit) night-patrol ("ronda") attendance: a QR-linked report page used at the guard post, a public monthly recap for residents, and automatic fines. MVP scope is one RT, one post, one or more admins; no multi-tenant, member login, native app, notifications, online payment, or incident reports (those are phase 2).

Two docs drive the work (both Indonesian, both in the repo root):

- `PRD — PatrolHub (MVP Aplikasi Pencatatan Ronda).md` — requirements, IDs `F1.1`–`F5.5`, each Must or Should, plus business rules and decisions.
- `PatrolHub-Implementation-Plan.md` — 14-step build guide with a Checkpoint per step. Requirement IDs in it refer to the PRD.

If they conflict, the PRD wins: note the difference and ask the product owner. When unsure about a business rule, stop and ask rather than guess.

**Progress:** Steps 1–6 are done and committed (setup, schema, domain, router/layouts, admin login/settings/QR, members + schedule + `seed-demo`). Next is Step 7 (public report page). `/recap` and `/admin/nights` are still "Segera hadir" placeholders.

## Working rules

- Code, file names, tables, routes, comments in **English**; all user-facing text in **Indonesian**.
- Only use libraries in the plan's Stack table; no new dependencies without approval.
- One step = one commit, message `step-<n>: <summary>` (e.g. `step-4.2: add patrol window logic`). A step is done only when `make check` passes and its Checkpoint items are met. Work steps in order.
- Never edit a committed migration; schema changes after Step 2 need a new migration file.
- Business logic (patrol window, night team, fines, monthly recap) lives **only** in `internal/domain/` as pure functions: no DB, no HTTP, no `time.Now()` — the current time is a parameter. Table-driven tests required (≥85% coverage).
- All date math uses `Asia/Jakarta` (`config.Jakarta`; `time/tzdata` is embedded), never the server timezone.
- Money is `INTEGER` rupiah, displayed `Rp 5.000`.
- A member displays as `Nama (Blok)`, or just the block (e.g. `C-10`) when name is empty. Blocks are stored upper-cased and trimmed.

## Commands

Tools (`templ`, `sqlc`, `air`, `tailwindcss`) are installed into the git-ignored `./bin`; the Makefile prepends it to `PATH`, so use `make` rather than calling them bare. The Tailwind binary and daisyUI plugin files (`web/styles/daisyui*.mjs`) are vendored/downloaded, not Go dependencies.

```
docker compose up -d     # dev Postgres (patrolhub/patrolhub on :5432)
make generate            # templ generate + sqlc generate (sqlc skipped if no sqlc.yaml)
make css                 # Tailwind build -> web/static/css/app.css
make check               # generate, go vet ./..., go test ./...   (must pass before every commit)
make dev                 # air live reload (runs `serve`)
make build               # generate + css + CGO_ENABLED=0 build -> bin/patrolhub

go test ./internal/domain -run TestCurrentWindow   # single test
go test ./internal/domain/... -cover
```

The binary reads config from environment only (no `.env` loader). For local runs: `set -a; . ./.env.example; set +a`. Subcommands: `patrolhub serve` (auto-migrates, then HTTP; background jobs to be added), `migrate`, `create-admin <username>`, `seed-demo` (only when `APP_ENV=development`). `DATABASE_URL` and `BASE_URL` are required.

Generated code (`internal/db/*`, `*_templ.go`) is not hand-edited; `*_templ.go` is git-ignored, `internal/db` is committed. Migrations live in `migrations/` and are embedded via `migrations/embed.go` (`//go:embed *`).

## Architecture

Layering: **handler** (`internal/httpx`) → **service** (`internal/service`: orchestrates domain + db + storage, transactions via `WithTx`) → **repository** (`internal/db`, sqlc output). Handlers never call `db` directly. Views are `.templ` in `internal/view`; static assets embedded in `web/` and served at `/static/`. Public/admin split: public pages are mobile-first (≤480px), admin has its own layout.

Concepts that span several files:

- **Patrol night** is keyed by `patrol_date` = the date the night *starts*. The window runs `window_open` (22:00) on D to `window_close` (06:00) on D+1, so a report at 00:30 belongs to the *previous* date (`domain.CurrentWindow`). Outside the window the page is read-only and shows the upcoming team.
- **Night team** = `schedule_rule` (weekday, `effective_from`/`effective_to`; members are removed by setting `effective_to`, never by deleting) + `schedule_override` (ADD/REMOVE/SWAP, applied in order) via `domain.ResolveTeam`. Attendance rows are synced lazily on page view/write; `PRESENT` and `WALK_IN` rows are never auto-deleted. Schedule changes never alter closed nights.
- **Anonymous reporting** (`/patrol/tonight`, no login, 60 req/min/IP) writes attendance, situation note (≤1000 chars), photos (client-side resized to ~1280px/<500KB; server accepts ≤1MB JPEG/WebP, 3 min / 10 max), and rain requests. Anyone with the link can edit until close; every write goes to `audit_log` with IP/UA, `actor='ANONYMOUS'`. Convention: audit rows on child entities carry `night_id` in `detail` so `ListAuditByNight` can find them.
- **Closer job** (every 5 min, idempotent): backfills missing nights from `go_live_date` (looking back ≤14 days), then closes nights past window close in a transaction with `GetNightByDateForUpdate`. `domain.CloseNight` rules: `RAINED_OUT` → all `RAINED_OUT`, no fines; `RAIN_REQUESTED` → left pending, no fines, `needs_review`; no report → `NO_REPORT`, everyone fined; report with fewer than `min_photos` → `REPORTED` + `needs_review`; else `REPORTED`, fine only `ABSENT`. The fine rate is frozen in `patrol_night.fine_rate` at close, so tariff changes never touch past months.
- **Admin** (session login, bcrypt cost 12): confirm/reject rain (reject on a closed night recalculates fines from the frozen rate), mark rain before/after close, correct attendance (reason ≥5 chars, audited as `admin:<id>`), record fine payments per member per `YYYY-MM`, CSV export, printable A5 QR.
- **Public recap** `/recap/{yyyy}/{mm}` (+ `/situations`) is built only from closed nights (`domain.BuildMonthRecap`); `RAIN_REQUESTED` counts as reported until confirmed; future months 404. Recap and fines use the same calendar month. It must never expose phone numbers or photos. Fines are billed the following month, but billing is not shown in the recap.
- Photos are served only to admins (`/admin/photos/{id}`), except the reporter's thumbnails while that night is still open. Retention job deletes photos older than 183 days.
- No "izin" (excused) status in the MVP: absent without rain is always fined.

## Schema notes

`migrations/00001_init.sql` is copied verbatim from the plan and has more than the PRD's eight tables: `sessions` (scs/pgxstore), `setting` (7 seeded keys incl. `go_live_date`), plus `fine_rate` and `rain_requested_at` on `patrol_night`. `schedule_override` has a CHECK that `replacement_member_id` is set iff `action='SWAP'`. sqlc query files in `queries/` are one per table; `CloseNight` takes an explicit `closed_at` (the plan's signature omitted it) so the job stays deterministic and testable.

## HTTP layer notes

- `httpx.NewRouter(Deps)` middleware order: RequestID, RealIP (only if `TRUST_PROXY`), Logger, Recoverer, Compress(5), scs `LoadAndSave`, nosurf. nosurf runs before routing, so any unsafe request without a valid token gets 403 even on unknown paths.
- nosurf 1.2 also requires proof of same origin (`Sec-Fetch-Site`, `Origin` or `Referer`). The origin of `BASE_URL` is whitelisted because behind a TLS-terminating proxy the app sees plain http; `config.Load` therefore requires `BASE_URL` to be an absolute http(s) URL.
- Layouts get the CSRF token and RT name from the request context (`view.PageData`, set by the `pageData` middleware on page routes), not from template arguments. HTMX gets the token via `hx-headers` on `<body>`; plain forms must include a hidden `csrf_token` field.
- `SettingsLoader` (interface in `httpx`) lets router tests use a fake instead of Postgres. `service.Settings.Validate` returns `ValidationErrors` keyed by setting key (= form field name). `TestSettingsServiceRoundTrip` runs only when `DATABASE_URL` is set and restores the original settings.
- The admin nav links to `/admin/fines` (no month); that route must redirect to the current month when Step 9.4 adds it.
- Admin routes live in `httpx/admin.go` (`adminRoutes`, mounted at `/admin` inside the page group). Login/logout are outside `requireAdmin`; everything else is inside it, which also sets `Cache-Control: private, no-store`. Add new admin pages to that group.
- Rate limiting uses `limitByIP(n)` (keys on `RemoteAddr`, not `X-Forwarded-For`, so it cannot be forged; with `TRUST_PROXY=true` the RealIP middleware has already rewritten `RemoteAddr`). Reuse it for `/patrol/tonight` in Step 7 (60/min). Do not use `httprate.LimitByIP`.
- Flash messages: `sessions.Put(ctx, sessionFlash, msg)` then redirect (POST-redirect-GET); the GET handler reads it with `PopString`. Invalid forms re-render with 422 and echo submitted values via `view.SettingsForm`-style `Values`/`Errors` maps keyed by field name.
- `create-admin` reads the password twice without echo on a terminal, or two lines when stdin is piped (`printf 'pw\npw\n' | patrolhub create-admin <user>`; also works with `docker compose exec -T`). Passwords need ≥10 characters; `AdminService.Authenticate` does a dummy bcrypt compare for unknown users to equalize timing.
- **Integration tests** use `internal/testdb.New(t)`: a throwaway, freshly migrated database created on the server in `DATABASE_URL` and dropped afterwards (skipped when unset), so tests never touch dev data. `httpx/integration_test.go` (`newApp`, `loggedIn`, `browser.post/mustPost`) runs the whole app with an injectable clock (`Deps.Now`); run them with `set -a; . ./.env.example; set +a; go test ./...`.
- Service-level "not found" is `service.ErrNotFound`; form errors are `service.ValidationErrors` (field → message) which handlers re-render with 422.
- A schedule rule that has not started yet is deleted when "removed"; a running one gets `effective_to = yesterday` (never deleted, so closed nights keep their team).
