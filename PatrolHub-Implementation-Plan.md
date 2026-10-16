# PatrolHub — Rencana Implementasi MVP

Dokumen ini adalah panduan langkah demi langkah untuk membangun MVP PatrolHub sesuai PRD. Ditulis untuk dieksekusi oleh programmer junior atau model AI: kerjakan **berurutan**, satu langkah sekali jalan, dan jangan lanjut sebelum *Checkpoint* langkah itu lolos.

Referensi kebutuhan memakai ID dari PRD (mis. **F2.5**). Bila rencana ini dan PRD bertentangan, PRD yang menang; catat perbedaannya dan tanyakan ke pemilik produk.

---

## 0. Aturan kerja untuk eksekutor

1. **Bahasa:** kode, nama file, tabel, route, dan komentar dalam bahasa Inggris. Semua teks yang tampil ke pengguna dalam bahasa Indonesia.
2. **Library:** hanya pakai library di tabel "Stack". Jangan menambah dependensi lain tanpa persetujuan.
3. **Satu langkah = satu commit.** Format pesan commit: `step-<nomor>: <ringkasan singkat>` (mis. `step-4.2: add patrol window logic`).
4. **Setelah setiap langkah** jalankan `make check` (lihat Langkah 1.6). Langkah dianggap selesai hanya bila `make check` lolos dan semua poin *Checkpoint* terpenuhi.
5. **Jangan mengubah skema database** di luar Langkah 2 tanpa membuat file migrasi baru. Jangan pernah mengedit migrasi yang sudah di-commit.
6. **Logika bisnis** (jendela waktu, tim malam, denda, rekap) hanya boleh ditulis di `internal/domain/` sebagai fungsi murni (tanpa database, tanpa HTTP, tanpa `time.Now()` di dalamnya). Waktu sekarang selalu dioper sebagai parameter.
7. **Zona waktu:** semua perhitungan tanggal memakai `Asia/Jakarta`. Jangan pernah memakai zona waktu server.
8. **Uang:** simpan sebagai `INTEGER` rupiah (tanpa desimal). Tampilkan dengan format `Rp 5.000` (pemisah ribuan titik).
9. Bila ragu, **berhenti dan tanya**, jangan menebak aturan bisnis.

---

## Stack

| Bagian | Pilihan |
| --- | --- |
| Bahasa | Go (versi stabil terbaru, minimal 1.22 karena butuh pola routing `net/http` baru) |
| Router & middleware | `net/http` + `github.com/go-chi/chi/v5` |
| Rate limit | `github.com/go-chi/httprate` |
| Template | `github.com/a-h/templ` |
| Interaktivitas | HTMX + Alpine.js (file JS di-*vendor* ke `web/static/js`, bukan dari CDN) |
| CSS | TailwindCSS v4 *standalone CLI* + daisyUI v5 (ikuti petunjuk "standalone" di dokumentasi daisyUI) |
| Database | PostgreSQL 16+, driver `github.com/jackc/pgx/v5` |
| Query | `sqlc` (kode Go type-safe dari file SQL) |
| Migrasi | `github.com/pressly/goose/v3` |
| Sesi admin | `github.com/alexedwards/scs/v2` + `github.com/alexedwards/scs/pgxstore` |
| CSRF | `github.com/justinas/nosurf` |
| Hash password | `golang.org/x/crypto/bcrypt` |
| QR code | `github.com/skip2/go-qrcode` |
| UUID | `github.com/google/uuid` |
| Object storage (S3) | `github.com/minio/minio-go/v7` (added in Step 15, see `PatrolHub-Fly-Deploy-Plan.md`) |
| Live reload (dev) | `github.com/air-verse/air` |

---

## Langkah 1 — Setup proyek

### 1.1 Inisialisasi modul

```bash
mkdir patrolhub && cd patrolhub
git init
go mod init github.com/<username>/patrolhub
```

### 1.2 Struktur folder

Buat struktur berikut (folder kosong boleh diisi `.gitkeep`):

```
patrolhub/
├── cmd/patrolhub/main.go        # entrypoint + subcommand
├── internal/
│   ├── config/                  # baca env var
│   ├── db/                      # hasil generate sqlc (jangan diedit manual)
│   ├── domain/                  # logika bisnis murni + unit test
│   ├── service/                 # orkestrasi: domain + db + storage
│   ├── httpx/                   # router, handler, middleware
│   ├── view/                    # file .templ (layout, komponen, halaman)
│   ├── storage/                 # penyimpanan foto
│   └── job/                     # job penutupan malam & retensi foto
├── migrations/                  # file SQL goose
├── queries/                     # file SQL untuk sqlc
├── web/
│   ├── styles/input.css         # sumber Tailwind
│   └── static/                  # css hasil build, js vendor, ikon
├── sqlc.yaml
├── Makefile
├── docker-compose.yml           # Postgres untuk development
├── Dockerfile
├── .air.toml
└── .env.example
```

Lapisan yang dipakai: **handler** (`internal/httpx`) → **service** (`internal/service`) → **repository** (`*db.Queries` hasil sqlc). Handler tidak boleh memanggil `db` langsung.

### 1.3 Konfigurasi

Buat `internal/config/config.go` dengan struct berikut, dibaca dari environment variable:

| Env var | Contoh | Keterangan |
| --- | --- | --- |
| `APP_ENV` | `development` / `production` | Mengatur cookie `Secure` dan level log |
| `HTTP_ADDR` | `:8080` | Alamat server |
| `BASE_URL` | `https://ronda.example.com` | Dipakai untuk QR dan teks rekap |
| `DATABASE_URL` | `postgres://patrolhub:patrolhub@localhost:5432/patrolhub?sslmode=disable` | |
| `PHOTO_DIR` | `./data/photos` | Folder penyimpanan foto |
| `TRUST_PROXY` | `true` / `false` | Bila `true`, IP diambil dari `X-Forwarded-For` |

Fungsi `config.Load() (Config, error)` mengembalikan error bila `DATABASE_URL` atau `BASE_URL` kosong. Buat `.env.example` berisi semua variabel di atas.

Tambahkan juga di `internal/config/tz.go`:

```go
package config

import (
	"time"
	_ "time/tzdata" // data zona waktu ikut ke dalam binary
)

var Jakarta = mustLoad("Asia/Jakarta")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}
```

### 1.4 Postgres untuk development

`docker-compose.yml`:

```yaml
services:
  db:
    image: postgres:16
    environment:
      POSTGRES_USER: patrolhub
      POSTGRES_PASSWORD: patrolhub
      POSTGRES_DB: patrolhub
    ports: ["5432:5432"]
    volumes: [pgdata:/var/lib/postgresql/data]
volumes:
  pgdata:
```

### 1.5 Entry point dan subcommand

`cmd/patrolhub/main.go` membaca argumen pertama:

| Perintah | Fungsi |
| --- | --- |
| `patrolhub serve` | Menjalankan migrasi otomatis, job latar belakang, lalu server HTTP |
| `patrolhub migrate` | Hanya menjalankan migrasi `up` |
| `patrolhub create-admin <username>` | Meminta password dari stdin, membuat akun admin |
| `patrolhub seed-demo` | Mengisi data contoh (Langkah 6.4); hanya boleh jalan bila `APP_ENV=development` |

Migrasi di-*embed* ke binary dengan `embed.FS` (`//go:embed migrations/*.sql` di sebuah package `migrations`) dan dijalankan memakai `goose.SetBaseFS` + `goose.Up`.

### 1.6 Makefile

Minimal target berikut:

