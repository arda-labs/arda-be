package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/service"
)

// UserHandler exposes IAM HTTP handlers.
type UserHandler struct {
	svc *service.UserService
}

// NewUserHandler creates a new handler backed by svc.
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// Me returns the current user context based on the injected internal IAM user id.
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	h.getContextByID(w, r.Context(), userID)
}

// GetBySubject returns a user context by external subject.
func (h *UserHandler) GetBySubject(w http.ResponseWriter, r *http.Request) {
	sub := r.PathValue("subject")
	if sub == "" {
		respondError(w, http.StatusBadRequest, "missing subject")
		return
	}
	h.getContextBySubject(w, r.Context(), sub)
}

// GetContextByID returns a user context by internal UUID.
func (h *UserHandler) GetContextByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing id")
		return
	}
	h.getContextByID(w, r.Context(), id)
}

func (h *UserHandler) UpdateMyAvatar(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		AvatarFileID string `json:"avatar_file_id"`
		PictureURL   string `json:"picture_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx, err := h.svc.UpdateUserAvatar(r.Context(), userID, strings.TrimSpace(req.AvatarFileID), strings.TrimSpace(req.PictureURL))
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, ctx)
}

func (h *UserHandler) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		Name          string `json:"name"`
		Headline      string `json:"headline"`
		Department    string `json:"department"`
		EmployeeID    string `json:"employee_id"`
		ApprovalLevel string `json:"approval_level"`
		DailyLimit    string `json:"daily_limit"`
		Bio           string `json:"bio"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx, err := h.svc.UpdateUserProfile(r.Context(), userID,
		strings.TrimSpace(req.Name),
		strings.TrimSpace(req.Headline),
		strings.TrimSpace(req.Department),
		strings.TrimSpace(req.EmployeeID),
		strings.TrimSpace(req.ApprovalLevel),
		strings.TrimSpace(req.DailyLimit),
		strings.TrimSpace(req.Bio),
	)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, ctx)
}

func (h *UserHandler) getContextBySubject(w http.ResponseWriter, ctx context.Context, subject string) {
	userCtx, err := h.svc.GetUserContextBySubject(ctx, subject)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, userCtx)
}

func (h *UserHandler) getContextByID(w http.ResponseWriter, ctx context.Context, id string) {
	userCtx, err := h.svc.GetUserContextByID(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, userCtx)
}
