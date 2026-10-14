# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

PatrolHub is a neighborhood (RT) night-patrol ("ronda") attendance/reporting web app. **The repo currently contains only `PatrolHub-Implementation-Plan.md`** (in Indonesian) — no code, git repo, or build files exist yet. The plan is the source of truth: a 14-step MVP build guide with checkpoints per step. Requirement IDs (e.g. F2.5) refer to a PRD that is not in the repo; if the plan and PRD conflict, the PRD wins — note the difference and ask the product owner. When unsure about a business rule, stop and ask rather than guess.

## Working rules from the plan

- Code, file names, tables, routes, comments in **English**; all user-facing text in **Indonesian**.
- Only use libraries listed in the plan's Stack table; no extra dependencies without approval.
- One step = one commit, message format `step-<n>: <summary>` (e.g. `step-4.2: add patrol window logic`). A step is done only when `make check` passes and its Checkpoint items are met. Work steps in order.
- Never edit a committed migration; schema changes outside Step 2 need a new migration file.
- Business logic (patrol window, night team, fines, monthly recap) lives **only** in `internal/domain/` as pure functions: no DB, no HTTP, no `time.Now()` — current time is passed in as a parameter. Table-driven tests required (≥85% coverage on domain).
- All date math uses `Asia/Jakarta` (`config.Jakarta`, with `time/tzdata` embedded), never server timezone.
- Money is `INTEGER` rupiah, displayed as `Rp 5.000`.

## Planned stack and commands

Go 1.22+ with chi, templ, HTMX + Alpine.js (vendored, no CDN), Tailwind v4 standalone CLI + daisyUI v5, PostgreSQL 16 via pgx, sqlc, goose, scs sessions, nosurf CSRF.

```
make generate   # templ generate + sqlc generate
make css        # ./bin/tailwindcss -i web/styles/input.css -o web/static/css/app.css --minify
make check      # generate, go vet ./..., go test ./...
make dev        # air live reload
make build      # generate + css + CGO_ENABLED=0 go build -o bin/patrolhub ./cmd/patrolhub
docker compose up -d                  # dev Postgres
go test ./internal/domain/... -cover  # domain tests
go test ./internal/domain -run TestCurrentWindow   # single test
```

Binary subcommands: `patrolhub serve` (auto-migrate + background jobs + HTTP), `migrate`, `create-admin <username>`, `seed-demo` (only when `APP_ENV=development`). Config via env: `APP_ENV`, `HTTP_ADDR`, `BASE_URL`, `DATABASE_URL`, `PHOTO_DIR`, `TRUST_PROXY`.

## Architecture (planned)

Layering: **handler** (`internal/httpx`) → **service** (`internal/service`, orchestrates domain + db + storage, transactions via `WithTx`) → **repository** (`internal/db`, sqlc-generated, never hand-edited). Handlers must not call `db` directly. Views are `.templ` files in `internal/view`; migrations embedded via `embed.FS` and run with goose; static assets embedded and served at `/static/`.

Key domain concepts that span several files:

- **Patrol night** is keyed by `patrol_date` = the date the night *starts*. The report window runs `window_open` (22:00) on day D to `window_close` (06:00) on D+1, so a report at 00:30 belongs to the *previous* date. `domain.CurrentWindow` decides this.
- **Night team** = recurring `schedule_rule` (weekday + effective_from/to; members are removed by setting `effective_to`, never deleting) with `schedule_override` (ADD/REMOVE/SWAP) applied on top, via `domain.ResolveTeam`. Attendance rows are synced lazily from the team on page view/write; `PRESENT` and `WALK_IN` rows are never auto-deleted.
- **Anonymous reporting** (QR → `/patrol/tonight`, no login, rate-limited by IP) writes attendance, situation note, photos (≤1 MB JPEG/WebP, client-side resized), and rain requests; every write is audited in `audit_log` with IP/UA. Photos are never shown publicly and only visible to admins after the night closes.
- **Closer job** (`internal/job/closer.go`, every 5 min, idempotent): backfills missed nights from `go_live_date`, then closes nights past window close. `domain.CloseNight` decides status (`RAINED_OUT` / `RAIN_REQUESTED` / `NO_REPORT` / `REPORTED`), fines, and `needs_review`. The fine rate is frozen on the night (`fine_rate`) at close; later rate changes don't alter closed nights.
- **Admin** (session login, bcrypt) confirms/rejects rain, corrects attendance (reason required, audited), records fine payments, exports CSV. **Public recap** (`/recap/{yyyy}/{mm}`) is built from closed nights only via `domain.BuildMonthRecap`; it must never expose phone numbers or photos.
- Retention job deletes photos older than ~6 months (183 days).
