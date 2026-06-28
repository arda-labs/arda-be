package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/media-service/internal/config"
	"github.com/arda-labs/arda/apps/media-service/internal/domain"
	"github.com/arda-labs/arda/apps/media-service/internal/repository"
	"github.com/arda-labs/arda/apps/media-service/internal/storage"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrNotFound     = errors.New("media file not found")
	ErrNotReady     = errors.New("media file is not ready")
)

type MediaService struct {
	cfg     config.Config
	repo    *repository.MediaRepository
	storage storage.Provider
}

func NewMediaService(cfg config.Config, repo *repository.MediaRepository, provider storage.Provider) *MediaService {
	return &MediaService{cfg: cfg, repo: repo, storage: provider}
}

func (s *MediaService) InitUpload(ctx context.Context, req domain.InitUploadRequest) (domain.InitUploadResponse, error) {
	req.TenantID = strings.TrimSpace(req.TenantID)
	if req.TenantID == "" {
		req.TenantID = domain.DefaultTenantID
	}
	req.Module = strings.TrimSpace(req.Module)
	req.OriginalFilename = strings.TrimSpace(req.OriginalFilename)
	req.ContentType = strings.TrimSpace(req.ContentType)
	if req.Module == "" || req.OriginalFilename == "" || req.ContentType == "" {
		return domain.InitUploadResponse{}, fmt.Errorf("%w: module, original_filename and content_type are required", ErrInvalidInput)
	}
	if req.SizeBytes < 0 {
		return domain.InitUploadResponse{}, fmt.Errorf("%w: size_bytes must be positive", ErrInvalidInput)
	}
	if s.cfg.UploadMaxSizeMB > 0 && req.SizeBytes > s.cfg.UploadMaxSizeMB*1024*1024 {
		return domain.InitUploadResponse{}, fmt.Errorf("%w: file is larger than upload limit", ErrInvalidInput)
	}

	fileID := domain.NewID("file")
	publicID := domain.NewID("mf")
	versionID := "v1"
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(req.OriginalFilename)), ".")
	storageClass := defaultString(req.StorageClass, "standard")
	visibility := defaultString(req.Visibility, "private")
	objectKey := buildObjectKey(req.TenantID, fileID, versionID)
	expiresAt := time.Now().UTC().Add(s.cfg.PresignUploadTTL)
	tempExpiresAt := time.Now().UTC().Add(s.cfg.TempFileTTL)

	file := domain.File{
		ID:               fileID,
		PublicID:         publicID,
		TenantID:         req.TenantID,
		OrgID:            req.OrgID,
		OwnerUserID:      req.OwnerUserID,
		Module:           req.Module,
		EntityType:       "", // Left empty for TEMP
		EntityID:         "", // Left empty for TEMP
		OriginalFilename: req.OriginalFilename,
		ContentType:      req.ContentType,
		Extension:        extension,
		SizeBytes:        req.SizeBytes,
		ChecksumSHA256:   req.ChecksumSHA256,
		Status:           domain.StatusPendingUpload,
		ScanStatus:       domain.ScanNotRequired,
		StorageProvider:  s.cfg.StorageProvider,
		Bucket:           s.cfg.StorageBucket,
		ObjectKey:        objectKey,
		StorageClass:     storageClass,
		VersionID:        versionID,
		Visibility:       visibility,
		CreatedBy:        req.CreatedBy,
		ExpiresAt:        &tempExpiresAt,
	}
	session := domain.UploadSession{
		ID:        domain.NewID("upl"),
		TenantID:  req.TenantID,
		FileID:    fileID,
		ExpiresAt: expiresAt,
		Status:    "pending",
	}

	presigned, err := s.storage.PresignPutObject(ctx, storage.PresignPutInput{
		Bucket:      file.Bucket,
		Key:         file.ObjectKey,
		ContentType: file.ContentType,
		ExpiresIn:   s.cfg.PresignUploadTTL,
	})
	if err != nil {
		return domain.InitUploadResponse{}, err
	}

	created, err := s.repo.CreatePendingUpload(ctx, file, session, map[string]any{
		"file_id":      file.ID,
		"tenant_id":    file.TenantID,
		"org_id":       file.OrgID,
		"module":       file.Module,
		"entity_type":  file.EntityType,
		"entity_id":    file.EntityID,
		"content_type": file.ContentType,
		"size_bytes":   file.SizeBytes,
	})
	if err != nil {
		return domain.InitUploadResponse{}, err
	}

	return domain.InitUploadResponse{
		File: created,
		Upload: domain.UploadURL{
			Method:    "PUT",
			URL:       presigned.URL,
			Headers:   presigned.Headers,
			ExpiresAt: presigned.ExpiresAt,
		},
		UploadID: session.ID,
	}, nil
}

