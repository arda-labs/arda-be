package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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

	avatarID := strings.TrimSpace(req.AvatarFileID)
	if avatarID != "" {
		if err := attachMediaFile(r.Context(), avatarID, "iam_user", userID, r); err != nil {
			slog.Error("failed to attach avatar file", "file_id", avatarID, "err", err)
		}
	}

	respondJSON(w, http.StatusOK, ctx)
}

func (h *UserHandler) UpdateMyCover(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		CoverFileID   string `json:"cover_file_id"`
		CoverImageURL string `json:"cover_image_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx, err := h.svc.UpdateUserCover(r.Context(), userID, strings.TrimSpace(req.CoverFileID), strings.TrimSpace(req.CoverImageURL))
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	coverID := strings.TrimSpace(req.CoverFileID)
	if coverID != "" {
		if err := attachMediaFile(r.Context(), coverID, "iam_user_cover", userID, r); err != nil {
			slog.Error("failed to attach cover file", "file_id", coverID, "err", err)
		}
	}

	respondJSON(w, http.StatusOK, ctx)
}

func attachMediaFile(ctx context.Context, publicID, ownerType, ownerID string, r *http.Request) error {
	url := "http://localhost:8092/api/media/attach"
	if envUrl := os.Getenv("MEDIA_SERVICE_URL"); envUrl != "" {
		url = strings.TrimSuffix(envUrl, "/") + "/api/media/attach"
	}

	payload := map[string]any{
		"public_ids": []string{publicID},
		"owner_type": ownerType,
		"owner_id":   ownerID,
	}
	bodyData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	req.Header.Set("X-Tenant-Id", r.Header.Get("X-Tenant-Id"))
	req.Header.Set("X-Org-Id", r.Header.Get("X-Org-Id"))
	req.Header.Set("X-User-Id", r.Header.Get("X-User-Id"))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to attach: status %d", resp.StatusCode)
	}
	return nil
}

func (h *UserHandler) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		Name          string `json:"name"`
		FirstName     string `json:"first_name"`
		LastName      string `json:"last_name"`
		PhoneNumber   string `json:"phone_number"`
		Birthdate     string `json:"birthdate"`
		Gender        string `json:"gender"`
		Address       string `json:"address"`
		Country       string `json:"country"`
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
		strings.TrimSpace(req.FirstName),
		strings.TrimSpace(req.LastName),
		strings.TrimSpace(req.PhoneNumber),
		strings.TrimSpace(req.Birthdate),
		strings.TrimSpace(req.Gender),
		strings.TrimSpace(req.Address),
		strings.TrimSpace(req.Country),
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

func (h *UserHandler) UpdateMyEmail(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json")
		return
	}
	newEmail := strings.TrimSpace(req.Email)
	if newEmail == "" {
		respondError(w, http.StatusBadRequest, "email is required")
		return
	}
	ctx, err := h.svc.UpdateUserEmail(r.Context(), userID, newEmail)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, ctx)
}