```make
generate:      ## templ + sqlc
	templ generate
	sqlc generate

css:
	./bin/tailwindcss -i web/styles/input.css -o web/static/css/app.css --minify

check: generate
	go vet ./...
	go test ./...

dev:           ## live reload
	air

build: generate css
	CGO_ENABLED=0 go build -o bin/patrolhub ./cmd/patrolhub
```

### 1.7 Aset frontend

1. Unduh `htmx.min.js` dan `alpine.min.js` (versi stabil terbaru) ke `web/static/js/`.
2. Unduh Tailwind v4 standalone CLI ke `bin/tailwindcss` dan file plugin daisyUI sesuai petunjuk dokumentasi daisyUI untuk standalone CLI.
3. `web/styles/input.css`:

```css
@import "tailwindcss";
@plugin "./daisyui.mjs";
@source "../../internal/view";
```

4. Aset statis di-*embed* (`//go:embed static` di package `web`) dan disajikan di `/static/`.

**Checkpoint Langkah 1**

- [ ] `docker compose up -d` menyalakan Postgres.
- [ ] `go run ./cmd/patrolhub serve` jalan dan `GET /healthz` mengembalikan `200 ok`.
- [ ] `make check` lolos.

---

## Langkah 2 — Skema database

Buat `migrations/00001_init.sql`. Isinya **persis** seperti di bawah (boleh menambah index, jangan mengurangi kolom).

```sql
-- +goose Up
CREATE TABLE admin_user (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE member (
    id          BIGSERIAL PRIMARY KEY,
    house_block TEXT NOT NULL,              -- mis. "C-10", wajib
    name        TEXT,                       -- opsional
    phone       TEXT,                       -- opsional, tidak pernah tampil publik
    notes       TEXT,
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE schedule_rule (
    id             BIGSERIAL PRIMARY KEY,
    member_id      BIGINT NOT NULL REFERENCES member(id),
    day_of_week    SMALLINT NOT NULL CHECK (day_of_week BETWEEN 1 AND 7), -- 1=Senin ... 7=Minggu
    effective_from DATE NOT NULL,
    effective_to   DATE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (effective_to IS NULL OR effective_to >= effective_from)
);
CREATE INDEX schedule_rule_dow_idx ON schedule_rule (day_of_week);

CREATE TABLE schedule_override (
    id                    BIGSERIAL PRIMARY KEY,
    patrol_date           DATE NOT NULL,
    action                TEXT NOT NULL CHECK (action IN ('ADD', 'REMOVE', 'SWAP')),
    member_id             BIGINT NOT NULL REFERENCES member(id),
    replacement_member_id BIGINT REFERENCES member(id),
    note                  TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((action = 'SWAP') = (replacement_member_id IS NOT NULL))
);
CREATE INDEX schedule_override_date_idx ON schedule_override (patrol_date);

CREATE TABLE patrol_night (
    id                BIGSERIAL PRIMARY KEY,
    patrol_date       DATE NOT NULL UNIQUE,
    status            TEXT NOT NULL DEFAULT 'OPEN'
                      CHECK (status IN ('OPEN', 'REPORTED', 'NO_REPORT', 'RAIN_REQUESTED', 'RAINED_OUT')),
    situation_note    TEXT NOT NULL DEFAULT '',
    first_reported_at TIMESTAMPTZ,          -- diisi saat ada aksi laporan pertama
    rain_requested_at TIMESTAMPTZ,
    closed_at         TIMESTAMPTZ,          -- NULL = belum ditutup
    fine_rate         INTEGER,              -- tarif denda yang dibekukan saat ditutup (F3.4)
    needs_review      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE patrol_attendance (
    id          BIGSERIAL PRIMARY KEY,
    night_id    BIGINT NOT NULL REFERENCES patrol_night(id) ON DELETE CASCADE,
    member_id   BIGINT NOT NULL REFERENCES member(id),
    status      TEXT NOT NULL DEFAULT 'ABSENT' CHECK (status IN ('PRESENT', 'ABSENT', 'RAINED_OUT')),
    source      TEXT NOT NULL CHECK (source IN ('SCHEDULED', 'OVERRIDE', 'WALK_IN')),
    fine_amount INTEGER NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (night_id, member_id)
);

CREATE TABLE patrol_photo (
    id          BIGSERIAL PRIMARY KEY,
    night_id    BIGINT NOT NULL REFERENCES patrol_night(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL,
    taken_at    TIMESTAMPTZ NOT NULL,
    latitude    DOUBLE PRECISION,
    longitude   DOUBLE PRECISION,
    size_bytes  INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX patrol_photo_night_idx ON patrol_photo (night_id);

CREATE TABLE audit_log (
    id         BIGSERIAL PRIMARY KEY,
    entity     TEXT NOT NULL,               -- 'patrol_night' | 'patrol_attendance' | 'patrol_photo' | ...
    entity_id  BIGINT NOT NULL,
    action     TEXT NOT NULL,               -- 'toggle_attendance' | 'update_situation' | 'upload_photo' | ...
    actor      TEXT NOT NULL,               -- 'ANONYMOUS', 'SYSTEM', atau 'admin:<id>'
    ip         TEXT,
    user_agent TEXT,
    reason     TEXT,                        -- wajib untuk koreksi admin
    detail     JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE fine_payment (
    id          BIGSERIAL PRIMARY KEY,
    member_id   BIGINT NOT NULL REFERENCES member(id),
    period      CHAR(7) NOT NULL CHECK (period ~ '^[0-9]{4}-[0-9]{2}$'), -- 'YYYY-MM'
    amount      INTEGER NOT NULL CHECK (amount > 0),
    paid_at     DATE NOT NULL,
    recorded_by BIGINT NOT NULL REFERENCES admin_user(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE setting (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO setting (key, value) VALUES
    ('rt_name', 'RT 02'),
    ('window_open', '22:00'),
    ('window_close', '06:00'),
    ('fine_per_absence', '5000'),
    ('min_photos', '3'),
    ('max_photos', '10'),
    ('go_live_date', '');   -- 'YYYY-MM-DD'; job penutupan tidak memproses tanggal sebelum ini

-- tabel sesi untuk scs/pgxstore
CREATE TABLE sessions (
    token  TEXT PRIMARY KEY,
    data   BYTEA NOT NULL,
    expiry TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_expiry_idx ON sessions (expiry);

-- +goose Down
DROP TABLE sessions, setting, fine_payment, audit_log, patrol_photo,
    patrol_attendance, patrol_night, schedule_override, schedule_rule, member, admin_user;
```

> Catatan untuk pemilik produk: dibanding PRD ada tambahan kolom `fine_rate`, `rain_requested_at`, `created_at`, tabel `sessions`, serta pengaturan `go_live_date`. Semuanya pendukung teknis, tidak mengubah perilaku.

### 2.1 Konfigurasi sqlc

`sqlc.yaml`:

```yaml
version: "2"
sql:
  - engine: postgresql
    schema: migrations
    queries: queries
    gen:
      go:
        package: db
        out: internal/db
        sql_package: pgx/v5
        emit_json_tags: true
        emit_pointers_for_null_types: true
```

### 2.2 Query awal

Buat satu file query per tabel di `queries/` (mis. `member.sql`, `schedule.sql`, `night.sql`, `attendance.sql`, `photo.sql`, `audit.sql`, `setting.sql`, `admin.sql`, `fine.sql`). Query yang **wajib ada** (nama fungsi sqlc ditulis dalam komentar `-- name:`):

