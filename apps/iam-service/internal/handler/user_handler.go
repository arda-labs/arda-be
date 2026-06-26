package handler

import (
	"context"
	"net/http"

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

// Me returns the current user context based on the injected X-User-Subject header.
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	sub := r.Header.Get("X-User-Subject")
	if sub == "" {
		respondError(w, http.StatusUnauthorized, "missing X-User-Subject")
		return
	}
	h.getContextBySubject(w, r.Context(), sub)
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
	ctx, err := h.svc.GetUserContextByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
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
