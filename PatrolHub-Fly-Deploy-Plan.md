# PatrolHub — Plan: S3 photo storage and Fly.io deployment

Follow-up to `PatrolHub-Implementation-Plan.md` (Steps 1–14 are done). The same working rules apply: one step = one commit (`step-<n>: <summary>`), `make check` must pass, Checkpoints must be met, work in order, and stop and ask when unsure.

## Goal

Run PatrolHub on Fly.io with:

- photos in **Tigris**, Fly's S3-compatible object storage, instead of the local disk;
- a single always-on machine in `sin` (Singapore), so the closer and retention jobs keep running;
- Postgres outside the app machine, reached through `DATABASE_URL`.

The S3 store must also work with SeaweedFS and MinIO, so local development and a later move off Fly need no code changes. When no bucket is configured, the app keeps using `LocalStore` exactly as it does today.

## Why not a Fly volume

- A volume belongs to one machine in one region and is not replicated.
- It is mounted owned by root. The image runs as distroless `nonroot` (uid 65532), so the app could not write photos without changing the image.
- With photos in Tigris, the machine holds no state and can be replaced freely.

## Decisions needed before starting

| # | Question | Default if no answer |
| --- | --- | --- |
| D1 | Approve the new dependency `github.com/minio/minio-go/v7`? | Required. Step 15 cannot start without it. |
| D2 | Postgres: Fly Managed Postgres, or an external provider (Neon, Supabase, …)? | Only affects Step 16's docs. The app just needs `DATABASE_URL`. |
| D3 | Domain: `<app>.fly.dev` or a custom domain (`fly certs add`)? | `<app>.fly.dev`. `BASE_URL` must match whichever is chosen, and the printed QR code uses it. |
| D4 | Do photos need a backup copy outside Tigris? | No. Photos are kept for 183 days and are evidence, not records; the records live in Postgres. |

## Stack addition

| Part | Choice |
| --- | --- |
| Object storage client | `github.com/minio/minio-go/v7`: one module, works with any S3 API (Tigris, SeaweedFS, MinIO, R2, AWS) |

Add it to the Stack table in `PatrolHub-Implementation-Plan.md` and to `README.md`.

---

## Step 15 — S3 photo store

### 15.1 Configuration — `internal/config`

Use the variable names that `fly storage create` sets, so on Fly nothing has to be copied by hand. Confirm the names when running `fly storage create` in Step 16; if they differ, rename them here before committing.

| Variable | Meaning | Default |
| --- | --- | --- |
| `BUCKET_NAME` | Bucket name. **When set, photos go to S3**; when empty, `LocalStore` is used. | — |
| `AWS_ENDPOINT_URL_S3` | Endpoint URL, e.g. `https://fly.storage.tigris.dev` or `http://localhost:8333`. The scheme sets TLS on or off. | — (required when `BUCKET_NAME` is set) |
| `AWS_ACCESS_KEY_ID` | Access key | — (required when `BUCKET_NAME` is set) |
| `AWS_SECRET_ACCESS_KEY` | Secret key | — (required when `BUCKET_NAME` is set) |
| `AWS_REGION` | Region | `auto` |
| `S3_PATH_STYLE` | `true` for SeaweedFS/MinIO (path-style URLs), `false` for Tigris | `false` |

- Add an `S3` struct to `Config` with these fields and an `Enabled()` method.
- `config.Load` returns an error when `BUCKET_NAME` is set but the endpoint or keys are missing, or when the endpoint is not an absolute http(s) URL.
- When `BUCKET_NAME` is set, `PHOTO_DIR` is ignored.
- Never log the secret key.

### 15.2 Shared key validation — `internal/storage/photos.go`

- Move the key check out of `LocalStore.path` into `validKey(key string) (string, error)`. It returns the cleaned slash-separated key or `ErrInvalidKey`.
- Both stores use it, so `S3Store` rejects the same keys (`""`, `..`, `../x.jpg`, absolute paths, …).
- Update the package comment: it no longer says S3 is "later".

### 15.3 `S3Store` — `internal/storage/s3.go`

`S3Store` implements `PhotoStore`, plus a `NewS3Store(cfg config.S3) (*S3Store, error)` constructor.

