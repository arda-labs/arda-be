-- +goose Up
ALTER TABLE iam_users ADD COLUMN IF NOT EXISTS password_hash VARCHAR(512);
ALTER TABLE iam_users ADD COLUMN IF NOT EXISTS source VARCHAR(50) NOT NULL DEFAULT 'internal';

-- Set the admin password (bcrypt hash of "admin123")
UPDATE iam_users SET password_hash = '$2a$12$LJ3m4ys3Lk0TSwHlvS.JJOvc5sx5GQJfKPdKR0MJfN.ZcJKW5K7iW' WHERE username = 'admin';

-- +goose Down
ALTER TABLE iam_users DROP COLUMN IF EXISTS password_hash;
ALTER TABLE iam_users DROP COLUMN IF EXISTS source;
