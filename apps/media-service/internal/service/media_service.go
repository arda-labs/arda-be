package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"path/filepath"
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

	ErrPreviewUnsupported = errors.New("preview is not supported for this file type")
	ErrPreviewUnavailable = errors.New("document conversion is not configured")
	ErrPreviewTooLarge    = errors.New("file is too large to convert for preview")
)

// DocumentConverter renders an Office document to PDF. Implemented by the
// Gotenberg client; kept as an interface so tests can stub conversion.
type DocumentConverter interface {
	OfficeToPDF(ctx context.Context, filename string, content []byte) ([]byte, error)
}

type MediaService struct {
	cfg          config.Config
	repo         *repository.MediaRepository
	storage      storage.Provider
	uploadPolicy UploadPolicy
	converter    DocumentConverter
}

// Option customises optional MediaService collaborators.
type Option func(*MediaService)

// WithDocumentConverter enables /preview conversion for office documents.
func WithDocumentConverter(converter DocumentConverter) Option {
	return func(s *MediaService) { s.converter = converter }
}

func NewMediaService(cfg config.Config, repo *repository.MediaRepository, provider storage.Provider, opts ...Option) *MediaService {
	svc := &MediaService{cfg: cfg, repo: repo, storage: provider, uploadPolicy: NewUploadPolicy(cfg.AllowedUploadMIME)}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc
}

// MaxUploadBytes returns the hard request-body cap for uploads: the configured
// file limit plus multipart overhead.
func (s *MediaService) MaxUploadBytes() int64 {
	limitMB := s.cfg.UploadMaxSizeMB
	if limitMB <= 0 {
		limitMB = 100
	}
	return limitMB*1024*1024 + (1 << 20)
}

// SupportsBrowserPresign reports whether large GETs can be redirected to a
// public storage URL. Without a public endpoint the handler must stream the
// object instead of redirecting the browser to an unreachable internal host.
func (s *MediaService) SupportsBrowserPresign() bool {
	provider, ok := s.storage.(interface{ BrowserPresignAvailable() bool })
	return ok && provider.BrowserPresignAvailable()
}

// MaxStreamBytes returns the size below which objects are streamed through the
// service. Larger objects redirect to the public storage endpoint when one is
// configured. Defaults to 2MB, the historical threshold.
func (s *MediaService) MaxStreamBytes() int64 {
	limitMB := s.cfg.StreamMaxSizeMB
	if limitMB <= 0 {
		limitMB = 2
	}
	return limitMB * 1024 * 1024
}

// PreviewPDF returns a PDF rendering of the file: PDFs pass straight through,
// office documents convert through Gotenberg and cache the result under a
// derived storage key so repeat views skip the conversion.
func (s *MediaService) PreviewPDF(ctx context.Context, file domain.File) ([]byte, error) {
	if isPDFContentType(file.ContentType) {
		return s.readObject(ctx, file.Bucket, file.ObjectKey)
	}
	if !isOfficeConvertible(file.ContentType, file.OriginalFilename) {
		return nil, ErrPreviewUnsupported
	}
	if s.converter == nil {
		return nil, ErrPreviewUnavailable
	}
	if limit := s.previewLimitBytes(); limit > 0 && file.SizeBytes > limit {
		return nil, ErrPreviewTooLarge
	}
	cacheKey := previewObjectKey(file)
	if cached, err := s.readObject(ctx, file.Bucket, cacheKey); err == nil {
		return cached, nil
	}
	source, err := s.readObject(ctx, file.Bucket, file.ObjectKey)
	if err != nil {
		return nil, err
	}
	pdf, err := s.converter.OfficeToPDF(ctx, file.OriginalFilename, source)
	if err != nil {
		return nil, fmt.Errorf("convert office document: %w", err)
	}
	// Best-effort cache: a failed store must not block the preview response.
	if storeErr := s.storage.PutObject(ctx, file.Bucket, cacheKey, bytes.NewReader(pdf), int64(len(pdf)), "application/pdf"); storeErr != nil {
		slog.Warn("store derived preview", "public_id", file.PublicID, "err", storeErr)
	}
	return pdf, nil
}

func (s *MediaService) previewLimitBytes() int64 {
	limitMB := s.cfg.PreviewMaxSizeMB
	if limitMB <= 0 {
		limitMB = 25
	}
	return limitMB * 1024 * 1024
}

