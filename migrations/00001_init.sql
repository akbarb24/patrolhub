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