| File | Query |
| --- | --- |
| `member.sql` | `ListMembers`, `ListActiveMembers`, `GetMember`, `CreateMember`, `UpdateMember`, `SetMemberActive` |
| `schedule.sql` | `ListRulesForDate(dow, date)` — aturan aktif pada tanggal itu; `ListRulesByMember`, `CreateRule`, `EndRule(id, effective_to)`, `ListOverridesForDate`, `ListOverridesBetween(from, to)`, `CreateOverride`, `DeleteOverride` |
| `night.sql` | `InsertNightIfMissing` (`ON CONFLICT (patrol_date) DO NOTHING`), `GetNightByDate`, `GetNightByDateForUpdate` (`FOR UPDATE`), `MarkReported` (isi `first_reported_at` bila masih NULL), `UpdateSituation`, `SetNightStatus`, `CloseNight(id, status, fine_rate, needs_review)`, `ListUnclosedNightsBefore(date)`, `ListNightsBetween(from, to)` |
| `attendance.sql` | `UpsertAttendance` (`ON CONFLICT (night_id, member_id) DO NOTHING`), `ListAttendanceByNight`, `ListAttendanceBetween(from, to)` (join `patrol_night` + `member`), `SetAttendanceStatus`, `SetAttendanceFine`, `DeleteAbsentAttendanceNotIn(night_id, member_ids)` |
| `photo.sql` | `InsertPhoto`, `CountPhotosByNight`, `ListPhotosByNight`, `GetPhoto`, `ListPhotosOlderThan(ts)`, `DeletePhoto` |
| `audit.sql` | `InsertAudit`, `ListAuditByNight` |
| `setting.sql` | `ListSettings`, `UpsertSetting` |
| `admin.sql` | `GetAdminByUsername`, `CreateAdmin` |
| `fine.sql` | `ListPaymentsForPeriod`, `InsertPayment`, `DeletePayment` |

Tambahkan juga helper `internal/service/tx.go` berisi `WithTx(ctx, pool, func(q *db.Queries) error) error` untuk transaksi.

**Checkpoint Langkah 2**

- [ ] `patrolhub migrate` membuat semua tabel dan 7 baris `setting`.
- [ ] `sqlc generate` sukses tanpa error; folder `internal/db` terisi.
- [ ] `make check` lolos.

---

## Langkah 3 — Logika domain (fungsi murni + unit test)

Semua file di `internal/domain/`. Setiap fungsi **wajib** punya *table-driven test* dengan minimal semua kasus yang tercantum. Gunakan `config.Jakarta` di test.

### 3.1 Tipe dasar — `types.go`

```go
type NightStatus string // "OPEN", "REPORTED", "NO_REPORT", "RAIN_REQUESTED", "RAINED_OUT"
type AttendanceStatus string // "PRESENT", "ABSENT", "RAINED_OUT"
type AttendanceSource string // "SCHEDULED", "OVERRIDE", "WALK_IN"

// Date adalah tanggal kalender tanpa jam, selalu ditafsirkan di Asia/Jakarta.
type Date struct{ Year int; Month time.Month; Day int }

func DateOf(t time.Time) Date                 // ambil tanggal di Asia/Jakarta
func (d Date) At(hour, min int) time.Time     // jam tertentu pada tanggal itu, Asia/Jakarta
func (d Date) AddDays(n int) Date
func (d Date) ISOWeekday() int                // 1=Senin ... 7=Minggu
func (d Date) String() string                 // "2026-10-07"
```

Konversi hari: `iso := (int(t.Weekday())+6)%7 + 1` (Go: Minggu=0).

### 3.2 Nama tampilan & format rupiah — `format.go`

| Fungsi | Input | Output |
| --- | --- | --- |
| `DisplayName(name *string, block string)` | `"Dhani"`, `"A-2"` | `Dhani (A-2)` |
| | `nil` atau `"  "`, `"C-10"` | `C-10` |
| `FormatRupiah(n int)` | `0` / `5000` / `180000` / `1250000` | `Rp 0` / `Rp 5.000` / `Rp 180.000` / `Rp 1.250.000` |
| `FormatDateID(d Date)` | 2026-10-07 | `Rabu, 7 Oktober 2026` |
| `MonthNameID(m time.Month)` | `9` | `September` |

### 3.3 Jendela laporan — `window.go` (F2.2, F2.3)

```go
type Clock struct{ Hour, Min int }           // diparse dari "22:00"
func ParseClock(s string) (Clock, error)

type WindowState struct {
    Open      bool
    NightDate Date      // malam yang sedang berjalan bila Open; malam berikutnya bila tidak
    OpensAt   time.Time
    ClosesAt  time.Time
}

func CurrentWindow(now time.Time, open, close Clock) WindowState
func ClosesAt(night Date, close Clock) time.Time  // = night.AddDays(1).At(close)
```

Aturan: jendela malam tanggal `D` mulai `D` pukul `open` dan berakhir `D+1` pukul `close` (asumsi `open > close`, artinya melewati tengah malam).

- Bila jam lokal ≥ `open` → terbuka, `NightDate = hari ini`.
- Bila jam lokal < `close` → terbuka, `NightDate = kemarin`.
- Selain itu → tertutup, `NightDate = hari ini` (malam yang akan datang).

Kasus uji wajib (open 22:00, close 06:00):

| `now` (WIB) | Open | NightDate |
| --- | --- | --- |
| 2026-10-07 21:59 | false | 2026-10-07 |
| 2026-10-07 22:00 | true | 2026-10-07 |
| 2026-10-07 23:59 | true | 2026-10-07 |
| 2026-10-08 00:00 | true | 2026-10-07 |
| 2026-10-08 05:59 | true | 2026-10-07 |
| 2026-10-08 06:00 | false | 2026-10-08 |
| 2026-10-08 12:00 | false | 2026-10-08 |
| 2026-10-07 15:30 UTC (= 22:30 WIB) | true | 2026-10-07 |

Kasus terakhir memastikan fungsi tidak bergantung pada zona waktu input.

### 3.4 Tim malam — `team.go` (F1.2–F1.4)

```go
type Rule struct { MemberID int64; DayOfWeek int; From Date; To *Date }
type Override struct { Action string; MemberID int64; ReplacementID *int64 }
type TeamMember struct { MemberID int64; Source AttendanceSource }

func ResolveTeam(night Date, rules []Rule, overrides []Override, activeIDs map[int64]bool) []TeamMember
```

Algoritma:

1. Ambil aturan dengan `DayOfWeek == night.ISOWeekday()`, `From <= night`, dan (`To == nil` atau `night <= *To`), dan member aktif → source `SCHEDULED`.
2. Terapkan override **sesuai urutan input**: `ADD` menambah (source `OVERRIDE`) bila belum ada; `REMOVE` menghapus; `SWAP` menghapus `MemberID` lalu menambah `ReplacementID` (source `OVERRIDE`).
3. Tidak boleh ada duplikat. Urutkan hasil berdasarkan `MemberID` agar deterministik (urutan tampilan diurus di layer view).

Kasus uji wajib:

| Kasus | Harapan |
| --- | --- |
| 3 aturan Senin, tanggal Senin, tanpa override | 3 member `SCHEDULED` |
| Aturan Senin, tanggal Selasa | kosong |
| Aturan berakhir (`To`) sehari sebelum tanggal | tidak termasuk |
| Aturan mulai (`From`) sehari setelah tanggal | tidak termasuk |
| Member nonaktif | tidak termasuk |
| `ADD` member lain | bertambah 1, source `OVERRIDE` |
| `ADD` member yang sudah terjadwal | tidak duplikat, source tetap `SCHEDULED` |
| `REMOVE` member terjadwal | berkurang 1 |
| `SWAP` A → B | A hilang, B ada dengan source `OVERRIDE` |