func (s *MediaService) readObject(ctx context.Context, bucket, key string) ([]byte, error) {
	reader, err := s.storage.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// previewObjectKey is the stable derived-object key for a converted PDF. The
// source file is immutable, so the cache never needs invalidation.
func previewObjectKey(file domain.File) string {
	return fmt.Sprintf("derived/%s/%s/v1.pdf", file.TenantID, file.PublicID)
}

func isPDFContentType(contentType string) bool {
	return strings.EqualFold(strings.TrimSpace(contentType), "application/pdf")
}

func isOfficeConvertible(contentType, filename string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(ct, "application/vnd.openxmlformats-officedocument"):
		return true
	case strings.HasPrefix(ct, "application/vnd.ms-"):
		return true
	case ct == "application/msword":
		return true
	case strings.HasPrefix(ct, "application/vnd.oasis.opendocument"):
		return true
	}
	// Browsers sometimes upload office files as octet-stream; fall back to the
	// extension so preview still works.
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".odt", ".ods", ".odp":
		return true
	default:
		return false
	}
}

func (s *MediaService) InitUpload(ctx context.Context, req domain.InitUploadRequest) (domain.InitUploadResponse, error) {
	req.TenantID = strings.TrimSpace(req.TenantID)
	if req.TenantID == "" {
		return domain.InitUploadResponse{}, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}
	req.Module = strings.TrimSpace(req.Module)
	req.OriginalFilename = strings.TrimSpace(req.OriginalFilename)
	req.ContentType = strings.TrimSpace(req.ContentType)
	if req.Module == "" || req.OriginalFilename == "" || req.ContentType == "" {
		return domain.InitUploadResponse{}, fmt.Errorf("%w: module, original_filename and content_type are required", ErrInvalidInput)
	}
	resolvedType, err := s.uploadPolicy.ValidateDeclared(req.ContentType, req.OriginalFilename)
	if err != nil {
		return domain.InitUploadResponse{}, err
	}
	req.ContentType = resolvedType
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
	file, err := s.repo.GetFileInternal(ctx, fileID)
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
	resolvedType, err := s.resolveStoredContentType(ctx, file, info.ContentType)
	if err != nil {
		return domain.CompleteUploadResponse{}, err
	}

	nextStatus := domain.StatusTemp
	scanStatus := domain.ScanNotRequired
	if s.cfg.RequireScanBeforeReady {
		nextStatus = domain.StatusScanPending
		scanStatus = domain.ScanPending
	}

	updated, err := s.repo.CompleteUpload(ctx, fileID, info.SizeBytes, resolvedType, nextStatus, scanStatus, map[string]any{
		"file_id":      file.ID,
		"tenant_id":    file.TenantID,
		"org_id":       file.OrgID,
		"module":       file.Module,
		"entity_type":  file.EntityType,
		"entity_id":    file.EntityID,
		"content_type": resolvedType,
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

func (s *MediaService) CompleteUploadScoped(ctx context.Context, scope domain.FileScope, fileID string) (domain.CompleteUploadResponse, error) {
	file, err := s.repo.GetFileByPublicIDScoped(ctx, scope, fileID)
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
	resolvedType, err := s.resolveStoredContentType(ctx, file, info.ContentType)
	if err != nil {
		return domain.CompleteUploadResponse{}, err
	}
	nextStatus := domain.StatusTemp
	scanStatus := domain.ScanNotRequired
	if s.cfg.RequireScanBeforeReady {
		nextStatus = domain.StatusScanPending
		scanStatus = domain.ScanPending
	}
	updated, err := s.repo.CompleteUploadScoped(ctx, scope, file.ID, info.SizeBytes, resolvedType, nextStatus, scanStatus, map[string]any{
		"file_id": file.ID, "tenant_id": file.TenantID, "org_id": file.OrgID,
		"module": file.Module, "content_type": resolvedType, "size_bytes": info.SizeBytes,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.CompleteUploadResponse{}, ErrNotFound
		}
		return domain.CompleteUploadResponse{}, err
	}
	return domain.CompleteUploadResponse{File: updated}, nil
}

func (s *MediaService) GetFileInternal(ctx context.Context, fileID string) (domain.File, error) {
	file, err := s.repo.GetFileInternal(ctx, fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.File{}, ErrNotFound
		}
		return domain.File{}, err
	}
	return file, nil
}

func (s *MediaService) GetFileByPublicIDScoped(ctx context.Context, scope domain.FileScope, publicID string) (domain.File, error) {
	file, err := s.repo.GetFileByPublicIDScoped(ctx, scope, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.File{}, ErrNotFound
		}
		return domain.File{}, err
	}
	return file, nil
}

// ListFileMetadata returns metadata (name, size, content type, status) for the
// given public ids inside the caller's tenant/org scope. Used by catalogs that
// store media URLs but need to render file names and gate large previews.
func (s *MediaService) ListFileMetadata(ctx context.Context, scope domain.FileScope, publicIDs []string) ([]domain.File, error) {
	return s.repo.ListMetadataByPublicIDs(ctx, scope, publicIDs)
}

func (s *MediaService) GetPublicFileByPublicID(ctx context.Context, publicID string) (domain.File, error) {
	file, err := s.repo.GetPublicFileByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.File{}, ErrNotFound
		}
		return domain.File{}, err
	}
	return file, nil
}

