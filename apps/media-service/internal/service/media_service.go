package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	versionID := "v1"
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(req.OriginalFilename)), ".")
	storageClass := defaultString(req.StorageClass, "standard")
	visibility := defaultString(req.Visibility, "private")
	objectKey := buildObjectKey(req.TenantID, fileID, versionID)
	expiresAt := time.Now().UTC().Add(s.cfg.PresignUploadTTL)

	file := domain.File{
		ID:               fileID,
		TenantID:         req.TenantID,
		OrgID:            req.OrgID,
		OwnerUserID:      req.OwnerUserID,
		Module:           req.Module,
		EntityType:       req.EntityType,
		EntityID:         req.EntityID,
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

	nextStatus := domain.StatusReady
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

func (s *MediaService) GetDownloadURL(ctx context.Context, fileID string) (domain.DownloadURLResponse, error) {
	file, err := s.GetFile(ctx, fileID)
	if err != nil {
		return domain.DownloadURLResponse{}, err
	}
	if file.Status != domain.StatusReady && file.Status != domain.StatusUploaded {
		return domain.DownloadURLResponse{}, ErrNotReady
	}
	presigned, err := s.storage.PresignGetObject(ctx, storage.PresignGetInput{
		Bucket:    file.Bucket,
		Key:       file.ObjectKey,
		ExpiresIn: s.cfg.PresignDownloadTTL,
	})
	if err != nil {
		return domain.DownloadURLResponse{}, err
	}
	return domain.DownloadURLResponse{
		FileID:    file.ID,
		Method:    "GET",
		URL:       presigned.URL,
		ExpiresAt: presigned.ExpiresAt,
	}, nil
}

func (s *MediaService) GetContentRedirectURL(ctx context.Context, fileID string) (string, error) {
	resp, err := s.GetDownloadURL(ctx, fileID)
	if err != nil {
		return "", err
	}
	return resp.URL, nil
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