### 3.5 Penutupan malam & denda — `closing.go` (F3.1–F3.4, Aturan bisnis)

```go
type CloseInput struct {
    Status          NightStatus
    HasReport       bool            // first_reported_at != nil
    PhotoCount      int
    MinPhotos       int
    FineRate        int
    Attendance      map[int64]AttendanceStatus // member_id -> status saat ini
}
type CloseResult struct {
    Status      NightStatus
    NeedsReview bool
    Attendance  map[int64]AttendanceStatus
    Fines       map[int64]int
}

func CloseNight(in CloseInput) CloseResult
func RecalculateFines(statuses map[int64]AttendanceStatus, fineRate int) map[int64]int
```

Aturan (urut dari atas, ambil yang pertama cocok):

| Kondisi | Status akhir | Absen | Denda | NeedsReview |
| --- | --- | --- | --- | --- |
| `Status == RAINED_OUT` | `RAINED_OUT` | semua `RAINED_OUT` | 0 | false |
| `Status == RAIN_REQUESTED` | `RAIN_REQUESTED` | tidak diubah | 0 untuk semua (menunggu admin) | true |
| `!HasReport` | `NO_REPORT` | semua `ABSENT` | `FineRate` per orang | false |
| `PhotoCount < MinPhotos` | `REPORTED` | tidak diubah | `FineRate` per `ABSENT` | true |
| lainnya | `REPORTED` | tidak diubah | `FineRate` per `ABSENT` | false |

`RecalculateFines`: `ABSENT` → `fineRate`, selainnya → 0. Dipakai juga oleh koreksi admin (Langkah 9).

Kasus uji: satu kasus untuk tiap baris tabel, ditambah kasus "laporan ada, 4 dari 4 hadir, 3 foto" → total denda 0.

### 3.6 Rekap bulanan — `recap.go` (F4.2–F4.6)

```go
type NightRow struct {
    Date   Date
    Status NightStatus
    Note   string
    Attendance []AttendanceRow
}
type AttendanceRow struct { MemberID int64; DisplayName string; Status AttendanceStatus; Fine int }

type MonthRecap struct {
    Year int; Month time.Month
    Summary  Summary
    Cards    []DayCard     // 7 kartu, Senin..Minggu; kartu tanpa malam tetap ada tapi kosong
    Timeline []TimelineItem // terbaru di atas
}
type Summary struct {
    NightsTotal, Reported, Rain, NoReport int
    PresentCount, SlotCount int  // SlotCount = PRESENT + ABSENT (hujan tidak dihitung)
    RatePercent int              // dibulatkan; 0 bila SlotCount 0
    TotalFine int
    FinedMembers int
}
type DayCard struct {
    Weekday int; Label string   // "Senin"
    Dates []Date                // malam dengan weekday ini yang sudah ditutup
    Rows []CardRow
    Subtotal int
}
type CardRow struct {
    DisplayName string
    Cells []AttendanceStatus    // satu per Dates; "" bila peserta tidak bertugas malam itu
    Fine int
}
type TimelineItem struct {
    Date Date; Weekday string
    Status NightStatus
    Present, Total int
    Note string
    Special bool                // catatan bukan sekadar "aman"
}

func BuildMonthRecap(year int, month time.Month, nights []NightRow) MonthRecap
func IsSpecialNote(note string) bool
```

Aturan:

- `nights` yang dioper **hanya malam yang sudah ditutup** (`closed_at` tidak NULL). Malam yang belum terjadi tidak tampil (F4.4).
- `Reported` menghitung status `REPORTED`; malam `RAIN_REQUESTED` dihitung sebagai `Reported` sampai dikonfirmasi admin.
- Baris di kartu diurutkan berdasarkan `DisplayName` (A–Z); peserta yang muncul di beberapa malam hari yang sama hanya satu baris.
- `IsSpecialNote`: setelah `strings.TrimSpace` + `strings.ToLower` + buang titik di akhir, bernilai `false` bila hasilnya `""` atau `"aman"`; selain itu `true`.
- Untuk malam `NO_REPORT`, teks linimasa: `Tidak ada laporan sampai pukul 06.00.`; untuk `RAINED_OUT`: catatan apa adanya, atau `Hujan, ronda ditiadakan.` bila kosong.

Kasus uji: buat fixture 1 bulan kecil (mis. 2 hari jaga, 4 malam: 1 lengkap, 1 sebagian, 1 hujan, 1 tanpa laporan) lalu cek semua angka `Summary`, isi satu `DayCard`, dan urutan `Timeline`.

**Checkpoint Langkah 3**

- [ ] `go test ./internal/domain/...` lolos dan cakupan file domain ≥ 85% (`go test -cover`).
- [ ] Tidak ada import `database`, `net/http`, atau pemanggilan `time.Now()` di `internal/domain`.

---

## Langkah 4 — Kerangka server & tampilan dasar

### 4.1 Router — `internal/httpx/router.go`

Middleware global (urutan): `middleware.RequestID`, `middleware.RealIP` (hanya bila `TRUST_PROXY=true`), `middleware.Logger`, `middleware.Recoverer`, `middleware.Compress(5)`, sesi `scs` (`LoadAndSave`), `nosurf` (CSRF).

Kelompok route:

```
/healthz                         GET
/static/*                        aset embed (Cache-Control: public, max-age=31536000 untuk file ber-hash; 1 jam untuk lainnya)
/                                GET → redirect 302 ke /recap
/patrol/tonight...               (Langkah 7) + httprate.LimitByIP(60, time.Minute)
/recap...                        (Langkah 10)
/admin/login                     GET, POST
/admin/...                       (Langkah 5, 6, 8, 9) + middleware RequireAdmin
```

CSRF untuk HTMX: di layout, tambahkan `hx-headers='{"X-CSRF-Token": "<token>"}'` pada `<body>`; konfigurasi `nosurf` agar membaca header `X-CSRF-Token`.

### 4.2 Layout templ — `internal/view/layout.templ`

- `<html lang="id">`, `<meta name="viewport" content="width=device-width, initial-scale=1">`, tema daisyUI terang.
- Memuat `/static/css/app.css`, `/static/js/htmx.min.js`, `/static/js/alpine.min.js` (atribut `defer`).
- Dua layout: `PublicLayout(title)` (mobile-first, lebar konten maks. 480px, header biru tua bertuliskan "PatrolHub · <rt_name>") dan `AdminLayout(title)` (navigasi: Malam, Anggota, Jadwal, Denda, Pengaturan, Keluar).
- Komponen bersama di `internal/view/components.templ`: `StatusDot(status)`, `StatusBadge(status)`, `FlashMessage(kind, text)`, `EmptyState(text)`.

Warna status (dipakai konsisten di semua halaman, sesuai mockup):

| Status | Penanda | Warna |
| --- | --- | --- |
| Hadir | lingkaran dengan centang | latar `#1E4FB8`, ikon putih |
| Tidak hadir | lingkaran dengan silang | latar `#C2410C`, ikon putih |
| Hujan | lingkaran dengan huruf "h" | latar `#DCE7F5`, teks `#1E3A6E` |

Penanda selalu punya `aria-label` (mis. "7 September: hadir"), sehingga makna tidak bergantung pada warna saja (F4.4).

### 4.3 Service pengaturan — `internal/service/settings.go`

