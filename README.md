# PatrolHub

Night-patrol ("ronda") attendance for a single RT (neighborhood unit), replacing an Excel sheet.

- **Guard post report page** — a QR-linked page, no login, used on a phone at the post: attendance, situation note, photos, rain requests.
- **Public monthly recap** — residents see who patrolled and the month's summary, with a WhatsApp-ready text.
- **Automatic fines** — nights are closed by a background job and absentees are fined at the rate frozen at close time.
- **Admin area** — schedule, members, rain confirmation, attendance corrections, fine payments, CSV export, printable QR.

The MVP covers one RT, one post and one or more admins. Multi-tenant, member login, native app, notifications, online payment and incident reports are phase 2.

The UI is in Indonesian; code, routes and comments are in English.

## Stack

Go, chi, templ, HTMX + Alpine.js, Tailwind CSS v4 + daisyUI, PostgreSQL 16+ (pgx, sqlc, goose), scs sessions, nosurf CSRF, minio-go for optional S3 photo storage. The full list is in [PatrolHub-Implementation-Plan.md](PatrolHub-Implementation-Plan.md).

## Getting started

Requirements: Go, Docker, and `make`. `templ`, `sqlc`, `air` and the Tailwind CLI are installed into the git-ignored `./bin`, and the Makefile puts it on `PATH`, so call tools through `make`.

```sh
docker compose up -d                 # dev Postgres on :5432 (patrolhub/patrolhub)
set -a; . ./.env.example; set +a     # config comes from environment variables only
make generate css                    # templ + sqlc, then Tailwind
go run ./cmd/patrolhub migrate       # optional: `serve` also migrates on start
printf 'a-long-password\na-long-password\n' | go run ./cmd/patrolhub create-admin admin
go run ./cmd/patrolhub seed-demo     # optional demo data (APP_ENV=development only)
make dev                             # live reload on http://localhost:8080
```

Then log in at `/admin/login`, open **Pengaturan** and set the RT name, patrol window, fine, minimum photos and `go_live_date`.

### Configuration

| Variable | Description |
| --- | --- |
| `DATABASE_URL` | Required. Postgres connection string. |
| `BASE_URL` | Required. Absolute http(s) URL users reach the app at (used for CSRF origin checks and the QR). |
| `APP_ENV` | `development` enables `seed-demo`. |
| `HTTP_ADDR` | Listen address, default `:8080`. |
| `PHOTO_DIR` | Where uploaded photos are stored. |
| `TRUST_PROXY` | `true` behind a reverse proxy (takes the client IP from `X-Forwarded-For`). |

### Commands

| Command | Purpose |
| --- | --- |
| `make check` | generate, css, `go vet`, `go test`. Must pass before every commit. |
| `make generate` | `templ generate` + `sqlc generate` |
| `make css` | build `web/static/css/app.css` |
| `make dev` | live reload with air |
| `make build` | production binary at `bin/patrolhub` |
| `make docker` | build the CSS, then the Docker image `patrolhub:latest` |

Integration tests need a database server: run `set -a; . ./.env.example; set +a; go test ./...`. Each test gets a throwaway database that is dropped afterwards, so dev data is never touched. Without `DATABASE_URL` they are skipped.

## How it works

- A patrol night is keyed by the date it **starts**. The window runs from 22:00 on day D to 06:00 on D+1 (configurable), so a report at 00:30 belongs to the previous date.
- The night team comes from weekly schedule rules plus per-date overrides (add / remove / swap).
- Every 5 minutes a closer job closes finished nights: no report → everyone fined; report → only absentees fined; rain → no fines (a pending rain request waits for admin review).
- Business rules live only in `internal/domain/` as pure functions with table-driven tests; all date math uses `Asia/Jakarta`.

## Project layout

```
cmd/patrolhub/     entry point and subcommands (serve, migrate, create-admin, seed-demo)
internal/domain/   pure business logic (window, team, closing, fines, recap)
internal/service/  orchestration: domain + db + storage, transactions
internal/httpx/    handlers, router, middleware, acceptance test
internal/db/       sqlc output (committed, not hand-edited)
internal/view/     templ templates
internal/job/      closer and photo-retention jobs
migrations/        goose migrations (never edit a committed one)
queries/           sqlc query files, one per table
web/               embedded static assets and Tailwind sources
deploy/            production compose, Caddyfile, backup script
```

## Deployment

A multi-stage Dockerfile produces a small non-root distroless image. `deploy/` holds a production compose file (Postgres + app + Caddy for HTTPS), the Caddyfile and a backup script. See [deploy/README.md](deploy/README.md) for the first-release steps. HTTPS is required because browsers only expose the camera location on secure pages.

## Documentation

- [PRD — PatrolHub (MVP Aplikasi Pencatatan Ronda).md](<PRD — PatrolHub (MVP Aplikasi Pencatatan Ronda).md>) — requirements and business rules (Indonesian). It wins when documents disagree.
- [PatrolHub-Implementation-Plan.md](PatrolHub-Implementation-Plan.md) — the 14-step build guide (Indonesian).
- [CLAUDE.md](CLAUDE.md) — working rules and architecture notes for contributors and AI assistants.

## Status

All 14 plan steps are implemented. `internal/httpx/acceptance_test.go` walks the PRD acceptance table end to end. Still to verify by hand on real devices: photo capture/resize on a phone, the layout at 360 px, A5 QR printing, HTTPS through Caddy, the "Salin rekap" clipboard button, and restoring a production backup.
