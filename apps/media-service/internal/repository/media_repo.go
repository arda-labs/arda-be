package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/arda-labs/arda/apps/media-service/internal/domain"
)

type MediaRepository struct {
	db *sql.DB
}

func NewMediaRepository(db *sql.DB) *MediaRepository {
	return &MediaRepository{db: db}
}

func (r *MediaRepository) CreatePendingUpload(ctx context.Context, file domain.File, session domain.UploadSession, eventPayload map[string]any) (domain.File, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.File{}, err
	}
	defer tx.Rollback()

	const insertFile = `
INSERT INTO media_files (
  id, tenant_id, org_id, owner_user_id, module, entity_type, entity_id,
  original_filename, content_type, extension, size_bytes, checksum_sha256,
  status, scan_status, storage_provider, bucket, object_key, storage_class,
  version_id, visibility, created_by
) VALUES (
  $1,$2,NULLIF($3,''),NULLIF($4,''),$5,NULLIF($6,''),NULLIF($7,''),
  $8,$9,NULLIF($10,''),$11,NULLIF($12,''),$13,$14,$15,$16,$17,$18,$19,$20,NULLIF($21,'')
)
RETURNING created_at`
	if err := tx.QueryRowContext(ctx, insertFile,
		file.ID, file.TenantID, file.OrgID, file.OwnerUserID, file.Module, file.EntityType, file.EntityID,
		file.OriginalFilename, file.ContentType, file.Extension, file.SizeBytes, file.ChecksumSHA256,
		file.Status, file.ScanStatus, file.StorageProvider, file.Bucket, file.ObjectKey, file.StorageClass,
		file.VersionID, file.Visibility, file.CreatedBy,
	).Scan(&file.CreatedAt); err != nil {
		return domain.File{}, fmt.Errorf("insert media file: %w", err)
	}

	const insertSession = `
INSERT INTO media_upload_sessions (
  id, tenant_id, file_id, upload_type, expires_at, status, created_by
) VALUES ($1,$2,$3,'single_part',$4,$5,NULLIF($6,''))`
	if _, err := tx.ExecContext(ctx, insertSession, session.ID, session.TenantID, session.FileID, session.ExpiresAt, session.Status, file.CreatedBy); err != nil {
		return domain.File{}, fmt.Errorf("insert upload session: %w", err)
	}

	if err := insertOutbox(ctx, tx, file.TenantID, "media.upload.initiated", "media_file", file.ID, eventPayload); err != nil {
		return domain.File{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.File{}, err
	}
	return file, nil
}

func (r *MediaRepository) GetFile(ctx context.Context, fileID string) (domain.File, error) {
	const query = `
SELECT id, tenant_id, COALESCE(org_id,''), COALESCE(owner_user_id,''), module,
  COALESCE(entity_type,''), COALESCE(entity_id,''), original_filename, content_type,
  COALESCE(extension,''), size_bytes, COALESCE(checksum_sha256,''), status, scan_status,
  storage_provider, bucket, object_key, storage_class, version_id, visibility,
  COALESCE(created_by,''), created_at, uploaded_at
FROM media_files
WHERE id = $1 AND deleted_at IS NULL`
	var file domain.File
	err := r.db.QueryRowContext(ctx, query, fileID).Scan(
		&file.ID, &file.TenantID, &file.OrgID, &file.OwnerUserID, &file.Module,
		&file.EntityType, &file.EntityID, &file.OriginalFilename, &file.ContentType,
		&file.Extension, &file.SizeBytes, &file.ChecksumSHA256, &file.Status, &file.ScanStatus,
		&file.StorageProvider, &file.Bucket, &file.ObjectKey, &file.StorageClass, &file.VersionID, &file.Visibility,
		&file.CreatedBy, &file.CreatedAt, &file.UploadedAt,
	)
	if err != nil {
		return domain.File{}, err
	}
	return file, nil
}

func (r *MediaRepository) CompleteUpload(ctx context.Context, fileID string, sizeBytes int64, contentType, nextStatus, scanStatus string, eventPayload map[string]any) (domain.File, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.File{}, err
	}
	defer tx.Rollback()

	const updateFile = `
UPDATE media_files
SET status = $2,
    scan_status = $3,
    size_bytes = CASE WHEN $4 > 0 THEN $4 ELSE size_bytes END,
    content_type = CASE WHEN $5 <> '' THEN $5 ELSE content_type END,
    uploaded_at = now()
WHERE id = $1
  AND deleted_at IS NULL
  AND status = 'pending_upload'
RETURNING tenant_id`
	var tenantID string
	if err := tx.QueryRowContext(ctx, updateFile, fileID, nextStatus, scanStatus, sizeBytes, contentType).Scan(&tenantID); err != nil {
		if err == sql.ErrNoRows {
			return domain.File{}, sql.ErrNoRows
		}
		return domain.File{}, fmt.Errorf("update media file: %w", err)
	}
	if tenantID == "" {
		return domain.File{}, sql.ErrNoRows
	}

	const updateSession = `
UPDATE media_upload_sessions
SET status = 'completed', completed_at = now()
WHERE file_id = $1 AND status = 'pending'`
	if _, err := tx.ExecContext(ctx, updateSession, fileID); err != nil {
		return domain.File{}, fmt.Errorf("update upload session: %w", err)
	}

	if err := insertOutbox(ctx, tx, tenantID, "media.upload.completed", "media_file", fileID, eventPayload); err != nil {
		return domain.File{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.File{}, err
	}
	return r.GetFile(ctx, fileID)
}

func insertOutbox(ctx context.Context, tx *sql.Tx, tenantID, eventType, aggregateType, aggregateID string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	const query = `
INSERT INTO media_outbox_events (
  id, tenant_id, event_type, aggregate_type, aggregate_id, payload, status, next_retry_at
) VALUES ($1,$2,$3,$4,$5,$6,'pending',$7)`
	if _, err := tx.ExecContext(ctx, query, domain.NewID("evt"), tenantID, eventType, aggregateType, aggregateID, data, time.Now().UTC()); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}