`Settings` struct (RTName, WindowOpen, WindowClose `domain.Clock`, FinePerAbsence, MinPhotos, MaxPhotos int, GoLiveDate `*domain.Date`) dengan `Load(ctx)` dan `Save(ctx, Settings)`. Validasi saat simpan: jam format `HH:MM`, `open` > `close`, denda ≥ 0, `1 ≤ min_photos ≤ max_photos ≤ 20`.

**Checkpoint Langkah 4**

- [ ] Halaman kosong berlayout publik bisa dibuka di `/recap` (sementara boleh berisi teks "Segera hadir").
- [ ] Request POST tanpa token CSRF ditolak `403`.
- [ ] `make check` lolos.

---

## Langkah 5 — Login admin & pengaturan (F5.1, F5.3, F5.4)

### 5.1 Akun admin

1. Implementasi `patrolhub create-admin <username>`: baca password dua kali dari stdin (`golang.org/x/term` untuk menyembunyikan input), minimal 10 karakter, simpan hash `bcrypt` cost 12.
2. `GET /admin/login`: form username + password.
3. `POST /admin/login`: bandingkan dengan `bcrypt.CompareHashAndPassword`. Bila gagal tampilkan "Username atau password salah." (pesan sama untuk kedua kasus). Bila berhasil: `sessionManager.RenewToken`, simpan `admin_id`, redirect ke `/admin/nights`.
4. Rate limit login: `httprate.LimitByIP(10, time.Minute)`.
5. `POST /admin/logout`: hapus sesi, redirect ke `/admin/login`.
6. Middleware `RequireAdmin`: bila tidak ada `admin_id` di sesi → redirect ke `/admin/login`.
7. Cookie sesi: `HttpOnly`, `SameSite=Lax`, `Secure` bila `APP_ENV=production`, umur 14 hari.

### 5.2 Halaman pengaturan — `/admin/settings`

Form: nama RT, jam buka, jam tutup, denda per ketidakhadiran, minimal foto, maksimal foto, tanggal mulai aplikasi (`go_live_date`). Simpan lewat `Settings.Save`. Tampilkan pesan validasi di bawah field yang salah.

### 5.3 Cetak QR — `/admin/qr`

- Buat PNG QR berisi `BASE_URL + "/patrol/tonight"` dengan `go-qrcode` ukuran 512px, disajikan di `/admin/qr.png`.
- Halaman cetak ukuran A5: judul "Laporan Ronda <rt_name>", QR besar, teks "Pindai pukul 22.00–06.00 untuk mengisi laporan ronda", dan URL dalam teks biasa. CSS `@media print` menyembunyikan navigasi admin.

**Checkpoint Langkah 5**

- [ ] Admin bisa login, logout, dan halaman `/admin/*` lain me-redirect bila belum login.
- [ ] Pengaturan tersimpan dan validasi bekerja.
- [ ] Halaman QR bisa dicetak rapi di A5, QR bisa dipindai HP dan membuka `/patrol/tonight`.

---

## Langkah 6 — Anggota & jadwal (F1.1–F1.5)

### 6.1 Anggota — `/admin/members`

- Tabel: Nama tampilan (`DisplayName`), Blok, No. HP, Status (Aktif/Nonaktif), tombol Ubah.
- Form tambah/ubah: Blok rumah (wajib; simpan sebagai `strings.ToUpper(strings.TrimSpace(input))`, mis. ` c-10 ` → `C-10`), Nama (opsional), No. HP (opsional), Catatan.
- Tombol Nonaktifkan/Aktifkan (bukan hapus). Member nonaktif tidak masuk tim baru, tapi tetap tampil di rekap lama.

### 6.2 Jadwal tetap — `/admin/schedule`

- Tampilan 7 kolom/blok (Senin–Minggu); tiap blok berisi daftar peserta dengan aturan aktif hari itu.
- Form "Tambah ke jadwal": pilih peserta, pilih hari, tanggal mulai berlaku (bawaan: hari ini).
- Tombol "Keluarkan dari jadwal" mengisi `effective_to = kemarin` (bukan menghapus baris), supaya malam lama tidak berubah.

### 6.3 Perubahan per tanggal — `/admin/schedule/overrides`

- Form: tanggal malam, aksi (Tambah / Keluarkan / Tukar), peserta, peserta pengganti (hanya untuk Tukar), catatan.
- Daftar perubahan 30 hari ke depan dan 7 hari ke belakang, dengan tombol hapus (hanya untuk malam yang belum ditutup).
- Tampilkan pratinjau tim (pakai `domain.ResolveTeam`) untuk tanggal yang dipilih sebelum disimpan.

### 6.4 Data contoh — `patrolhub seed-demo`

Isi peserta dan jadwal mengikuti spreadsheet contoh (nama + blok bebas), mis. Senin: Dhani, Yusuf, Adytya, Ugi; Selasa: Gemilang, Wasis, C-10, E-10; dst. Tujuannya mempermudah uji manual.

### 6.5 Pratinjau jadwal sebulan (Should, F1.5)

`/admin/schedule/preview?month=YYYY-MM`: daftar tanggal 1–akhir bulan beserta tim hasil `ResolveTeam`.

**Checkpoint Langkah 6**

- [ ] Admin bisa menambah peserta tanpa nama (hanya blok) dan tampil sebagai blok saja.
- [ ] Tukar jadwal satu malam terlihat benar di pratinjau tim.
- [ ] Mengeluarkan peserta dari jadwal tidak mengubah malam yang sudah ditutup.

---

## Langkah 7 — Halaman laporan malam (F2.1–F2.11, F3.3 sisi pelapor)

### 7.1 Service — `internal/service/patrol.go`

```go
type ReportContext struct { IP, UserAgent string }

func (s *PatrolService) Tonight(ctx, now time.Time) (TonightView, error)
func (s *PatrolService) ToggleAttendance(ctx, now, memberID int64, present bool, rc ReportContext) error
func (s *PatrolService) UpdateSituation(ctx, now, note string, rc ReportContext) error
func (s *PatrolService) AddPhoto(ctx, now, file io.Reader, size int64, lat, lng *float64, rc ReportContext) (Photo, error)
func (s *PatrolService) RequestRain(ctx, now, rc ReportContext) error
func (s *PatrolService) AddWalkIn(ctx, now, memberID int64, rc ReportContext) error // Should, F2.10
```

Aturan yang berlaku untuk **semua** method di atas:

1. Hitung `w := domain.CurrentWindow(now, settings.WindowOpen, settings.WindowClose)`. Bila `!w.Open` → kembalikan `ErrWindowClosed` (handler membalas `409` dan merender ulang halaman "tertutup").
2. Pastikan baris malam ada: `InsertNightIfMissing(w.NightDate)` lalu `GetNightByDate`.
3. Bila malam sudah `closed_at` atau berstatus `RAINED_OUT` → `ErrNightClosed`.
4. **Sinkronkan tim** (hanya di `Tonight` dan sebelum aksi tulis): hitung `ResolveTeam`, `UpsertAttendance` untuk anggota tim yang belum punya baris (status `ABSENT`), lalu `DeleteAbsentAttendanceNotIn` untuk anggota yang sudah dikeluarkan admin dan masih `ABSENT` (baris `PRESENT` dan `WALK_IN` tidak pernah dihapus).
5. Setiap aksi tulis: `MarkReported` (mengisi `first_reported_at` bila masih NULL) dan `InsertAudit` dengan `actor = 'ANONYMOUS'`, IP, user agent, dan `detail` JSON (mis. `{"member_id": 12, "present": true}`).
6. Semua langkah tulis dalam satu transaksi (`WithTx`).

