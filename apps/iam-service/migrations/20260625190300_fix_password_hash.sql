-- +goose Up
-- Reset admin password to a known bcrypt hash of "admin123"
-- Generated fresh to ensure correctness
UPDATE iam_users
SET password_hash = '$2a$12$LJ3m4ys3Lk0TSwHlvS.JJOvc5sx5GQJfKPdKR0MJfN.ZcJKW5K7iW'
WHERE username = 'admin'
  AND password_hash IS NULL;

-- Add a test user
INSERT INTO iam_users (id, external_subject, username, email, display_name, password_hash, source, status, tenant_id)
VALUES (uuidv7(), 'test-user', 'test', 'test@arda.local', 'Test User', '$2a$12$LJ3m4ys3Lk0TSwHlvS.JJOvc5sx5GQJfKPdKR0MJfN.ZcJKW5K7iW', 'internal', 'ACTIVE', 'default')
ON CONFLICT (username) DO NOTHING;

-- +goose Down
-- No-op
SELECT 1;
