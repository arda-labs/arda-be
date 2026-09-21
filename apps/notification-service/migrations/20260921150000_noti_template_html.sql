-- +goose Up

-- Optional HTML body for email templates. Templates authored in an external
-- email builder are pasted/imported as body_html; `body` stays the plain-text
-- alternative (and the only body for non-email channels). The delivery worker
-- renders both and the SMTP mailer sends multipart/alternative when HTML exists.

ALTER TABLE noti_templates
    ADD COLUMN IF NOT EXISTS body_html TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE noti_templates
    DROP COLUMN IF EXISTS body_html;