- **Constructor.** Parse the endpoint: host goes to `minio.New`, scheme sets `Secure`, `S3_PATH_STYLE` sets `BucketLookup` (`BucketLookupPath` or `BucketLookupDNS`). Do not create the bucket; check it exists with `BucketExists` at startup and fail clearly if not.
- **Every call gets a timeout** (`context.WithTimeout`, 15 s). `Save` runs inside the report transaction, which holds the night row lock (`FOR UPDATE`), so a hanging S3 must not hold that lock for long.
- **`Save`.** Read the body into memory (photos are ≤1 MB) and `PutObject` with the exact size. Set `ContentType` from `PhotoContentType(key)`; move that function from `service` to `storage` so both packages can use it. Objects stay private: no ACL and no public URLs.
- **`Open`.** `GetObject` fetches lazily, so a missing object only fails on the first `Read`. Read the whole object (≤1 MB), and return `io.NopCloser(bytes.NewReader(data))`. That way a missing key fails inside `Open` itself.
- **Missing objects.** Map S3 `NoSuchKey` (`minio.ToErrorResponse(err).Code`) to an error wrapping `fs.ErrNotExist`, so callers behave as they do with `LocalStore`.
- **`Delete`.** `RemoveObject`. S3 already succeeds for a missing key. The retention job accepts both `nil` and `fs.ErrNotExist` ([retention.go:41](internal/service/retention.go#L41)), so it needs no change.

### 15.4 Wiring — `cmd/patrolhub/main.go`

- Replace `photos := storage.LocalStore{Dir: cfg.PhotoDir}` with a choice. If `cfg.S3.Enabled()`, use `NewS3Store`; otherwise use `LocalStore`.
- Log which store is active (`slog.Info("photo store", "kind", "s3", "bucket", …)`), without credentials.
- Startup fails if the bucket is unreachable.

### 15.5 Local SeaweedFS for development

- Add a `seaweedfs` service to the dev `docker-compose.yml`. Use image `chrislusf/seaweedfs` with a pinned tag and command `server -s3 -dir=/data`, port `8333`, and a named volume. It runs without credentials in dev.
- In `.env.example`, add the S3 variables commented out, plus a line on how to create the bucket (for example `aws --endpoint-url http://localhost:8333 s3 mb s3://patrolhub-photos`, or SeaweedFS `weed shell` → `s3.bucket.create -name patrolhub-photos`).

### 15.6 Tests

- **Shared contract test** in `internal/storage/contract_test.go`: `testPhotoStore(t, s PhotoStore)` checks
  - save then open returns the same bytes;
  - opening a missing key gives `fs.ErrNotExist`;
  - deleting a missing key gives `nil` or `fs.ErrNotExist`;
  - invalid keys give `ErrInvalidKey` for all three methods.
- `LocalStore` runs the contract test always. `S3Store` runs it only when `S3_TEST_ENDPOINT` is set, using a unique key prefix per test that is cleaned up afterwards. This follows the same pattern as `testdb` with `DATABASE_URL`.
- `config` tests: S3 is off without `BUCKET_NAME`; a missing endpoint or key is an error; `AWS_REGION` defaults to `auto`; the endpoint must be http(s).
- The existing integration tests keep using `LocalStore` (temporary directory) and must still pass unchanged.

### Checkpoint 15

- [ ] `make check` passes. The S3 contract test was also run once against local SeaweedFS (`S3_TEST_ENDPOINT=http://localhost:8333 go test ./internal/storage`).
- [ ] With the S3 variables set, `make dev` against SeaweedFS works: a photo uploaded on `/patrol/tonight` shows as a reporter thumbnail and on `/admin/photos/{id}`, and the object exists in the bucket.
- [ ] Without the S3 variables the app behaves exactly as before (photos under `PHOTO_DIR`).
- [ ] Stopping SeaweedFS during an upload gives the guard "Gagal mengunggah foto" within about 15 s, not a hang. The attendance and note forms keep working.
- [ ] Retention deletes the object, then the row.

Commit: `step-15: add S3 photo store (Tigris, SeaweedFS, MinIO)`

---

## Step 16 — Fly.io deployment

### 16.1 `fly.toml` (repo root)

- `primary_region = "sin"`.
- `[build]` uses the existing `Dockerfile`. As with `make docker`, it builds from the committed `web/static/css/app.css`, so run `make css` before deploying.
- `[env]`: `APP_ENV=production`, `HTTP_ADDR=:8080`, `TRUST_PROXY=true`. Secrets go in `fly secrets`, not here.
- `[http_service]`:
  - `internal_port = 8080`, `force_https = true`;
  - `auto_stop_machines = "off"` and `min_machines_running = 1` — **required**: the closer (every 5 minutes) and the retention job run inside `serve`; a stopped machine means nights never close and nobody is fined;
  - `auto_start_machines = true`.
- `[[http_service.checks]]`: `GET /healthz`, interval 15 s, timeout 5 s.
- `[[vm]]`: `shared-cpu-1x`, 256 MB. Raise it only if the metrics show memory pressure.
- **Run exactly one machine** (`fly scale count 1`). `fly launch` creates two by default. Two machines would not corrupt data, because the closer is idempotent and locks rows, but the rate limiter is in memory per machine, so the 60/min limit would effectively double.

### 16.2 Makefile

- `deploy: css` → `fly deploy`. The CSS is rebuilt first so a stale `app.css` is never shipped.

### 16.3 Docs — `deploy/FLY.md`

Write the first-release steps in order, as copy-paste commands:

1. `fly launch --no-deploy` (use the existing `fly.toml`), then `fly scale count 1`.
2. Postgres per D2, then `fly secrets set DATABASE_URL=…`.
3. `fly storage create` — creates the Tigris bucket and sets `BUCKET_NAME` and the `AWS_*` secrets. Confirm the names match Step 15.1.
4. `fly secrets set BASE_URL=https://<app>.fly.dev` (or the custom domain per D3, after `fly certs add <domain>`).
5. Optional: `fly secrets set WINDOW_OPEN=22:00 WINDOW_CLOSE=06:00`.
6. `make deploy`.
7. Create the first admin: `fly ssh console -C "/patrolhub create-admin <username>"`. The image is distroless and has no shell; check that `-C` with an interactive password prompt works. If it does not, pipe the password the way the CLAUDE.md `create-admin` note describes.
8. Log in, set `go_live_date` and the other settings, print the QR code from `/admin/qr`.

Also cover:

- **Backups.** `deploy/backup.sh` relies on host cron, which Fly does not have. Postgres backups come from the provider (D2). Photos per D4.
- **Moving existing photos.** Only needed if a self-hosted install already has photos: copy `PHOTO_DIR` into the bucket with the same keys (`rclone copy` or `aws s3 sync`). Keys are relative paths, so no database change is needed.
- `deploy/README.md` gets one line pointing to `FLY.md`. The Caddy/compose setup stays as the self-hosted option.

### Checkpoint 16

- [ ] `fly deploy` succeeds; `/healthz` is green; `fly status` shows one machine in `sin`.
- [ ] The machine stays up overnight with no traffic. `fly logs` shows closer ticks, and a night is closed after its window ends.
- [ ] From a phone over mobile data, using the printed QR code: the report page loads over HTTPS, a photo upload works, and the object appears in Tigris (`fly storage dashboard`).
- [ ] Admin login works, and the admin photo view loads from Tigris.
- [ ] `fly machine restart` loses nothing: photos are in Tigris and sessions and data are in Postgres.

Commit: `step-16: add Fly.io config and deploy guide`

---

## Step 17 — Client IP behind Fly's proxy

`realIP` ([middleware.go:171](internal/httpx/middleware.go#L171)) uses the rightmost `X-Forwarded-For` entry, which is correct behind Caddy. Behind Fly it must be **verified**, because the rate limit (60/min per IP on `/patrol/tonight`) depends on it.

### 17.1 Verify

- Temporarily log `RemoteAddr`, `X-Forwarded-For` and `Fly-Client-IP` on one request. Use a debug log, and never commit header logging: `requestLogger` must keep logging no headers.
- Send `curl -H 'X-Forwarded-For: 1.2.3.4' -H 'Fly-Client-IP: 1.2.3.4' https://<app>.fly.dev/healthz` and check which values reach the app.

### 17.2 Adjust (only if needed)

- If Fly overwrites `Fly-Client-IP` with the real client IP, add an optional `CLIENT_IP_HEADER` config. When `TRUST_PROXY=true` and the variable is set, take the IP from that single header only. Otherwise keep today's rightmost-`X-Forwarded-For` logic, so the Caddy setup is unchanged.
- Set `CLIENT_IP_HEADER=Fly-Client-IP` in `fly.toml` `[env]`.
- Tests: the configured header wins; a forged `X-Forwarded-For` is ignored when the header is configured; an invalid IP in the header leaves `RemoteAddr` unchanged.
- If the rightmost `X-Forwarded-For` entry turns out to be correct on Fly, skip 17.2 and only record the finding in CLAUDE.md.

### Checkpoint 17

- [ ] On Fly, the rate limit counts the real client IP: 61 quick requests from one phone get a 429, and a request from a second network is not affected.
- [ ] A forged `X-Forwarded-For` or `Fly-Client-IP` sent by the client cannot change the IP the app sees.

Commit: `step-17: read client IP from Fly-Client-IP behind Fly proxy` (or `step-17: verify client IP handling on Fly` if 17.2 was skipped)

---

## After all steps

Update `CLAUDE.md`:
- Storage section: the S3 store, its env vars, the `fs.ErrNotExist` mapping, the 15 s timeout.
- Deploy section: Fly, always-on machine, one machine only, `make deploy`.
- Client IP outcome from Step 17.
- Under "Not verified": anything still not checked on a real phone.
