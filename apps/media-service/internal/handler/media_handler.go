package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/media-service/internal/domain"
	"github.com/arda-labs/arda/apps/media-service/internal/service"
)

type MediaHandler struct {
	service *service.MediaService
}

func NewMediaHandler(service *service.MediaService) *MediaHandler {
	return &MediaHandler{service: service}
}

func (h *MediaHandler) InitUpload(w http.ResponseWriter, r *http.Request) {
	var req domain.InitUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "validation.invalid_json", "Request body is not valid JSON")
		return
	}
	applyRequestContext(r, &req)

	resp, err := h.service.InitUpload(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *MediaHandler) CompleteUpload(w http.ResponseWriter, r *http.Request, fileID string) {
	resp, err := h.service.CompleteUpload(r.Context(), fileID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *MediaHandler) GetFile(w http.ResponseWriter, r *http.Request, fileID string) {
	file, err := h.service.GetFile(r.Context(), fileID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (h *MediaHandler) GetDownloadURL(w http.ResponseWriter, r *http.Request, fileID string) {
	resp, err := h.service.GetDownloadURL(r.Context(), fileID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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

func firstHeader(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(r.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "validation.invalid_input", err.Error())
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, "media.file.not_found", "Media file not found")
	case errors.Is(err, service.ErrNotReady):
		writeError(w, http.StatusConflict, "media.file.not_ready", "Media file is not ready")
	default:
		writeError(w, http.StatusInternalServerError, "common.error.internal", "Internal server error")
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

