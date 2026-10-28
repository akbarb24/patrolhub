-- +goose Up
-- share_token: secret link to a night's photos, sent with the WhatsApp report.
-- wa_sent_at: when the report went to the WhatsApp group (NULL = not sent yet).
ALTER TABLE patrol_night
    ADD COLUMN share_token TEXT UNIQUE,
    ADD COLUMN wa_sent_at  TIMESTAMPTZ;

-- +goose Down
ALTER TABLE patrol_night DROP COLUMN share_token, DROP COLUMN wa_sent_at;
