package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/media-service/internal/domain"
	"github.com/arda-labs/arda/apps/media-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

type MediaHandler struct {
	service *service.MediaService
}

func NewMediaHandler(service *service.MediaService) *MediaHandler {
	return &MediaHandler{service: service}
}

func (h *MediaHandler) Upload(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	slog.Info("incoming upload request", "method", r.Method, "content_type", contentType)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, r, http.StatusBadRequest, "media.upload.invalid_form", "Failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "media.upload.missing_file", "File is required")
		return
	}
	defer file.Close()

	visibility := strings.TrimSpace(r.FormValue("visibility"))
	module := strings.TrimSpace(r.FormValue("module"))
	entityType := strings.TrimSpace(r.FormValue("entity_type"))
	if visibility == "" {
		if module == "iam" || entityType == "iam_user" || entityType == "iam_user_cover" {
			visibility = "public"
		}
	}

	req := domain.InitUploadRequest{
		Module:           module,
		EntityType:       entityType,
		EntityID:         r.FormValue("entity_id"),
		OriginalFilename: header.Filename,
		ContentType:      header.Header.Get("Content-Type"),
		SizeBytes:        header.Size,
		Visibility:       visibility,
	}
	applyRequestContext(r, &req)
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.OrgID) == "" {
		writeError(w, r, http.StatusForbidden, ardaerrors.CodeTenantScopeRequired, "tenant and organization are required")
		return
	}

	resp, err := h.service.UploadFile(r.Context(), req, file)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusCreated, map[string]any{
		"public_id":  resp.PublicID,
		"file_name":  resp.OriginalFilename,
		"mime_type":  resp.ContentType,
		"size":       resp.SizeBytes,
		"created_at": resp.CreatedAt,
	})
}

func (h *MediaHandler) InitUpload(w http.ResponseWriter, r *http.Request) {
	var req domain.InitUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "Request body is not valid JSON")
		return
	}
	applyRequestContext(r, &req)
	if strings.TrimSpace(req.OrgID) == "" {
		writeError(w, r, http.StatusForbidden, ardaerrors.CodeTenantScopeRequired, "tenant and organization are required")
		return
	}
	if req.Visibility == "" {
		if req.Module == "iam" || req.EntityType == "iam_user" || req.EntityType == "iam_user_cover" {
			req.Visibility = "public"
		}
	}
	resp, err := h.service.InitUpload(r.Context(), req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, map[string]any{
		"file":      publicFileJSON(resp.File),
		"upload":    resp.Upload,
		"upload_id": resp.UploadID,
	})
}

func (h *MediaHandler) CompleteUpload(w http.ResponseWriter, r *http.Request, fileID string) {
	scope, ok := mediaScope(r)
	if !ok {
		writeError(w, r, http.StatusForbidden, ardaerrors.CodeTenantScopeRequired, "tenant and organization are required")
		return
	}
	resp, err := h.service.CompleteUploadScoped(r.Context(), scope, fileID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"file": publicFileJSON(resp.File)})
}

func (h *MediaHandler) View(w http.ResponseWriter, r *http.Request, publicID string) {
	h.handleRetrieve(w, r, publicID, false)
}

func (h *MediaHandler) Download(w http.ResponseWriter, r *http.Request, publicID string) {
	h.handleRetrieve(w, r, publicID, true)
}

func (h *MediaHandler) PublicView(w http.ResponseWriter, r *http.Request, publicID string) {
	h.handlePublicRetrieve(w, r, publicID, false)
}

func (h *MediaHandler) PublicDownload(w http.ResponseWriter, r *http.Request, publicID string) {
	h.handlePublicRetrieve(w, r, publicID, true)
}

func (h *MediaHandler) Delete(w http.ResponseWriter, r *http.Request, publicID string) {
	slog.Info("incoming delete request", "public_id", publicID)
	ctx := r.Context()
	userID := firstHeader(r, "X-Actor-User-Id", "X-User-Id", "X-User-Subject")
	tenantID := firstHeader(r, "X-Tenant-Id")
	orgID := firstHeader(r, "X-Org-Id")
	scope, ok := mediaScope(r)
	if !ok {
		writeError(w, r, http.StatusForbidden, ardaerrors.CodeTenantScopeRequired, "tenant and organization are required")
		return
	}
	ip := r.RemoteAddr
	userAgent := r.UserAgent()

	err := h.service.DeleteFileByPublicIDScoped(ctx, scope, publicID)
	if err != nil {
		fmt.Printf("AUDIT: user_id=%s tenant_id=%s org_id=%s media_id=%s action=delete ip=%s ua=%s result=failed error=%v\n",
			userID, tenantID, orgID, publicID, ip, userAgent, err)
		writeServiceError(w, r, err)
		return
	}

	fmt.Printf("AUDIT: user_id=%s tenant_id=%s org_id=%s media_id=%s action=delete ip=%s ua=%s result=success\n",
		userID, tenantID, orgID, publicID, ip, userAgent)

	writeJSON(w, r, http.StatusOK, map[string]any{"status": "deleted"})
}

