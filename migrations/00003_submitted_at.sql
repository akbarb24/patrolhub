-- +goose Up
-- submitted_at: when the reporter pressed "Kirim Laporan". Only a submitted night
-- counts as reported at close; after it the report page is read-only.
ALTER TABLE patrol_night ADD COLUMN submitted_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE patrol_night DROP COLUMN submitted_at;
