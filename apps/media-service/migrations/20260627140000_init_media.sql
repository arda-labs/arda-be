-- +goose Up
CREATE TABLE IF NOT EXISTS media_files (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  org_id TEXT,
  owner_user_id TEXT,
  module TEXT NOT NULL,
  entity_type TEXT,
  entity_id TEXT,
  original_filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  extension TEXT,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  checksum_sha256 TEXT,
  status TEXT NOT NULL,
  scan_status TEXT NOT NULL DEFAULT 'not_required',
  storage_provider TEXT NOT NULL,
  bucket TEXT NOT NULL,
  object_key TEXT NOT NULL,
  storage_class TEXT NOT NULL DEFAULT 'standard',
  version_id TEXT NOT NULL DEFAULT 'v1',
  visibility TEXT NOT NULL DEFAULT 'private',
  created_by TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  uploaded_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  UNIQUE (storage_provider, bucket, object_key)
);

CREATE INDEX IF NOT EXISTS idx_media_files_tenant_entity
  ON media_files (tenant_id, entity_type, entity_id)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_media_files_tenant_status
  ON media_files (tenant_id, status)
  WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS media_file_links (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  file_id TEXT NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  relation_type TEXT NOT NULL DEFAULT 'attachment',
  created_by TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, file_id, entity_type, entity_id, relation_type)
);

CREATE INDEX IF NOT EXISTS idx_media_file_links_entity
  ON media_file_links (tenant_id, entity_type, entity_id);

CREATE TABLE IF NOT EXISTS media_derivatives (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  file_id TEXT NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  content_type TEXT NOT NULL,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  storage_provider TEXT NOT NULL,
  bucket TEXT NOT NULL,
  object_key TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (file_id, kind)
);

CREATE TABLE IF NOT EXISTS media_upload_sessions (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  file_id TEXT NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
  provider_upload_id TEXT,
  upload_type TEXT NOT NULL DEFAULT 'single_part',
  expires_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL,
  created_by TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_media_upload_sessions_file
  ON media_upload_sessions (file_id);

CREATE TABLE IF NOT EXISTS media_storage_providers (
  id TEXT PRIMARY KEY,
  code TEXT NOT NULL UNIQUE,
  type TEXT NOT NULL,
  endpoint TEXT,
  region TEXT,
  bucket_prefix TEXT,
  is_active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS media_replication_jobs (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  file_id TEXT NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
  source_provider TEXT NOT NULL,
  target_provider TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS media_outbox_events (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  payload JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  next_retry_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_media_outbox_events_pending
  ON media_outbox_events (status, next_retry_at, created_at);

-- +goose Down
DROP TABLE IF EXISTS media_outbox_events;
DROP TABLE IF EXISTS media_replication_jobs;
DROP TABLE IF EXISTS media_storage_providers;
DROP TABLE IF EXISTS media_upload_sessions;
DROP TABLE IF EXISTS media_derivatives;
DROP TABLE IF EXISTS media_file_links;
DROP TABLE IF EXISTS media_files;
