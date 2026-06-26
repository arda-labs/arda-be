package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/kratos"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

// AdminHandler manages users, roles, permissions.
type AdminHandler struct {
	userRepo *repository.UserRepository
	kratos   *kratos.Client
	logger   *slog.Logger
}

// NewAdminHandler creates an admin handler.
func NewAdminHandler(userRepo *repository.UserRepository, kratosClient *kratos.Client) *AdminHandler {
	return &AdminHandler{
		userRepo: userRepo,
		kratos:   kratosClient,
		logger:   slog.Default(),
	}
}

type createUserRequest struct {
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Name     string   `json:"name"`
	RoleIDs  []string `json:"role_ids,omitempty"`
}

type userResponse struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Roles    []string `json:"roles,omitempty"`
	KratosID string   `json:"kratos_id,omitempty"`
}

// CreateUser creates user in iam-service + Kratos.
func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "username, email, and password required")
		return
	}

	// 1. Create identity in Kratos
	kratosIdentity, err := h.kratos.CreateIdentity(req.Email, req.Password, req.Name)
	if err != nil {
		h.logger.Warn("kratos create identity", "err", err)
		respondError(w, http.StatusConflict, err.Error())
		return
	}

	// 2. Create user in iam-service DB
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)

	user := &domain.User{
		Username:     req.Username,
		Email:        req.Email,
		DisplayName:  req.Name,
		Subject:      kratosIdentity.ID,
		PasswordHash: string(hash),
		Source:       "kratos",
		Status:       "ACTIVE",
		TenantID:     "default",
	}
	created, err := h.userRepo.CreateUser(r.Context(), user)
	if err != nil {
		h.kratos.DeleteIdentity(kratosIdentity.ID)
		respondError(w, http.StatusInternalServerError, "create user failed")
		return
	}

	// 3. Create identity mapping
	h.userRepo.CreateIdentityMapping(r.Context(), &domain.IdentityMapping{
		ProviderID:     "kratos",
		ExternalID:     kratosIdentity.ID,
		InternalUserID: created.ID,
		IsActive:       true,
	})

	// 4. Assign roles if provided
	for _, roleID := range req.RoleIDs {
		h.assignRole(created.ID, roleID)
	}

	h.logger.Info("user created with Kratos", "username", req.Username, "kratos_id", kratosIdentity.ID)

	respondJSON(w, http.StatusCreated, userResponse{
		ID:       created.ID,
		Username: created.Username,
		Email:    created.Email,
		Name:     created.DisplayName,
		Status:   created.Status,
		KratosID: kratosIdentity.ID,
	})
}

// ListUsers returns all users.
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.allUsers(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "list users failed")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"users": users})
}

// GetUser returns a single user by ID.
func (h *AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing user id")
		return
	}

	u, err := h.userRepo.GetUserByID(r.Context(), id)
	if err != nil || u == nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}

	roles, _ := h.userRepo.GetUserRoles(r.Context(), id)
	roleCodes := make([]string, len(roles))
	for i, role := range roles {
		roleCodes[i] = role.Code
	}

	respondJSON(w, http.StatusOK, userResponse{
		ID: u.ID, Username: u.Username, Email: u.Email,
		Name: u.DisplayName, Status: u.Status, Roles: roleCodes,
	})
}

// DeleteUser removes user from iam-service + Kratos.
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing user id")
		return
	}

	u, err := h.userRepo.GetUserByID(r.Context(), id)
	if err != nil || u == nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}

	// Delete Kratos identity
	h.kratos.DeleteIdentity(u.Subject)

	// In production, add actual DB delete
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── helpers (stubs — implement with real DB queries) ──

func (h *AdminHandler) allUsers(ctx context.Context) ([]userResponse, error) {
	// In production: query with pagination
	return nil, fmt.Errorf("not implemented")
}

func (h *AdminHandler) assignRole(userID, roleID string) {
	// In production: INSERT INTO iam_user_roles
}