func (s *MediaService) CompleteUpload(ctx context.Context, fileID string) (domain.CompleteUploadResponse, error) {
	file, err := s.repo.GetFile(ctx, fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.CompleteUploadResponse{}, ErrNotFound
		}
		return domain.CompleteUploadResponse{}, err
	}
	if file.Status != domain.StatusPendingUpload {
		return domain.CompleteUploadResponse{File: file}, nil
	}

	info, err := s.storage.HeadObject(ctx, file.Bucket, file.ObjectKey)
	if err != nil {
		return domain.CompleteUploadResponse{}, err
	}

	nextStatus := domain.StatusTemp
	scanStatus := domain.ScanNotRequired
	if s.cfg.RequireScanBeforeReady {
		nextStatus = domain.StatusScanPending
		scanStatus = domain.ScanPending
	}

	updated, err := s.repo.CompleteUpload(ctx, fileID, info.SizeBytes, info.ContentType, nextStatus, scanStatus, map[string]any{
		"file_id":      file.ID,
		"tenant_id":    file.TenantID,
		"org_id":       file.OrgID,
		"module":       file.Module,
		"entity_type":  file.EntityType,
		"entity_id":    file.EntityID,
		"content_type": defaultString(info.ContentType, file.ContentType),
		"size_bytes":   info.SizeBytes,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.CompleteUploadResponse{}, ErrNotFound
		}
		return domain.CompleteUploadResponse{}, err
	}
	return domain.CompleteUploadResponse{File: updated}, nil
}

func (s *MediaService) GetFile(ctx context.Context, fileID string) (domain.File, error) {
	file, err := s.repo.GetFile(ctx, fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.File{}, ErrNotFound
		}
		return domain.File{}, err
	}
	return file, nil
}

func (s *MediaService) GetFileByPublicID(ctx context.Context, publicID string) (domain.File, error) {
	file, err := s.repo.GetFileByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.File{}, ErrNotFound
		}
		return domain.File{}, err
	}
	return file, nil
}

func (s *MediaService) DeleteFileByPublicID(ctx context.Context, publicID string) error {
	file, err := s.repo.GetFileByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	// 1. Delete object from storage
	err = s.storage.DeleteObject(ctx, file.Bucket, file.ObjectKey)
	if err != nil {
		return fmt.Errorf("delete from S3: %w", err)
	}

	// 2. Mark as deleted in repo
	return s.repo.DeleteFile(ctx, file.ID)
}

func (s *MediaService) GetContentRedirectURLByPublicID(ctx context.Context, publicID string, download bool) (string, error) {
	file, err := s.repo.GetFileByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}

	if file.Status != domain.StatusReady && file.Status != domain.StatusUploaded && file.Status != domain.StatusTemp && file.Status != domain.StatusAttached {
		return "", ErrNotReady
	}

	input := storage.PresignGetInput{
		Bucket:    file.Bucket,
		Key:       file.ObjectKey,
		ExpiresIn: s.cfg.PresignDownloadTTL,
	}
	if download {
		input.ResponseContentDisposition = fmt.Sprintf("attachment; filename=%q", file.OriginalFilename)
	}

	presigned, err := s.storage.PresignGetObject(ctx, input)
	if err != nil {
		return "", err
	}
	return presigned.URL, nil
}

