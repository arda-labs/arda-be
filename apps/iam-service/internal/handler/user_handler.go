package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	ardamedia "github.com/arda-labs/arda/libs/go/arda-media"
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
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	h.getContextByID(w, r, r.Context(), userID)
}

// GetBySubject returns a user context by external subject.
func (h *UserHandler) GetBySubject(w http.ResponseWriter, r *http.Request) {
	sub := r.PathValue("subject")
	if sub == "" {
		respondError(w, r, http.StatusBadRequest, "missing subject")
		return
	}
	h.getContextBySubject(w, r, r.Context(), sub)
}

// GetContextByID returns a user context by internal UUID.
func (h *UserHandler) GetContextByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing id")
		return
	}
	h.getContextByID(w, r, r.Context(), id)
}

func (h *UserHandler) GetContextByKratosIdentityID(w http.ResponseWriter, r *http.Request) {
	identityID := r.PathValue("identityId")
	if identityID == "" {
		respondError(w, r, http.StatusBadRequest, "missing identity id")
		return
	}
	userCtx, err := h.svc.GetUserContextByKratosIdentityID(r.Context(), identityID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, userCtx)
}

func (h *UserHandler) ResolveOrLinkKratosIdentity(w http.ResponseWriter, r *http.Request) {
	var req resolveKratosIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	identityID := req.identityID()
	if identityID == "" {
		respondError(w, r, http.StatusBadRequest, "identity_id is required")
		return
	}
	userCtx, err := h.svc.ResolveOrLinkKratosIdentity(r.Context(), identityID, strings.TrimSpace(req.Email), strings.TrimSpace(req.Name))
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, userCtx)
}

func (h *UserHandler) ResolveOrLinkIdentity(w http.ResponseWriter, r *http.Request) {
	var req resolveIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	userCtx, err := h.svc.ResolveOrLinkIdentity(
		r.Context(),
		req.providerID(),
		req.externalID(),
		strings.TrimSpace(req.Email),
		strings.TrimSpace(req.Name),
		req.emailVerified(),
	)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, userCtx)
}

func (h *UserHandler) UpdateMyAvatar(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		AvatarFileID string `json:"avatar_file_id"`
		PictureURL   string `json:"picture_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	avatarID := strings.TrimSpace(req.AvatarFileID)
	if avatarID != "" {
		if err := ardamedia.NewClient().Attach(r.Context(), []string{avatarID}, "iam_user", userID, r); err != nil {
			slog.Error("failed to attach avatar file", "file_id", avatarID, "err", err)
			respondError(w, r, http.StatusBadGateway, "failed to attach avatar file")
			return
		}
	}

	ctx, err := h.svc.UpdateUserAvatar(r.Context(), userID, avatarID, strings.TrimSpace(req.PictureURL))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, ctx)
}

func (h *UserHandler) UpdateMyCover(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		CoverFileID   string `json:"cover_file_id"`
		CoverImageURL string `json:"cover_image_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	coverID := strings.TrimSpace(req.CoverFileID)
	if coverID != "" {
		if err := ardamedia.NewClient().Attach(r.Context(), []string{coverID}, "iam_user_cover", userID, r); err != nil {
			slog.Error("failed to attach cover file", "file_id", coverID, "err", err)
			respondError(w, r, http.StatusBadGateway, "failed to attach cover file")
			return
		}
	}

	ctx, err := h.svc.UpdateUserCover(r.Context(), userID, coverID, strings.TrimSpace(req.CoverImageURL))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, ctx)
}

func (h *UserHandler) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		Name          string `json:"name"`
		Nickname      string `json:"nickname"`
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
		respondError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	ctx, err := h.svc.UpdateUserProfile(r.Context(), userID,
		strings.TrimSpace(req.Name),
		strings.TrimSpace(req.Nickname),
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
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, ctx)
}

func (h *UserHandler) getContextBySubject(w http.ResponseWriter, r *http.Request, ctx context.Context, subject string) {
	userCtx, err := h.svc.GetUserContextBySubject(ctx, subject)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, userCtx)
}

func (h *UserHandler) getContextByID(w http.ResponseWriter, r *http.Request, ctx context.Context, id string) {
	userCtx, err := h.svc.GetUserContextByID(ctx, id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, userCtx)
}

func (h *UserHandler) UpdateMyEmail(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	newEmail := strings.TrimSpace(req.Email)
	if newEmail == "" {
		respondError(w, r, http.StatusBadRequest, "email is required")
		return
	}
	ctx, err := h.svc.UpdateUserEmail(r.Context(), userID, newEmail)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, ctx)
}

func (h *UserHandler) UpdateMyPassword(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if userID == "" {
		respondError(w, r, http.StatusUnauthorized, "missing X-User-Id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	newPassword := strings.TrimSpace(req.Password)
	if newPassword == "" {
		respondError(w, r, http.StatusBadRequest, "password is required")
		return
	}
	if err := h.svc.UpdateUserPassword(r.Context(), userID, newPassword); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}