func (s *MediaService) DeleteFileByPublicIDScoped(ctx context.Context, scope domain.FileScope, publicID string) error {
	file, err := s.repo.GetFileByPublicIDScoped(ctx, scope, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := s.storage.DeleteObject(ctx, file.Bucket, file.ObjectKey); err != nil {
		return fmt.Errorf("delete from S3: %w", err)
	}
	return s.repo.DeleteFileScoped(ctx, scope, file.ID)
}

func (s *MediaService) GetContentRedirectURLByPublicIDScoped(ctx context.Context, scope domain.FileScope, publicID string, download bool) (string, error) {
	file, err := s.repo.GetFileByPublicIDScoped(ctx, scope, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if file.Status != domain.StatusReady && file.Status != domain.StatusUploaded && file.Status != domain.StatusTemp && file.Status != domain.StatusAttached {
		return "", ErrNotReady
	}
	input := storage.PresignGetInput{Bucket: file.Bucket, Key: file.ObjectKey, ExpiresIn: s.cfg.PresignDownloadTTL, BrowserFacing: true}
	if download || !CanServeInline(file.ContentType) {
		input.ResponseContentDisposition = fmt.Sprintf("attachment; filename=%q", file.OriginalFilename)
	}
	presigned, err := s.storage.PresignGetObject(ctx, input)
	if err != nil {
		return "", err
	}
	return presigned.URL, nil
}

func (s *MediaService) GetPublicContentRedirectURLByPublicID(ctx context.Context, publicID string, download bool) (string, error) {
	file, err := s.repo.GetPublicFileByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if file.Status != domain.StatusReady && file.Status != domain.StatusUploaded && file.Status != domain.StatusTemp && file.Status != domain.StatusAttached {
		return "", ErrNotReady
	}
	input := storage.PresignGetInput{Bucket: file.Bucket, Key: file.ObjectKey, ExpiresIn: s.cfg.PresignDownloadTTL, BrowserFacing: true}
	if download || !CanServeInline(file.ContentType) {
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
		return domain.File{}, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
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

	head, body, err := ReadSniffHead(body)
	if err != nil {
		return domain.File{}, fmt.Errorf("%w: read upload body: %v", ErrInvalidInput, err)
	}
	resolvedType, err := s.uploadPolicy.Resolve(req.ContentType, req.OriginalFilename, head)
	if err != nil {
		return domain.File{}, err
	}
	req.ContentType = resolvedType

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

	err = s.storage.PutObject(ctx, file.Bucket, file.ObjectKey, body, file.SizeBytes, file.ContentType)
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

const (
	defaultEntityFileLimit = 50
	maxEntityFileLimit     = 200
)

// ListFilesByEntity returns files attached to an entity (entity_type +
// entity_id), newest first, scoped to the caller's tenant and organization.
func (s *MediaService) ListFilesByEntity(ctx context.Context, scope domain.FileScope, module, entityType, entityID string, limit int) ([]domain.File, error) {
	entityType = strings.TrimSpace(entityType)
	entityID = strings.TrimSpace(entityID)
	if entityType == "" || entityID == "" {
		return nil, fmt.Errorf("%w: entity_type and entity_id are required", ErrInvalidInput)
	}
	if limit <= 0 {
		limit = defaultEntityFileLimit
	}
	if limit > maxEntityFileLimit {
		limit = maxEntityFileLimit
	}
	return s.repo.ListFilesByEntity(ctx, scope, strings.TrimSpace(module), entityType, entityID, limit)
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

		err = s.repo.DeleteFileInternal(ctx, f.ID)
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

// resolveStoredContentType sniffs the first bytes of a stored object so the
// persisted content_type reflects real content. Presigned uploads bypass the
// multipart handler, so CompleteUpload is the enforcement point there.
func (s *MediaService) resolveStoredContentType(ctx context.Context, file domain.File, declared string) (string, error) {
	stream, err := s.storage.GetObject(ctx, file.Bucket, file.ObjectKey)
	if err != nil {
		return "", fmt.Errorf("open uploaded object: %w", err)
	}
	defer stream.Close()

	head := make([]byte, sniffLen)
	n, readErr := io.ReadFull(stream, head)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("read uploaded object: %w", readErr)
	}
	resolved, err := s.uploadPolicy.Resolve(defaultString(declared, file.ContentType), file.OriginalFilename, head[:n])
	if err != nil {
		return "", err
	}
	return resolved, nil
}