Aturan khusus:

- `ToggleAttendance`: member harus ada di attendance malam itu, bila tidak → `ErrNotInTeam` (`400`).
- `UpdateSituation`: trim; maksimal 1.000 karakter (lebih → `ErrTooLong`, `422`).
- `AddPhoto`: tolak bila jumlah foto sudah `max_photos` (`ErrTooManyPhotos`, `422`); ukuran > 1 MB ditolak; `http.DetectContentType` dari 512 byte pertama harus `image/jpeg` atau `image/webp`. `taken_at = now`.
- `RequestRain`: set `status = RAIN_REQUESTED`, `rain_requested_at = now`. Boleh dibatalkan? **Tidak** di MVP; tombol hilang setelah diajukan dan muncul teks "Hujan diajukan, menunggu konfirmasi pengurus."

### 7.2 Penyimpanan foto — `internal/storage/photos.go`

```go
type PhotoStore interface {
    Save(ctx context.Context, key string, r io.Reader) error
    Open(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
}
```

Implementasi MVP: `LocalStore{Dir string}` menulis ke `PHOTO_DIR/<key>`. Format key: `YYYY/MM/DD/<uuid>.jpg` memakai tanggal malam. Tulis ke file sementara lalu `os.Rename` agar tidak ada file setengah jadi. (Penyimpanan S3/MinIO bisa ditambah nanti dengan interface yang sama.)

### 7.3 Route & handler

| Method & path | Respons |
| --- | --- |
| `GET /patrol/tonight` | Halaman penuh (terbuka atau tertutup) |
| `POST /patrol/tonight/attendance/{memberID}` | form `present=true|false` → fragmen baris anggota |
| `POST /patrol/tonight/situation` | form `note` → fragmen kecil "Tersimpan pukul 22:41" |
| `POST /patrol/tonight/photos` | multipart `photo`, `lat`, `lng` → fragmen daftar thumbnail + penghitung foto |
| `GET /patrol/tonight/photos/{id}/thumb` | gambar, **hanya** untuk foto malam yang sedang terbuka (pelapor perlu melihat hasilnya); setelah malam ditutup → `404` |
| `POST /patrol/tonight/rain` | fragmen status hujan |
| `POST /patrol/tonight/walk-in` | (Should) fragmen daftar tim |

Semua respons fragmen dikirim dengan `Cache-Control: no-store`.

### 7.4 Tampilan — `internal/view/patrol_tonight.templ`

Mengikuti sketsa di PRD, dari atas ke bawah:

1. Judul "Laporan Ronda <rt_name>" dan "Tanggal: <FormatDateID(nightDate)>".
2. **Tim Ronda**: satu baris per anggota (urut `DisplayName`), seluruh baris bisa diketuk (tinggi minimal 48px) dengan checkbox besar di kanan. Atribut checkbox:
   `hx-post="/patrol/tonight/attendance/{id}" hx-trigger="change" hx-vals='{"present": <checked>}' hx-target="closest li" hx-swap="outerHTML"`. Bila request gagal, kembalikan checkbox ke posisi semula dan tampilkan pesan "Gagal menyimpan, coba lagi."
3. **Situasi**: `<textarea maxlength="1000">` dengan `hx-post="/patrol/tonight/situation" hx-trigger="keyup changed delay:800ms, blur"` dan indikator "Tersimpan pukul …".
4. **Foto**: penghitung "2 dari minimal 3 foto", tombol "Ambil Foto" (lihat 7.5), dan grid thumbnail.
5. Tombol sekunder "Ajukan Hujan" dengan `hx-confirm="Ajukan malam ini sebagai hujan? Pengurus akan mengonfirmasi."`.
6. (Should) Tombol "Tambah anggota pengganti" membuka daftar peserta aktif yang belum ada di tim.

Halaman **tertutup** (di luar jendela): judul yang sama, teks "Laporan dibuka pukul 22.00", lalu daftar tim malam berikutnya (hasil `ResolveTeam(w.NightDate)`) tanpa kontrol apa pun.

### 7.5 JavaScript foto — `web/static/js/photo.js`

Ukuran file ± 60 baris, tanpa library:

```html
<input id="photo-input" type="file" accept="image/*" capture="environment" hidden>
<button type="button" onclick="document.getElementById('photo-input').click()">Ambil Foto</button>
```

Saat `change`:

1. Baca file dengan `createImageBitmap(file)`.
2. Gambar ke `<canvas>` dengan sisi terpanjang maksimal 1280px (pertahankan rasio).
3. `canvas.toBlob(cb, 'image/jpeg', 0.7)`; bila hasil > 500 KB ulangi dengan kualitas 0.5.
4. Coba ambil lokasi dengan `navigator.geolocation.getCurrentPosition` (timeout 5 detik, **abaikan bila gagal**, F2.9).
5. Kirim `FormData` (`photo`, `lat`, `lng`) via `fetch` ke `/patrol/tonight/photos` dengan header `X-CSRF-Token` dan `HX-Request: true`; tampilkan status "Mengunggah…" di tombol.
6. Ganti isi `#photo-list` dengan HTML respons, lalu panggil `htmx.process(document.getElementById('photo-list'))`.
7. Reset `input.value = ''` agar foto berikutnya tetap memicu `change`.

**Checkpoint Langkah 7** (uji manual memakai HP sungguhan di jaringan lokal)

- [ ] Di luar jam buka tampil halaman tertutup beserta tim malam berikutnya.
- [ ] Pukul 00.30 laporan tercatat untuk malam **sebelumnya** (uji dengan mengubah jam buka/tutup di pengaturan, bukan jam sistem).
- [ ] Toggle hadir tersimpan tanpa reload, dan tetap benar setelah halaman di-refresh.
- [ ] Foto dari kamera belakang terunggah < 500 KB dan muncul sebagai thumbnail; foto ke-11 ditolak.
- [ ] Setiap aksi tercatat di `audit_log` lengkap dengan IP.

---

## Langkah 8 — Job penutupan malam (F3.1, F3.2)

File `internal/job/closer.go`. Dijalankan dari `serve` sebagai goroutine:

```go
func (c *Closer) Run(ctx context.Context) {
    c.tick(ctx, time.Now())                 // sekali saat start
    t := time.NewTicker(5 * time.Minute)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case now := <-t.C: c.tick(ctx, now)
        }
    }
}
```

`tick(now)`:

1. Baca pengaturan. Bila `go_live_date` kosong → tidak melakukan apa-apa.
2. **Isi malam yang terlewat:** untuk setiap tanggal `D` dari `max(go_live_date, hari ini − 14)` sampai `DateOf(now)`, bila `domain.ClosesAt(D, close) <= now` dan malam `D` belum ada → `InsertNightIfMissing(D)` lalu sinkronkan tim (`ResolveTeam` + `UpsertAttendance`). Ini menangani malam yang halamannya tidak pernah dibuka sama sekali.
3. **Tutup malam:** ambil `ListUnclosedNightsBefore(...)` yang `ClosesAt <= now`. Untuk tiap malam, dalam satu transaksi:
   - `GetNightByDateForUpdate` (kunci baris; lewati bila ternyata `closed_at` sudah terisi).
   - Hitung `domain.CloseNight(...)` dengan `FineRate = settings.FinePerAbsence`, `PhotoCount = CountPhotosByNight`.
   - Simpan status absen dan denda tiap anggota, lalu `CloseNight(id, status, fine_rate, needs_review)` dengan `closed_at = now`.
   - `InsertAudit` dengan `actor = 'SYSTEM'`, `action = 'close_night'`.