func (h *MediaHandler) handleRetrieve(w http.ResponseWriter, r *http.Request, publicID string, download bool) {
	slog.Info("incoming retrieve request", "public_id", publicID, "download", download)
	ctx := r.Context()
	scope, ok := mediaScope(r)
	if !ok {
		writeError(w, r, http.StatusForbidden, ardaerrors.CodeTenantScopeRequired, "tenant and organization are required")
		return
	}
	file, err := h.service.GetFileByPublicIDScoped(ctx, scope, publicID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	userID := firstHeader(r, "X-Actor-User-Id", "X-User-Id", "X-User-Subject")
	tenantID := firstHeader(r, "X-Tenant-Id")
	orgID := firstHeader(r, "X-Org-Id")
	ip := r.RemoteAddr
	userAgent := r.UserAgent()

	action := "view"
	if download {
		action = "download"
	}
	fmt.Printf("AUDIT: user_id=%s tenant_id=%s org_id=%s media_id=%s action=%s ip=%s ua=%s result=success\n",
		userID, tenantID, orgID, file.ID, action, ip, userAgent)

	if file.Status != domain.StatusReady && file.Status != domain.StatusUploaded && file.Status != domain.StatusTemp && file.Status != domain.StatusAttached {
		writeError(w, r, http.StatusConflict, "media.file.not_ready", "File is not ready")
		return
	}

	const MaxStreamSize = 2 * 1024 * 1024 // 2MB
	if file.SizeBytes < MaxStreamSize {
		stream, err := h.service.GetObjectStream(ctx, file)
		if err != nil {
			slog.Error("failed to get stream", "err", err)
			writeError(w, r, http.StatusInternalServerError, "media.stream.failed", "Failed to stream file")
			return
		}
		defer stream.Close()

		w.Header().Set("Content-Type", file.ContentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", file.SizeBytes))
		w.Header().Set("Cache-Control", "private, max-age=86400")

		if download {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.OriginalFilename))
		}

		etag := fmt.Sprintf(`W/"%s-%d"`, publicID, file.SizeBytes)
		w.Header().Set("ETag", etag)

		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, stream)
	} else {
		redirectURL, err := h.service.GetContentRedirectURLByPublicIDScoped(ctx, scope, publicID, download)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=30")
		http.Redirect(w, r, redirectURL, http.StatusFound)
	}
}

func (h *MediaHandler) handlePublicRetrieve(w http.ResponseWriter, r *http.Request, publicID string, download bool) {
	slog.Info("incoming public retrieve request", "public_id", publicID, "download", download)
	ctx := r.Context()
	file, err := h.service.GetPublicFileByPublicID(ctx, publicID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	if file.Status != domain.StatusReady && file.Status != domain.StatusUploaded && file.Status != domain.StatusTemp && file.Status != domain.StatusAttached {
		writeError(w, r, http.StatusConflict, "media.file.not_ready", "File is not ready")
		return
	}

	const MaxStreamSize = 2 * 1024 * 1024 // 2MB
	if file.SizeBytes < MaxStreamSize {
		stream, err := h.service.GetObjectStream(ctx, file)
		if err != nil {
			slog.Error("failed to get stream", "err", err)
			writeError(w, r, http.StatusInternalServerError, "media.stream.failed", "Failed to stream file")
			return
		}
		defer stream.Close()

		w.Header().Set("Content-Type", file.ContentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", file.SizeBytes))
		w.Header().Set("Cache-Control", "public, max-age=86400")

		if download {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.OriginalFilename))
		}

		etag := fmt.Sprintf(`W/"%s-%d"`, publicID, file.SizeBytes)
		w.Header().Set("ETag", etag)

		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, stream)
	} else {
		redirectURL, err := h.service.GetPublicContentRedirectURLByPublicID(ctx, publicID, download)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.Redirect(w, r, redirectURL, http.StatusFound)
	}
}

func applyRequestContext(r *http.Request, req *domain.InitUploadRequest) {
	if req.TenantID == "" {
		req.TenantID = firstHeader(r, "X-Tenant-Id", "X-Tenant-ID")
	}
	if req.OrgID == "" {
		req.OrgID = firstHeader(r, "X-Org-Id", "X-Org-ID")
	}
	if req.CreatedBy == "" {
		req.CreatedBy = firstHeader(r, "X-User-Id", "X-User-ID", "X-User-Subject")
	}
	if req.OwnerUserID == "" {
		req.OwnerUserID = req.CreatedBy
	}
}

func mediaScope(r *http.Request) (domain.FileScope, bool) {
	scope := domain.FileScope{
		TenantID: strings.TrimSpace(firstHeader(r, "X-Tenant-Id", "X-Tenant-ID")),
		OrgID:    strings.TrimSpace(firstHeader(r, "X-Org-Id", "X-Org-ID")),
	}
	return scope, scope.TenantID != "" && scope.OrgID != ""
}

func firstHeader(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(r.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func publicFileJSON(file domain.File) map[string]any {
	result := map[string]any{
		"public_id":         file.PublicID,
		"module":            file.Module,
		"entity_type":       file.EntityType,
		"entity_id":         file.EntityID,
		"original_filename": file.OriginalFilename,
		"content_type":      file.ContentType,
		"size_bytes":        file.SizeBytes,
		"status":            file.Status,
		"scan_status":       file.ScanStatus,
		"visibility":        file.Visibility,
		"created_at":        file.CreatedAt,
	}
	if file.UploadedAt != nil {
		result["uploaded_at"] = file.UploadedAt
	}
	if file.ExpiresAt != nil {
		result["expires_at"] = file.ExpiresAt
	}
	return result
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("media service error", "err", err)
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "validation.invalid_input", err.Error())
	case errors.Is(err, service.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "media.file.not_found", "Media file not found")
	case errors.Is(err, service.ErrNotReady):
		writeError(w, r, http.StatusConflict, "media.file.not_ready", "Media file is not ready")
	default:
		writeError(w, r, http.StatusInternalServerError, "common.error.internal", "Internal server error")
	}
}
