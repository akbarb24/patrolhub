# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

PatrolHub replaces an Excel sheet for a single RT's (neighborhood unit) night-patrol ("ronda") attendance: a QR-linked report page used at the guard post, a public monthly recap for residents, and automatic fines. MVP scope is one RT, one post, one or more admins; no multi-tenant, member login, native app, notifications, online payment, or incident reports (those are phase 2).

Two docs drive the work (both Indonesian, both in the repo root):

- `PRD — PatrolHub (MVP Aplikasi Pencatatan Ronda).md` — requirements, IDs `F1.1`–`F5.5`, each Must or Should, plus business rules and decisions.
- `PatrolHub-Implementation-Plan.md` — 14-step build guide with a Checkpoint per step. Requirement IDs in it refer to the PRD.

If they conflict, the PRD wins: note the difference and ask the product owner. When unsure about a business rule, stop and ask rather than guess.

**Progress:** All 14 steps of the plan are implemented and committed. `PatrolHub-Fly-Deploy-Plan.md` adds Steps 15–17 (S3 photo storage, Fly.io deploy, client IP behind Fly) with the same conventions. `internal/httpx/acceptance_test.go` walks the PRD acceptance table (F1–F5) end to end with the clock handed over in UTC.

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
make check               # generate, css, go vet ./..., go test ./...   (must pass before every commit)
make dev                 # air live reload (runs `serve`)
make build               # generate + css + CGO_ENABLED=0 build -> bin/patrolhub
make docker              # css, then docker build -t patrolhub:latest .
make deploy              # css, then fly deploy (see deploy/FLY.md)

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
- **Report page** (`httpx/patrol.go`, `service/patrol.go`): every reporter write goes through `PatrolService.write`, which checks the window, locks the night row (`FOR UPDATE`), syncs the team, runs the action, sets `first_reported_at`, and audits (`ANONYMOUS`, IP, UA, `night_id` in detail) in one transaction. Errors map to short Indonesian plain-text bodies that `web/static/js/patrol.js` shows as a toast; for window/night-closed the handler sets `HX-Redirect: /patrol/tonight` with 409 so htmx reloads into the closed state. Photo uploads are sniffed (`http.DetectContentType`), ≤1 MB, stored via `storage.PhotoStore` and the file is deleted again if the transaction fails.
- Fragment endpoints are `Cache-Control: no-store`; the patrol group uses `limitByIP(60)` and per-route `maxBody` (64 KB forms, ~1 MB + overhead for photos).
- `web/static/js/photo.js` (resize ≤1280px, JPEG q0.7 then 0.5 if >500 KB, optional geolocation, fetch upload) was only syntax-checked, never run in a real browser/phone.
- **Closer** (`service/closing.go`, run by `job.Closer` from `serve`): `Tick(now)` is a no-op without `go_live_date`; it creates missing nights only for days that have a team (an unscheduled weekday has no patrol night — creating one would show as a bogus "no report"), then closes every night whose window ended, skipping nights before go-live. Per-night errors are logged and joined, not fatal. It re-syncs the team right before closing.
- **Admin night actions** (`service/nights.go`): mark/confirm/reject rain and attendance correction all run via `adminAction` (locks the night, writes an `admin:<id>` audit entry with `night_id` in detail). Corrections are allowed only on **closed**, non-rained-out nights (on an open night the closer would overwrite them) and need a reason of ≥5 characters; fines are recalculated from the night's frozen `fine_rate` (falling back to the current rate if NULL). Rejecting a rain request on a closed night recalculates fines the same way. Admin photos are served at `/admin/photos/{id}` (`private, max-age=3600`).
- Fines (`service/fines.go`): per member and calendar month, total = sum of `fine_amount` on closed nights, minus recorded `fine_payment`s; negative remaining means overpaid. `/admin/fines` redirects to the current month.
- **Public recap** (`service/recap.go`, `httpx/recap.go`, `view/recap.templ`): `RecapService.NightRows` feeds `domain.BuildMonthRecap` with closed nights that have at least one attendance row (a night without a team is left out). Months after the current one (and years outside 2000–2100) are 404. Responses are `Cache-Control: private, max-age=300` rather than the plan's `public`, because every page carries a CSRF cookie. Each public page transfers ~166 KB gzipped (HTML + CSS + htmx + Alpine + the two preloaded fonts, ~82 KB of that); `TestPageWeightStaysUnder200KB` guards the 200 KB budget. The status markers use `aria-label`s like "7 Oktober: hadir". `aria-current="page"` must be written as a conditional attribute (`if … { aria-current="page" }`), not `aria-current?=`, which renders a bare attribute.
- **WhatsApp text** (`domain/recap_text.go`, `GET /recap/{yyyy}/{mm}/text?range=month|week`): pure builders; "week" = the seven calendar days ending with the latest closed night, clipped to the month. The "Salin rekap" menu is a native `<details>` plus `web/static/js/recap.js` (no Alpine: the CDN build starts before later deferred scripts, so `Alpine.data` registered there would be too late).
- **CSV export** (`GET /admin/export/{yyyy}/{mm}.csv`, admin only): UTF-8 BOM, columns `tanggal,hari,status_malam,blok,nama,status_absen,denda`, Indonesian labels, closed nights only. `csvSafe` prefixes `'` to cells starting with `= + - @` or a control character so spreadsheets never evaluate them.
- **Excel export** (`GET /admin/export/{yyyy}/{mm}.xlsx`, `httpx/export_xlsx.go`, excelize): the RT's old hand-kept grid built from `RecapService.Month` cards: weekday groups (merged column A), one column per day of the month, `v`/`1`/`h` for present/absent/rain, grey = no duty, then the fine per member and a total. Fines are values, not the old `SUM(...)*5000` formula, because rates are frozen per night.
- **Hardening** (`httpx/middleware.go`): global middleware order is RequestID, RealIP (only with `TRUST_PROXY`), `requestLogger` (slog; logs path but never query/headers/cookies/body), Recoverer, `securityHeaders` (nosniff, `Referrer-Policy: same-origin`, `X-Frame-Options: DENY`), Compress, `limitBodies` (64 KB forms, ~1 MB + overhead for `/patrol/tonight/photos`), sessions, CSRF. `limitBodies` must stay *before* CSRF because nosurf parses the form to find the token; an oversized form therefore ends as a rejected request (403), not a 413. `TestTemplatesNeverUseRawOutput` forbids `templ.Raw`.
- **Retention** (`service/retention.go`, `job/retention.go`): daily, deletes photos with `created_at` older than 183 days — file first, then row; a missing file still lets the row go, any other file error keeps the row for the next run.
- **Window defaults from env**: optional `WINDOW_OPEN` / `WINDOW_CLOSE` (HH:MM) are applied by `SettingsService.SeedWindow` at `serve` start, only while `go_live_date` is unset; after go-live the admin-edited DB values win. Parsed in `main` (config cannot import `domain`: import cycle).
- `deploy/backup.sh` is the cron script (pg_dump custom format + photo tarball, 30-day cleanup); the dump/restore round trip was verified against the dev database.
- **Docker/deploy**: `Dockerfile` (multi-stage, distroless `:nonroot`, uid 65532, ~17 MB) builds from the **committed** `web/static/css/app.css` — the container cannot reliably download the Tailwind CLI — so run `make css` first or use `make docker`; `make check` also rebuilds the CSS so a stale committed file shows up as a git diff. The image's `ENTRYPOINT` already contains `serve`: run one-off commands with `docker compose exec app /patrolhub create-admin <user>` or `docker run --entrypoint /patrolhub …`. `deploy/` holds the production compose (db + app + Caddy), `Caddyfile`, `.env.example`, `backup.sh`, and a short README with the first-release steps. The photo folder must be owned by uid 65532.
- **Photo storage** (`internal/storage`): `LocalStore` (disk, `PHOTO_DIR`) or `S3Store` (minio-go) when `BUCKET_NAME` is set, chosen in `main.photoStore`; startup fails if the bucket is missing. The S3 env names are the ones `fly storage create` sets (`BUCKET_NAME`, `AWS_ENDPOINT_URL_S3`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` default `auto`), plus `S3_PATH_STYLE=true` for SeaweedFS/MinIO. Keys are the same relative paths in both stores (`validKey` rejects escaping keys). `S3Store` maps `NoSuchKey` to `fs.ErrNotExist`, reads objects fully in `Open` (photos are ≤1 MB) so a missing key fails there, and bounds every call with a 15 s timeout because `Save` runs inside the report transaction holding the night lock. `storage.ContentType(key)` gives the media type. The contract test (`contract_test.go`) runs for S3 only with `S3_TEST_ENDPOINT` set; the dev SeaweedFS is `docker compose --profile s3 up -d` (credentials `dev`/`dev-secret` from `deploy/dev/seaweedfs-s3.json`).
- **Fly.io** (`fly.toml`, `deploy/FLY.md`): one always-on machine in `sin` (`auto_stop_machines = "off"`, `min_machines_running = 1`, `fly scale count 1`) because the closer/retention jobs run in-process and the rate limiter is per machine. Postgres is Neon via the **direct** URL (no `-pooler`; `channel_binding=require` is supported by pgx 5.11): through Neon's PgBouncer pooler pgx's statement cache failed on the first deploy with `prepared statement name is already in use (08P01)`; with the pooler, `default_query_exec_mode=exec` would be needed. Photos are in Tigris. `BASE_URL` is in `fly.toml` `[env]`, not a secret. A machine that hit Fly's max restart count stays stopped after a fixed deploy: `fly machine start <id>`. `create-admin` can run locally against the Neon URL; the image has no shell.
- With `TRUST_PROXY=true` the client IP comes from `realIP` (rightmost `X-Forwarded-For` entry only). chi's `RealIP` is deliberately not used: it trusts `True-Client-IP`/`X-Real-IP` first, which a client can forge through Caddy. The Caddyfile also strips those two headers.

## Visual design

`DESIGN.md` (Ko-fi-style: fog canvas `#e5e7eb`, white "sticker" cards with a 2px black outline and no shadows, oat `#e9dfd2` feature blocks, pill buttons/inputs/tags, Kofi Blue `#aac9f7` only for the primary action, hover `#72a4f2`). It is implemented as the `patrolhub` daisyUI theme plus a few component classes in `web/styles/input.css` (`.sticker`, `.oat`, `.display`, `.eyebrow`, and overrides for btn/input/textarea/alert/tabs-box). Fonts are self-hosted OFL files in `web/static/fonts/` (DM Sans for UI text, Bowlby One standing in for bogue-black), latin subset only, preloaded in `base`. Use the display font only for headlines of 48px (`text-5xl`) and up, via `pageTitle` or `.display`. Error red `#b42318` is the only colour outside the palette, kept for form errors and the report toast. Attendance markers: present = blue with a check, absent = ink with an ×, rain = dashed oat with rain strokes.

## Not verified (needs a real phone/browser or a server)

No browser was available while building, so these were checked only indirectly (HTML/markup assertions, JS syntax check) and still need a manual pass: the camera/resize/geolocation upload in `photo.js` on a real phone; the layout at 360 px width; printing the QR page on A5; HTTPS from a phone through Caddy; the "Salin rekap" clipboard button; restoring a real production backup; the DESIGN.md redesign (layout, fonts, wrapping of the 48px display headings at 360 px). The Docker image itself was built and exercised locally (size, non-root, volume permissions, restart persistence).