4. Log jumlah malam yang dibuat dan ditutup. Error di satu malam tidak boleh menghentikan malam lain.

Job harus **idempoten**: dijalankan dua kali berturut-turut tidak mengubah apa pun.

**Checkpoint Langkah 8**

- [ ] Test integrasi (memakai database dev): malam dengan laporan + 3 foto → `REPORTED`, denda hanya untuk `ABSENT`.
- [ ] Malam tanpa laporan sama sekali → `NO_REPORT`, semua anggota didenda.
- [ ] Malam `RAIN_REQUESTED` → ditutup tanpa denda dan `needs_review = true`.
- [ ] Menjalankan `tick` dua kali hasilnya sama.

---

## Langkah 9 — Panel malam admin (F3.3–F3.5, F5.2)

### 9.1 Daftar malam — `/admin/nights?month=YYYY-MM`

Tabel per malam: tanggal, status (badge), hadir/total, jumlah foto, penanda "Perlu ditinjau". Filter: "Hanya yang perlu ditinjau". Urut terbaru di atas. Malam `RAIN_REQUESTED` selalu di paling atas dengan tombol cepat "Konfirmasi hujan" dan "Tolak".

### 9.2 Detail malam — `/admin/nights/{date}`

1. Status malam, waktu laporan pertama, waktu penutupan, tarif denda.
2. Daftar anggota dengan status dan denda; tiap baris punya tombol "Koreksi".
3. Catatan situasi.
4. Galeri foto (gambar disajikan lewat `/admin/photos/{id}`, hanya untuk admin; header `Cache-Control: private`), dengan jam ambil dan tautan ke Google Maps bila ada koordinat.
5. Riwayat `audit_log` malam itu.

### 9.3 Aksi admin

| Aksi | Aturan |
| --- | --- |
| **Tandai hujan** (sebelum atau sesudah ditutup) | Status → `RAINED_OUT`, semua absen → `RAINED_OUT`, semua denda → 0, `needs_review = false`. |
| **Konfirmasi hujan** (untuk `RAIN_REQUESTED`) | Sama seperti "Tandai hujan". |
| **Tolak hujan** | Status → `REPORTED`. Bila malam sudah ditutup: hitung denda dengan `RecalculateFines(statuses, night.fine_rate)` (bila `fine_rate` NULL, isi dengan pengaturan sekarang). Bila belum ditutup: biarkan job yang menghitung. |
| **Koreksi absen** | Form status baru + **alasan wajib** (minimal 5 karakter). Simpan, lalu bila malam sudah ditutup hitung ulang denda baris itu memakai `night.fine_rate`. |

Semua aksi dalam transaksi dan dicatat di `audit_log` dengan `actor = 'admin:<id>'` dan `reason`.

### 9.4 Denda & pelunasan — `/admin/fines/{yyyy}/{mm}` (Should, F3.5)

Tabel peserta yang punya denda di bulan itu: total denda, total dibayar (dari `fine_payment` dengan `period = YYYY-MM`), sisa, tombol "Catat pembayaran" (nominal bawaan = sisa, tanggal bayar bawaan = hari ini).

**Checkpoint Langkah 9**

- [ ] Konfirmasi hujan menghapus semua denda malam itu dan rekap publik ikut berubah.
- [ ] Koreksi tanpa alasan ditolak; koreksi dengan alasan tercatat di riwayat.
- [ ] Foto tidak bisa dibuka tanpa login admin (uji di jendela incognito).

---

## Langkah 10 — Rekap bulanan publik (F4.1–F4.6)

### 10.1 Service — `internal/service/recap.go`

`MonthRecap(ctx, year, month)`: ambil malam yang `closed_at IS NOT NULL` di bulan itu beserta absen (`ListNightsBetween` + `ListAttendanceBetween`), ubah ke `[]domain.NightRow` (isi `DisplayName`), lalu `domain.BuildMonthRecap`. Bulan di masa depan → `404`.

### 10.2 Route

| Path | Isi |
| --- | --- |
| `GET /recap` | redirect 302 ke bulan berjalan |
| `GET /recap/{yyyy}/{mm}` | tab **Ringkasan** |
| `GET /recap/{yyyy}/{mm}/situations` | tab **Situasi**; query `?special=1` untuk filter (Should, F4.6) |

Respons publik boleh di-cache singkat: `Cache-Control: public, max-age=300`.

### 10.3 Tampilan Ringkasan — `internal/view/recap_summary.templ`

Ikuti mockup "Ringkasan + kartu per hari jaga":

1. Header: "PatrolHub · <rt_name>", judul **"Rekap Ronda"**, pemilih bulan (tombol ‹ dan ›, label "September 2026"; tombol › nonaktif di bulan berjalan). `<title>` halaman: "Rekap Ronda September 2026".
2. Tab: **Ringkasan** | **Situasi** (tautan biasa, tab aktif diberi `aria-current="page"`).
3. Ringkasan (grid 2 kolom):
   - "Malam terlapor": `Reported/NightsTotal`, sub-teks "`Rain` hujan · `NoReport` tanpa laporan".
   - "Kehadiran": `RatePercent%`, sub-teks "`PresentCount` dari `SlotCount` jadwal".
   - Kartu lebar (latar gelap): "Total denda September", `FormatRupiah(TotalFine)`, sub-teks "`FinedMembers` peserta kena denda · Rp 5.000 / tidak hadir" (pakai tarif dari pengaturan).
4. Legenda: Hadir, Tidak hadir, Hujan.
5. Tujuh kartu hari jaga: judul hari + "`len(Dates)` malam jaga"; baris kepala berisi tanggal; tiap baris peserta: nama tampilan, satu `StatusDot` per tanggal (kosong bila tidak bertugas), denda di kanan (`–` bila 0); footer "Subtotal tim". Kartu tanpa malam menampilkan "Belum ada malam jaga bulan ini."
6. Bila belum ada malam yang ditutup di bulan itu: `EmptyState("Belum ada data untuk bulan ini.")`.

### 10.4 Tampilan Situasi — `internal/view/recap_situations.templ`

Ikuti mockup "Linimasa situasi": tombol filter "Semua malam" / "Ada catatan khusus" (tautan dengan `?special=1`); daftar kartu per malam terbaru di atas, berisi tanggal besar + singkatan hari, badge (`x/y hadir`, "Hujan", atau "Tanpa laporan"), badge "Catatan khusus" bila `Special`, lalu teks catatan. **Foto tidak pernah ditampilkan.**

**Checkpoint Langkah 10**

- [ ] Dengan data `seed-demo` + beberapa malam yang sudah ditutup, angka ringkasan sama dengan hitungan manual.
- [ ] Halaman nyaman dipakai di lebar 360px tanpa scroll horizontal.
- [ ] Ukuran halaman (HTML + CSS + JS, tanpa cache) < 200 KB.
- [ ] Tidak ada nomor HP atau foto di HTML publik (cek *view source*).

---

## Langkah 11 — Fitur "Should" yang tersisa

Kerjakan hanya setelah Langkah 1–10 selesai. Urutan disarankan:

### 11.1 Salin rekap untuk WhatsApp (F4.7)

Tombol "Salin rekap" di tab Ringkasan dengan dua pilihan: **Bulan ini** dan **7 malam terakhir**. Teks dibuat di server (`GET /recap/{yyyy}/{mm}/text?range=month|week`, `Content-Type: text/plain`) lalu disalin dengan `navigator.clipboard.writeText` (Alpine.js), disertai notifikasi "Rekap disalin".

