-- +goose Up
ALTER TABLE iam_users
    ADD COLUMN IF NOT EXISTS kratos_identity_id VARCHAR(255);

CREATE UNIQUE INDEX IF NOT EXISTS idx_iam_users_kratos_identity_id
    ON iam_users(kratos_identity_id)
    WHERE kratos_identity_id IS NOT NULL AND kratos_identity_id <> '';

UPDATE iam_users u
SET kratos_identity_id = m.external_id
FROM iam_identity_mappings m
WHERE m.provider_id = 'kratos'
  AND m.internal_user_id = u.id
  AND COALESCE(u.kratos_identity_id, '') = ''
  AND COALESCE(m.external_id, '') <> '';

UPDATE iam_users
SET kratos_identity_id = external_subject
WHERE source = 'kratos'
  AND COALESCE(kratos_identity_id, '') = ''
  AND COALESCE(external_subject, '') <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_iam_users_kratos_identity_id;
ALTER TABLE iam_users
    DROP COLUMN IF EXISTS kratos_identity_id;
