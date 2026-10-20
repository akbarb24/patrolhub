# Deploying PatrolHub on Fly.io

One always-on machine in `sin` (Singapore), Postgres on **Neon**, photos in **Tigris** (Fly's
S3-compatible storage). Nothing is stored on the machine itself, so it can be replaced at any time.
The Caddy/compose setup in [README.md](README.md) remains the self-hosted alternative.

## Before you start

- `flyctl` installed and logged in: `fly auth login`.
- A Neon project, ideally in **AWS ap-southeast-1 (Singapore)** next to the Fly region. Use the
  **direct** connection string (host *without* `-pooler`) with `sslmode=require&channel_binding=require`;
  pgx supports both. The app keeps only a handful of connections, so it does not need the pooler, and
  through the pooler (PgBouncer) pgx's prepared-statement cache can fail with
  `prepared statement name is already in use (SQLSTATE 08P01)`. If you must use the pooler, append
  `&default_query_exec_mode=exec` to the URL.
- Run all commands from the repository root (where `fly.toml` is).

## First release

1. **Create the app.** App names are global; if `patrolhub` is taken, pick another and put it in
   `fly.toml` (`app = "…"`).

   ```sh
   fly apps create patrolhub
   ```

2. **Database.** Set the Neon URL as a secret (quote it: it contains `&`):

   ```sh
   fly secrets set --stage DATABASE_URL='postgresql://neondb_owner:<password>@ep-….ap-southeast-1.aws.neon.tech/neondb?sslmode=require&channel_binding=require'
   ```

3. **Photo bucket.** This creates a private Tigris bucket and sets `BUCKET_NAME`,
   `AWS_ENDPOINT_URL_S3`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_REGION` as secrets:

   ```sh
   fly storage create
   fly secrets list    # check those five names are there; the app reads exactly these
   ```

4. **Public address.** `BASE_URL` in `fly.toml` (`[env]`) must be the address people open; it is
   printed on the QR code. It defaults to `https://patrolhub.fly.dev`: change it if you renamed the
   app, or after adding a custom domain (`fly certs add ronda.example.com`, point DNS as shown, then
   `BASE_URL = "https://ronda.example.com"`). Without it the app exits on start with
   `config: BASE_URL is required` and the deploy times out waiting for the health check.

   Optional report window defaults (only used until `go_live_date` is set):
   `fly secrets set --stage WINDOW_OPEN=22:00 WINDOW_CLOSE=06:00`.

5. **Deploy**, then make sure exactly one machine runs (`fly deploy` may create two):

   ```sh
   make deploy          # rebuilds the CSS, then fly deploy
   fly scale count 1
   fly status           # one machine in sin, check passing
   ```

   The app migrates the database on start. `fly logs` should show `photo store kind=s3`.

6. **First admin.** Neon is reachable from anywhere, so the simplest way is from your own machine
   with the same `DATABASE_URL` (the password is read from the terminal):

   ```sh
   DATABASE_URL=DATABASE_URL='postgresql://neondb_owner:npg_hZaiXj3UOk4P@ep-wild-sky-a1ez6d4j-pooler.ap-southeast-1.aws.neon.tech/neondb?sslmode=require&channel_binding=require' BASE_URL=https://patrolhub.fly.dev \
     go run ./cmd/patrolhub create-admin <username>
   ```

   The image is distroless and has no shell, so `fly ssh console` is not needed for this.

7. Log in at `https://<app>.fly.dev/admin/login`, open **Pengaturan** and set the RT name, report
   window, fine, minimum photos and **tanggal mulai aplikasi** (`go_live_date`). Add members and the
   weekly schedule, then print the QR from `/admin/qr`.

## Later releases

```sh
make deploy
```

## Operations

- **Logs:** `fly logs`. The closer logs every run; if it stops appearing, the machine is down.
- **Always on:** `fly.toml` sets `auto_stop_machines = "off"` and `min_machines_running = 1`. Do
  not change this: the closer and retention jobs run inside the app, and a stopped machine means
  nights never close and nobody is fined.
- **One machine only.** Two would not corrupt data (closing is idempotent and locks rows), but the
  rate limit is kept in memory per machine, so the 60 requests/min limit would effectively double.
- **Neon compute:** the closer queries the database every 5 minutes, so the Neon compute will rarely
  scale to zero. Check that your Neon plan's compute allowance covers an always-on minimum-size
  compute.
- **Backups:** `backup.sh` needs host cron and is not used on Fly. The database is covered by Neon's
  point-in-time restore (the history window depends on the plan). Photos have no second copy; they
  are deleted after 183 days anyway, and all records live in Postgres.
- **Photos:** `fly storage dashboard` opens the bucket. Objects are private; the app serves them
  only to admins (and to the reporter while the night is open).

## Moving from a self-hosted install

Only if an existing install already has photos: copy `deploy/data/photos` into the bucket with the
same keys (relative paths such as `2026/10/07/<uuid>.jpg`), for example with
`aws s3 sync deploy/data/photos s3://<bucket> --endpoint-url https://fly.storage.tigris.dev`.
Restore the database dump into Neon with `pg_restore --no-owner -d '<the Neon URL>' <dump>`.