func (s *MediaService) UploadFile(ctx context.Context, req domain.InitUploadRequest, body io.Reader) (domain.File, error) {
	req.TenantID = strings.TrimSpace(req.TenantID)
	if req.TenantID == "" {
		req.TenantID = domain.DefaultTenantID
	}
	req.Module = strings.TrimSpace(req.Module)
	req.OriginalFilename = strings.TrimSpace(req.OriginalFilename)
	req.ContentType = strings.TrimSpace(req.ContentType)
	if req.Module == "" || req.OriginalFilename == "" || req.ContentType == "" {
		return domain.File{}, fmt.Errorf("%w: module, original_filename and content_type are required", ErrInvalidInput)
	}
	if req.SizeBytes < 0 {
		return domain.File{}, fmt.Errorf("%w: size_bytes must be positive", ErrInvalidInput)
	}
	if s.cfg.UploadMaxSizeMB > 0 && req.SizeBytes > s.cfg.UploadMaxSizeMB*1024*1024 {
		return domain.File{}, fmt.Errorf("%w: file is larger than upload limit", ErrInvalidInput)
	}

	fileID := domain.NewID("file")
	publicID := domain.NewID("mf")
	versionID := "v1"
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(req.OriginalFilename)), ".")
	storageClass := defaultString(req.StorageClass, "standard")
	visibility := defaultString(req.Visibility, "private")
	objectKey := buildObjectKey(req.TenantID, fileID, versionID)
	tempExpiresAt := time.Now().UTC().Add(s.cfg.TempFileTTL)

	file := domain.File{
		ID:               fileID,
		PublicID:         publicID,
		TenantID:         req.TenantID,
		OrgID:            req.OrgID,
		OwnerUserID:      req.OwnerUserID,
		Module:           req.Module,
		EntityType:       "", // Left empty for TEMP
		EntityID:         "", // Left empty for TEMP
		OriginalFilename: req.OriginalFilename,
		ContentType:      req.ContentType,
		Extension:        extension,
		SizeBytes:        req.SizeBytes,
		ChecksumSHA256:   req.ChecksumSHA256,
		Status:           domain.StatusTemp,
		ScanStatus:       domain.ScanNotRequired,
		StorageProvider:  s.cfg.StorageProvider,
		Bucket:           s.cfg.StorageBucket,
		ObjectKey:        objectKey,
		StorageClass:     storageClass,
		VersionID:        versionID,
		Visibility:       visibility,
		CreatedBy:        req.CreatedBy,
		ExpiresAt:        &tempExpiresAt,
	}

	err := s.storage.PutObject(ctx, file.Bucket, file.ObjectKey, body, file.SizeBytes, file.ContentType)
	if err != nil {
		return domain.File{}, fmt.Errorf("upload to S3: %w", err)
	}

	session := domain.UploadSession{
		ID:        domain.NewID("upl"),
		TenantID:  req.TenantID,
		FileID:    fileID,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		Status:    "completed",
	}

	file.Status = domain.StatusPendingUpload
	created, err := s.repo.CreatePendingUpload(ctx, file, session, map[string]any{
		"file_id":      file.ID,
		"public_id":    file.PublicID,
		"tenant_id":    file.TenantID,
		"org_id":       file.OrgID,
		"module":       file.Module,
		"entity_type":  file.EntityType,
		"entity_id":    file.EntityID,
		"content_type": file.ContentType,
		"size_bytes":   file.SizeBytes,
	})
	if err != nil {
		return domain.File{}, err
	}

	updated, err := s.repo.CompleteUpload(ctx, created.ID, file.SizeBytes, file.ContentType, domain.StatusTemp, domain.ScanNotRequired, map[string]any{
		"file_id":      created.ID,
		"public_id":    created.PublicID,
		"tenant_id":    created.TenantID,
		"org_id":       created.OrgID,
		"module":       created.Module,
		"entity_type":  created.EntityType,
		"entity_id":    created.EntityID,
		"content_type": created.ContentType,
		"size_bytes":   created.SizeBytes,
	})
	if err != nil {
		return domain.File{}, err
	}

	return updated, nil
}

func (s *MediaService) AttachFiles(ctx context.Context, publicIDs []string, tenantID, orgID, userID string, ownerType, ownerID string) error {
	if len(publicIDs) == 0 {
		return nil
	}

	payloads := make(map[string]map[string]any)
	for _, pid := range publicIDs {
		payloads[pid] = map[string]any{
			"public_id":   pid,
			"tenant_id":   tenantID,
			"org_id":      orgID,
			"owner_type":  ownerType,
			"owner_id":    ownerID,
			"attached_by": userID,
			"attached_at": time.Now().UTC(),
		}
	}

	return s.repo.AttachFiles(ctx, publicIDs, tenantID, orgID, userID, ownerType, ownerID, payloads)
}

func (s *MediaService) CleanupExpiredTempFiles(ctx context.Context) (int, error) {
	files, err := s.repo.GetExpiredTempFiles(ctx, 100)
	if err != nil {
		return 0, fmt.Errorf("query expired temp files: %w", err)
	}

	count := 0
	for _, f := range files {
		err := s.storage.DeleteObject(ctx, f.Bucket, f.ObjectKey)
		if err != nil {
			slog.Error("failed to delete expired temp file from storage", "file_id", f.ID, "object_key", f.ObjectKey, "err", err)
		}

		err = s.repo.DeleteFile(ctx, f.ID)
		if err != nil {
			slog.Error("failed to mark expired temp file as deleted in DB", "file_id", f.ID, "err", err)
			continue
		}

		count++
	}

	return count, nil
}

func buildObjectKey(tenantID, fileID, versionID string) string {
	now := time.Now().UTC()
	return fmt.Sprintf("tenants/%s/%04d/%02d/%02d/%s/%s/original", tenantID, now.Year(), now.Month(), now.Day(), fileID, versionID)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (s *MediaService) GetObjectStream(ctx context.Context, file domain.File) (io.ReadCloser, error) {
	return s.storage.GetObject(ctx, file.Bucket, file.ObjectKey)
}
