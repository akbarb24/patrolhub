# Deploying PatrolHub

One small VPS (or homelab box) with Docker and a domain pointing at it. HTTPS is required:
browsers only allow the camera location (GPS) on secure pages.

## First release

1. Copy `deploy/` and the repository to the server (the image is built from the repository root).
2. `cd deploy && cp .env.example .env`, then set `DOMAIN`, `BASE_URL`, and a long random
   `POSTGRES_PASSWORD` (use the same password inside `DATABASE_URL`).
3. `mkdir -p data/photos && sudo chown 65532:65532 data/photos`
4. `docker compose up -d --build`
5. `docker compose exec app /patrolhub create-admin <username>` (the password is read from the terminal;
   use `docker compose exec -T` and pipe it twice to script it).
6. Log in at `https://<domain>/admin/login` and open **Pengaturan**: set the RT name, the report window,
   the fine, the minimum photos and **tanggal mulai aplikasi** (`go_live_date`; nights before it are ignored).
7. Add members and the weekly schedule, then print the QR from **Pengaturan → /admin/qr** and put it up at the post.
8. Install the backup cron (see `backup.sh`), and restore a dump into an empty database once to prove it works.

## Operations

- Logs are JSON on stdout: `docker compose logs -f app`.
- The app migrates the database on start and runs the closing job (every 5 minutes) and the photo
  retention job (daily, 183 days) itself.
- Photos live in `deploy/data/photos`; the database in the `pgdata` volume. `backup.sh` archives both.
