# WhatsApp gateway (GOWA) on Fly.io

PatrolHub posts the patrol report to the RT's WhatsApp group through
[GOWA](https://github.com/aldinokemal/go-whatsapp-web-multidevice), a REST server built
on whatsmeow (the unofficial WhatsApp Web protocol). It runs as its own private Fly app
next to `patrolhub`; PatrolHub talks to it over plain HTTP, so there is no new Go
dependency.

> The PRD lists automatic WhatsApp notifications as phase 2 (out of MVP scope). Confirm
> with the product owner before shipping the PatrolHub side.

## How it fits together

- New Fly app `patrolhub-wa` in `sin`, one machine, always on.
- One 1 GB volume at `/app/storages`: GOWA's default SQLite keeps the WhatsApp login
  session there, so it survives deploys and restarts.
- **No public IP.** Only a private Flycast address: PatrolHub calls
  `http://patrolhub-wa.flycast`, nothing on the internet can reach it.
- Why Flycast and not `.internal`: `.internal` goes straight to the machine over IPv6,
  but GOWA listens with `app.Listen(AppHost+":"+AppPort)` on `0.0.0.0` (IPv4 only), and
  `APP_HOST="::"` would produce an invalid address. Flycast goes through Fly's proxy,
  which reaches the machine over IPv4.

## 1. Before you start

- A **dedicated phone number** on a phone that is online now and then (WhatsApp unlinks
  a linked device after roughly 14 days with the main phone offline). Add it to the RT
  group.
- **Pin an exact image tag** from GOWA's releases (`v9.7.0` at the time of writing), never
  `latest`.

## 2. `deploy/gowa/fly.toml`

The image's default command is already `rest`, and its entrypoint fixes the ownership of
mounted volumes before dropping to the non-root `gowauser`, so neither needs configuring.

```toml
# WhatsApp gateway (GOWA) for PatrolHub. Private only: reachable at
# http://patrolhub-wa.flycast from the patrolhub app, never from the internet.
app = "patrolhub-wa"
primary_region = "sin"

[build]
  image = "ghcr.io/aldinokemal/go-whatsapp-web-multidevice:v9.7.0"

[env]
  APP_PORT = "3000"
  # Not needed.
  MCP_ENABLED = "false"
  # The number sits in the RT group: without this every photo posted there fills the volume.
  WHATSAPP_AUTO_DOWNLOAD_MEDIA = "false"

[mounts]
  source = "wa_data"
  destination = "/app/storages"

[http_service]
  internal_port = 3000
  # Flycast is plain HTTP on port 80; an https redirect would break it.
  force_https = false
  # Keep the WhatsApp connection alive; a long-offline linked device gets logged out.
  auto_stop_machines = "off"
  auto_start_machines = true
  min_machines_running = 1

  [[http_service.checks]]
    grace_period = "15s"
    interval = "30s"
    timeout = "5s"
    method = "GET"
    path = "/health"

[[vm]]
  size = "shared-cpu-1x"
  memory = "256mb"
```

`/health` is public; every other endpoint needs basic auth.

The dashboard is not in the image: GOWA downloads it from the latest `aldinokemal/gowa-ui`
GitHub release at start and caches it in `storages/ui` (on the volume). Leave
`APP_UI_AUTO_UPDATE` at its default `true`: setting it to `false` also skips that first
download, and the page then only says "the dashboard has not been downloaded yet".

## 3. Create and deploy

```bash
fly apps create patrolhub-wa                      # same org as patrolhub, or Flycast won't route
fly volumes create wa_data --region sin --size 1 -a patrolhub-wa
fly secrets set APP_BASIC_AUTH='patrolhub:<long-random-password>' -a patrolhub-wa
fly ips allocate-v6 --private -a patrolhub-wa     # the Flycast address
fly deploy -c deploy/gowa/fly.toml --no-public-ips
fly ips list -a patrolhub-wa                      # must show ONLY the private v6
fly scale count 1 -a patrolhub-wa
```

If `fly ips list` shows a public v4/v6, release it (`fly ips release <ip> -a patrolhub-wa`):
otherwise the gateway, and the WhatsApp number behind it, is on the internet with only a
password in front of it. Check this after every deploy.

## 4. Link the number and find the group ID

Open a tunnel from your laptop:

```bash
fly proxy 3000:80 patrolhub-wa.flycast -a patrolhub-wa
```

1. Open `http://localhost:3000` (basic auth), click **Login**, and on the dedicated phone go
   to **WhatsApp → Perangkat tertaut → Tautkan perangkat** and scan the QR. A pairing-code
   login (`/app/login-with-code`) also exists.
2. Find the group JID (ends in `@g.us`):
   ```bash
   curl -u patrolhub:<pw> http://localhost:3000/user/my/groups
   ```
3. Send a test message:
   ```bash
   curl -u patrolhub:<pw> -H 'Content-Type: application/json' \
     -d '{"phone":"<group-jid>@g.us","message":"Tes dari PatrolHub"}' \
     http://localhost:3000/send/message
   ```

With one linked device no `X-Device-Id` header is needed: GOWA falls back to the only
device.

## 5. PatrolHub secrets

With these set, every night is posted to the group after it closes (within one 5-minute
closer tick). Without `WA_API_URL` the report is off; startup logs `whatsapp report
enabled=…` either way.

```bash
fly secrets set -a patrolhub \
  WA_API_URL=http://patrolhub-wa.flycast \
  WA_API_USER=patrolhub WA_API_PASS='<same password>' \
  WA_GROUP_ID='<group-jid>@g.us'
```

## Operations

- **Upgrade:** change the tag in `fly.toml`, `fly deploy -c deploy/gowa/fly.toml`. The
  session survives on the volume.
- **Logged out** (device unlinked, or volume lost): repeat step 4. Sends fail and are
  logged until then.
- **Backups:** Fly snapshots volumes daily by default. Worst case of losing the volume is
  scanning the QR again.
- **Memory:** if `fly logs` shows an out-of-memory kill, raise `memory` to `512mb`.
- **Risk:** this is an unofficial protocol. The number can be banned or the gateway can
  break when WhatsApp changes; PatrolHub must keep working without notifications.