Format bulanan:

```
*Rekap Ronda RT 02 — September 2026*
Malam terlapor: 26/30 (hujan 3, tanpa laporan 1)
Kehadiran: 78%
Total denda: Rp 180.000

Denda per peserta:
- Ugi (B-4): Rp 20.000
- C-15: Rp 15.000
...

Detail: https://ronda.example.com/recap/2026/09
```

Daftar denda diurutkan berdasarkan nama tampilan (A–Z), bukan besar denda, dan hanya memuat peserta dengan denda > 0. Format mingguan sama, tapi judulnya "7 malam terakhir (1–7 September 2026)".

### 11.2 Ekspor CSV (F5.5)

`GET /admin/export/{yyyy}/{mm}.csv` dengan kolom: `tanggal, hari, status_malam, blok, nama, status_absen, denda`. Pakai `encoding/csv`, pemisah koma, header `Content-Disposition: attachment; filename="rekap-ronda-2026-09.csv"`, dan BOM UTF-8 di awal file agar terbaca benar di Excel.

### 11.3 Sisanya

GPS (F2.9) dan anggota pengganti (F2.10) sudah tercakup di Langkah 7; pratinjau jadwal (F1.5) di Langkah 6.5; pelunasan denda (F3.5) di Langkah 9.4. Pastikan semuanya selesai dan teruji.

---

## Langkah 12 — Kebutuhan non-fungsional

### 12.1 Retensi foto (6 bulan)

Job kedua di `internal/job/retention.go`, jalan sekali sehari (ticker 24 jam, plus sekali saat start): ambil `ListPhotosOlderThan(now − 183 hari)`, hapus file lewat `PhotoStore.Delete`, lalu hapus barisnya. Error penghapusan file dicatat ke log, baris database tetap dihapus bila file memang sudah tidak ada (`os.IsNotExist`).

### 12.2 Keamanan

- [ ] Rate limit `/patrol/tonight/*` 60 request/menit per IP; login admin 10/menit.
- [ ] `http.MaxBytesReader` 1 MB untuk unggah foto, 64 KB untuk form lain.
- [ ] Header keamanan via middleware: `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, `X-Frame-Options: DENY`.
- [ ] Semua output templ otomatis di-escape; jangan pernah memakai `templ.Raw` untuk input pengguna.
- [ ] Bila `TRUST_PROXY=false`, abaikan header `X-Forwarded-For`.

### 12.3 Logging

Pakai `log/slog` dengan output JSON di production dan teks di development. Jangan pernah mencatat isi password, cookie sesi, atau nomor HP.

### 12.4 Backup

Skrip `deploy/backup.sh` yang dijalankan cron harian di server:

```bash
#!/bin/sh
set -e
STAMP=$(date +%Y%m%d)
docker compose exec -T db pg_dump -U patrolhub -Fc patrolhub > /backup/patrolhub-$STAMP.dump
tar -czf /backup/photos-$STAMP.tar.gz -C /srv/patrolhub data/photos
find /backup -type f -mtime +30 -delete
```

Uji pemulihan minimal sekali dengan `pg_restore` ke database kosong sebelum rilis.

---

## Langkah 13 — Docker & deploy

### 13.1 Dockerfile (multi-stage)

```dockerfile
FROM golang:1-bookworm AS build
WORKDIR /src
RUN go install github.com/a-h/templ/cmd/templ@latest \
 && go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN templ generate && sqlc generate \
 && ./bin/tailwindcss -i web/styles/input.css -o web/static/css/app.css --minify \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/patrolhub ./cmd/patrolhub

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/patrolhub /patrolhub
ENV HTTP_ADDR=:8080 PHOTO_DIR=/data/photos
EXPOSE 8080
ENTRYPOINT ["/patrolhub", "serve"]
```

Catatan: binary Tailwind di `bin/tailwindcss` harus versi Linux x64 saat build di Docker; tambahkan langkah unduh di Dockerfile bila binary di repo untuk OS lain.

### 13.2 docker-compose produksi — `deploy/docker-compose.yml`

Layanan: `db` (postgres:16, volume data), `app` (image di atas, `env_file: .env`, volume `./data/photos:/data/photos`, `depends_on: db`), dan reverse proxy **Caddy** untuk HTTPS otomatis ke `app:8080`. HTTPS wajib karena izin lokasi (GPS) di browser hanya jalan di HTTPS.

### 13.3 Langkah rilis pertama

1. Salin `deploy/` dan `.env` ke server, isi `BASE_URL` dengan domain sebenarnya, `APP_ENV=production`.
2. `docker compose up -d`.
3. `docker compose exec app /patrolhub create-admin <username>`.
4. Login, isi Pengaturan (termasuk `go_live_date` = tanggal mulai dipakai), masukkan peserta dan jadwal.
5. Cetak QR dari `/admin/qr` dan tempel di pos ronda.
6. Pasang cron backup.

**Checkpoint Langkah 13**

- [ ] Image < 50 MB, container jalan sebagai non-root.
- [ ] Restart container tidak menghilangkan foto maupun data.
- [ ] Situs dapat diakses via HTTPS dari HP.

---

## Langkah 14 — Checklist penerimaan akhir

Centang semua sebelum MVP dinyatakan selesai. Kolom "Cara uji" menjelaskan uji manual minimal.

| ID PRD | Cara uji |
| --- | --- |
| F1.1 | Tambah peserta tanpa nama → tampil sebagai blok saja; nonaktifkan → tidak muncul di tim baru, tetap ada di rekap lama. |
| F1.2–F1.4 | Atur jadwal Senin, tambah tukar untuk Senin depan → halaman laporan malam itu memuat tim hasil tukar. |
| F2.1–F2.3 | Ubah jam buka ke 5 menit dari sekarang, tunggu, pindai QR → halaman terbuka untuk tanggal yang benar. |
| F2.5–F2.8 | Centang 2 anggota, isi situasi, ambil 3 foto → refresh, semua tetap tersimpan; penghitung foto "3 dari minimal 3". |
| F2.11 | Ada entri `audit_log` untuk setiap aksi di atas. |
| F3.1–F3.2 | Setelah jam tutup + ≤ 5 menit, malam tertutup otomatis; malam tanpa laporan → semua anggota didenda. |
| F3.3 | Ajukan hujan dari HP → admin konfirmasi → denda malam itu 0, rekap menampilkan "h". |
| F3.4 | Ubah tarif denda → denda malam yang sudah ditutup tidak berubah. |
| F4.1–F4.5 | Rekap bulan berjalan menampilkan ringkasan, 7 kartu hari jaga, dan linimasa situasi tanpa foto. |
| F5.1–F5.4 | Login, ubah pengaturan, koreksi absen dengan alasan, cetak QR. |
| Non-fungsional | Halaman laporan dan rekap < 200 KB, nyaman di layar 360px, data waktu sesuai WIB walau server memakai UTC. |

---

## Lampiran — Bila waktu terbatas

Semua kebutuhan **Must** ada di Langkah 1–10 dan 13 (tanpa 6.5, 9.4, dan filter `?special=1` di 10.4). Kerjakan itu dulu sampai rilis. Fitur **Should** (pratinjau jadwal sebulan, GPS, anggota pengganti, pelunasan denda, filter catatan khusus, salin rekap, ekspor CSV) bisa menyusul setelah aplikasi mulai dipakai warga.
