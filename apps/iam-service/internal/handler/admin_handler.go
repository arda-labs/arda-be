package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/audit"
	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	"github.com/arda-labs/arda/apps/iam-service/internal/system"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// AdminHandler manages users, roles, permissions.
type AdminHandler struct {
	userRepo  *repository.UserRepository
	roleRepo  *repository.RoleRepository
	groupRepo *repository.GroupRepository
	userSvc   *service.AdminUserService
	audit     *audit.Logger
	logger    *slog.Logger
}

// NewAdminHandler creates an admin handler.
func NewAdminHandler(userRepo *repository.UserRepository, roleRepo *repository.RoleRepository, groupRepo *repository.GroupRepository, userSvc *service.AdminUserService, auditLogger *audit.Logger) *AdminHandler {
	return &AdminHandler{
		userRepo:  userRepo,
		roleRepo:  roleRepo,
		groupRepo: groupRepo,
		userSvc:   userSvc,
		audit:     auditLogger,
		logger:    slog.Default(),
	}
}

// ── User CRUD ──


func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	listQuery := parseAdminListQuery(r)
	status := r.URL.Query().Get("status")
	tenantID := firstNonEmpty(r.URL.Query().Get("tenant_id"), r.URL.Query().Get("tenantId"))

	users, total, err := h.userSvc.ListUsers(r.Context(), repository.ListUsersParams{
		Page:      listQuery.Page,
		Size:      listQuery.PerPage,
		Status:    status,
		Search:    listQuery.Q,
		TenantID:  tenantID,
		SortField: listQuery.Sort,
		SortOrder: listSortOrder(listQuery),
	})
	if err != nil {
		h.logger.Error("list users", "err", err)
		respondRequestError(w, r, http.StatusInternalServerError, ardaerrors.CodeInternal, "list users failed")
		return
	}

	items := make([]adminUserItemJSON, 0, len(users))
	for _, u := range users {
		items = append(items, toAdminUserItemJSON(adminUserListFields{
			ID: u.ID, Username: u.Username, Email: u.Email,
			Name: u.Name, Status: u.Status, Source: u.Source,
			Nickname: u.Nickname, FirstName: u.FirstName, LastName: u.LastName,
			Gender: u.Gender, Country: u.Country, Address: u.Address, Position: u.Position,
			KratosIdentityID: u.KratosIdentityID, Roles: u.Roles,
			TenantID: u.TenantID, CreatedAt: u.CreatedAt.Format(time.RFC3339),
		}))
	}

	respondAdminList(w, r, items, total, listQuery.Page, listQuery.PerPage)
}

func (h *AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	detail, err := h.userSvc.GetUser(r.Context(), id)
	if err != nil || detail == nil || detail.User == nil {
		respondError(w, r, http.StatusNotFound, "user not found")
		return
	}
	respondJSON(w, r, http.StatusOK, toAdminUserDetailJSON(detail.User, detail.Roles))
}

func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		respondError(w, r, http.StatusBadRequest, "username, email, and password required")
		return
	}
	if req.TenantID == "" {
		req.TenantID = "default"
	}

	created, err := h.userSvc.CreateUser(r.Context(), service.CreateAdminUserInput{
		Username:  req.Username,
		Email:     req.Email,
		Password:  req.Password,
		Name:      req.Name,
		Nickname:  req.Nickname,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Gender:    req.Gender,
		Country:   req.Country,
		Address:   req.Address,
		Position:  req.Position,
		TenantID:  req.TenantID,
		RoleIDs:   req.RoleIDs,
	})
	if err != nil {
		h.logger.Warn("admin create user failed", "username", req.Username, "err", err)
		respondError(w, r, http.StatusConflict, err.Error())
		return
	}

	h.logger.Info("user created", "username", req.Username, "id", created.ID)
	h.auditAdmin(r, "admin.user.create", "create", "user", "success", map[string]any{
		"target_user_id": created.ID,
		"username":       created.Username,
		"tenant_id":      created.TenantID,
	})

	respondJSON(w, r, http.StatusCreated, toAdminUserDetailJSON(created, nil))
}

func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Status != nil && *req.Status != "ACTIVE" && h.isProtectedSuperAdmin(w, r, id) {
		return
	}

	u, err := h.userSvc.UpdateUser(r.Context(), id, service.UpdateAdminUserInput{
		Username:  req.Username,
		Email:     req.Email,
		Name:      req.Name,
		Nickname:  req.Nickname,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Gender:    req.Gender,
		Country:   req.Country,
		Address:   req.Address,
		Position:  req.Position,
		Status:    req.Status,
		TenantID:  req.TenantID,
	})
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		respondError(w, r, http.StatusNotFound, "user not found")
		return
	}
	h.auditAdmin(r, "admin.user.update", "update", "user", "success", map[string]any{
		"target_user_id": u.ID,
		"username":       u.Username,
		"tenant_id":      u.TenantID,
	})

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	u, err := h.userRepo.GetUserByID(r.Context(), id)
	if err != nil || u == nil {
		respondError(w, r, http.StatusNotFound, "user not found")
		return
	}
	if u.ID == system.SuperAdminUserID || u.Username == system.SuperAdminUsername {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminSystemUserProtected, "")
		return
	}
	if h.isProtectedSuperAdmin(w, r, u.ID) {
		return
	}

	if _, err := h.userSvc.DeleteUser(r.Context(), id); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.user.delete", "delete", "user", "success", map[string]any{
		"target_user_id": u.ID,
		"username":       u.Username,
		"tenant_id":      u.TenantID,
	})

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *AdminHandler) SetUserStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != "ACTIVE" && req.Status != "DISABLED" {
		respondError(w, r, http.StatusBadRequest, "status must be ACTIVE or DISABLED")
		return
	}
	if req.Status != "ACTIVE" && h.isProtectedSuperAdmin(w, r, id) {
		return
	}
	u, err := h.userSvc.SetStatus(r.Context(), id, req.Status)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		respondError(w, r, http.StatusNotFound, "user not found")
		return
	}
	h.auditAdmin(r, "admin.user.status", "update_status", "user", "success", map[string]any{
		"target_user_id": u.ID,
		"username":       u.Username,
		"status":         u.Status,
		"tenant_id":      u.TenantID,
	})
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *AdminHandler) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Password == "" {
		respondError(w, r, http.StatusBadRequest, "password is required")
		return
	}
	u, err := h.userSvc.ResetPassword(r.Context(), id, req.Password)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		respondError(w, r, http.StatusNotFound, "user not found")
		return
	}
	h.auditAdmin(r, "admin.user.password_reset", "reset_password", "user", "success", map[string]any{
		"target_user_id": u.ID,
		"username":       u.Username,
		"tenant_id":      u.TenantID,
	})
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "password_reset"})
}

// ── Role assignment ──

func (h *AdminHandler) AuditIdentityConsistency(w http.ResponseWriter, r *http.Request) {
	issues, err := h.userSvc.AuditIdentityConsistency(r.Context())
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"ok":     len(issues) == 0,
		"count":  len(issues),
		"issues": issues,
	})
}

func (h *AdminHandler) ProvisionUserIdentity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	var req provisionIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	u, identityID, err := h.userSvc.ProvisionIdentity(r.Context(), id, req.TemporaryPassword)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		respondError(w, r, http.StatusNotFound, "user not found")
		return
	}
	h.auditAdmin(r, "admin.user.identity_provision", "provision_identity", "user", "success", map[string]any{
		"target_user_id":     u.ID,
		"username":           u.Username,
		"kratos_identity_id": identityID,
		"tenant_id":          u.TenantID,
	})
	respondJSON(w, r, http.StatusOK, map[string]string{
		"status":             "provisioned",
		"kratos_identity_id": identityID,
	})
}

func (h *AdminHandler) AssignUserRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		RoleID string `json:"role_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.UserID == "" {
		req.UserID = r.PathValue("userId")
	}
	if req.UserID == "" || req.RoleID == "" {
		respondError(w, r, http.StatusBadRequest, "user_id and role_id required")
		return
	}

	if err := h.userRepo.AssignRole(r.Context(), req.UserID, req.RoleID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.user.assign_role", "assign_role", "user", "success", map[string]any{
		"target_user_id": req.UserID,
		"role_id":        req.RoleID,
	})

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "assigned"})
}

func (h *AdminHandler) UnassignUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	roleID := r.PathValue("roleId")
	if userID == "" || roleID == "" {
		respondError(w, r, http.StatusBadRequest, "missing user_id or role_id")
		return
	}
	if h.isProtectedSuperAdminRoleRemoval(w, r, userID, roleID) {
		return
	}

	if err := h.userRepo.UnassignRole(r.Context(), userID, roleID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.user.unassign_role", "unassign_role", "user", "success", map[string]any{
		"target_user_id": userID,
		"role_id":        roleID,
	})

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "unassigned"})
}

func (h *AdminHandler) ListUserRoles(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	roles, err := h.userRepo.GetUserRoles(r.Context(), userID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]any{"roles": roles})
}

// ── Role CRUD ──

func (h *AdminHandler) ListUserGroups(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	groups, err := h.groupRepo.ListUserGroups(r.Context(), userID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"groups": groups})
}

func (h *AdminHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	listQuery := parseAdminListQuery(r)
	groups, total, err := h.groupRepo.List(r.Context(), repository.ListGroupsParams{
		Page: listQuery.Page, Size: listQuery.PerPage,
		TenantID: firstNonEmpty(r.URL.Query().Get("tenant_id"), r.URL.Query().Get("tenantId")),
		Status:   r.URL.Query().Get("status"),
		Search:   listQuery.Q,
	})
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminList(w, r, groups, total, listQuery.Page, listQuery.PerPage)
}

func (h *AdminHandler) GetGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	group, err := h.groupRepo.GetByID(r.Context(), id)
	if err != nil || group == nil {
		respondError(w, r, http.StatusNotFound, "group not found")
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"group": group})
}

func (h *AdminHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
		TenantID    string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" || req.Name == "" {
		respondError(w, r, http.StatusBadRequest, "code and name required")
		return
	}
	if req.Status == "" {
		req.Status = "ACTIVE"
	}
	if req.Status != "ACTIVE" && req.Status != "DISABLED" {
		respondError(w, r, http.StatusBadRequest, "status must be ACTIVE or DISABLED")
		return
	}
	if req.TenantID == "" {
		req.TenantID = "default"
	}
	group := &domain.Group{
		Code: req.Code, Name: req.Name, Description: req.Description,
		Status: req.Status, TenantID: req.TenantID,
	}
	if err := h.groupRepo.Create(r.Context(), group); err != nil {
		respondError(w, r, http.StatusConflict, "group code may already exist: "+err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.create", "create", "group", "success", map[string]any{"group_id": group.ID, "code": group.Code})
	respondJSON(w, r, http.StatusCreated, group)
}

func (h *AdminHandler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	group, err := h.groupRepo.GetByID(r.Context(), id)
	if err != nil || group == nil {
		respondError(w, r, http.StatusNotFound, "group not found")
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
		TenantID    *string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name != nil {
		group.Name = *req.Name
	}
	if req.Description != nil {
		group.Description = *req.Description
	}
	if req.Status != nil {
		if *req.Status != "ACTIVE" && *req.Status != "DISABLED" {
			respondError(w, r, http.StatusBadRequest, "status must be ACTIVE or DISABLED")
			return
		}
		group.Status = *req.Status
	}
	if req.TenantID != nil {
		group.TenantID = *req.TenantID
	}
	if err := h.groupRepo.Update(r.Context(), group); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByGroup(r.Context(), id); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.update", "update", "group", "success", map[string]any{"group_id": id, "code": group.Code})
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *AdminHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	group, err := h.groupRepo.GetByID(r.Context(), id)
	if err != nil || group == nil {
		respondError(w, r, http.StatusNotFound, "group not found")
		return
	}
	if group.IsSystem || h.groupHasSuperAdminRole(w, r, id) {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if err := h.userRepo.BumpAuthVersionByGroup(r.Context(), id); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.groupRepo.Delete(r.Context(), id); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.delete", "delete", "group", "success", map[string]any{"group_id": id, "code": group.Code})
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *AdminHandler) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	if groupID == "" {
		respondError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	members, err := h.groupRepo.ListMembers(r.Context(), groupID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]adminUserItemJSON, 0, len(members))
	for _, u := range members {
		items = append(items, toAdminUserItemJSON(adminUserListFields{
			ID: u.ID, Username: u.Username, Email: u.Email,
			Name: u.DisplayName, Status: u.Status, Source: u.Source,
			Nickname: u.Nickname, FirstName: u.FirstName, LastName: u.LastName,
			Gender: u.Gender, Country: u.Country, Address: u.Address, Position: u.Position,
			KratosIdentityID: u.KratosIdentityID,
			TenantID:         u.TenantID,
			CreatedAt:        u.CreatedAt.Format(time.RFC3339),
		}))
	}
	respondJSONWithRequest(w, r, http.StatusOK, map[string]any{"items": items})
}

func (h *AdminHandler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if groupID == "" || req.UserID == "" {
		respondError(w, r, http.StatusBadRequest, "group id and user_id required")
		return
	}
	if err := h.groupRepo.AddMember(r.Context(), groupID, req.UserID, r.Header.Get("X-User-Id")); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersion(r.Context(), req.UserID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.add_member", "add_member", "group", "success", map[string]any{"group_id": groupID, "user_id": req.UserID})
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "added"})
}

func (h *AdminHandler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	userID := r.PathValue("userId")
	if groupID == "" || userID == "" {
		respondError(w, r, http.StatusBadRequest, "missing group id or user id")
		return
	}
	if h.groupHasSuperAdminRole(w, r, groupID) {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if err := h.groupRepo.RemoveMember(r.Context(), groupID, userID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersion(r.Context(), userID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.remove_member", "remove_member", "group", "success", map[string]any{"group_id": groupID, "user_id": userID})
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "removed"})
}

func (h *AdminHandler) ListGroupRoles(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	if groupID == "" {
		respondError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	roles, err := h.groupRepo.ListRoles(r.Context(), groupID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"roles": roles})
}

func (h *AdminHandler) AssignGroupRole(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	var req struct {
		RoleID string `json:"role_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if groupID == "" || req.RoleID == "" {
		respondError(w, r, http.StatusBadRequest, "group id and role_id required")
		return
	}
	if err := h.groupRepo.AssignRole(r.Context(), groupID, req.RoleID, r.Header.Get("X-User-Id")); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByGroup(r.Context(), groupID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.assign_role", "assign_role", "group", "success", map[string]any{"group_id": groupID, "role_id": req.RoleID})
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "assigned"})
}

func (h *AdminHandler) UnassignGroupRole(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	roleID := r.PathValue("roleId")
	if groupID == "" || roleID == "" {
		respondError(w, r, http.StatusBadRequest, "missing group id or role id")
		return
	}
	role, err := h.roleRepo.GetByID(r.Context(), roleID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if role != nil && role.Code == system.SuperAdminRoleCode {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if err := h.groupRepo.UnassignRole(r.Context(), groupID, roleID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByGroup(r.Context(), groupID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.unassign_role", "unassign_role", "group", "success", map[string]any{"group_id": groupID, "role_id": roleID})
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "unassigned"})
}

func (h *AdminHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	listQuery := parseAdminListQuery(r)
	tenantID := firstNonEmpty(r.URL.Query().Get("tenant_id"), r.URL.Query().Get("tenantId"))

	roles, total, err := h.roleRepo.List(r.Context(), repository.ListRolesParams{
		Page: listQuery.Page, Size: listQuery.PerPage, TenantID: tenantID, Search: listQuery.Q,
	})
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminList(w, r, roles, total, listQuery.Page, listQuery.PerPage)
}

func (h *AdminHandler) GetRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	role, err := h.roleRepo.GetByID(r.Context(), id)
	if err != nil || role == nil {
		respondError(w, r, http.StatusNotFound, "role not found")
		return
	}
	perms, _ := h.roleRepo.ListPermissionsByRole(r.Context(), id)
	respondJSON(w, r, http.StatusOK, map[string]any{"role": role, "permissions": perms})
}

func (h *AdminHandler) CreateRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" || req.Name == "" {
		respondError(w, r, http.StatusBadRequest, "code and name required")
		return
	}
	if req.TenantID == "" {
		req.TenantID = "default"
	}

	role := &domain.Role{Code: req.Code, Name: req.Name, TenantID: req.TenantID}
	if err := h.roleRepo.Create(r.Context(), role); err != nil {
		respondError(w, r, http.StatusConflict, "role code may already exist: "+err.Error())
		return
	}
	respondJSON(w, r, http.StatusCreated, role)
}

func (h *AdminHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	var req struct {
		Name *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}

	role, err := h.roleRepo.GetByID(r.Context(), id)
	if err != nil || role == nil {
		respondError(w, r, http.StatusNotFound, "role not found")
		return
	}
	if role.Code == system.SuperAdminRoleCode {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if req.Name != nil {
		role.Name = *req.Name
	}
	if err := h.roleRepo.Update(r.Context(), role); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *AdminHandler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	role, err := h.roleRepo.GetByID(r.Context(), id)
	if err != nil || role == nil {
		respondError(w, r, http.StatusNotFound, "role not found")
		return
	}
	if role.Code == system.SuperAdminRoleCode {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if err := h.userRepo.BumpAuthVersionByRole(r.Context(), id); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.roleRepo.Delete(r.Context(), id); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── Permission assignment ──

type assignPermissionRequest struct {
	PermissionID string `json:"permission_id"`
}

func (h *AdminHandler) AssignRolePermission(w http.ResponseWriter, r *http.Request) {
	roleID := r.PathValue("id")
	if roleID == "" {
		respondError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	var req assignPermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.roleRepo.AssignPermission(r.Context(), roleID, req.PermissionID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByRole(r.Context(), roleID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "assigned"})
}

func (h *AdminHandler) UnassignRolePermission(w http.ResponseWriter, r *http.Request) {
	roleID := r.PathValue("id")
	permID := r.PathValue("permId")
	if roleID == "" || permID == "" {
		respondError(w, r, http.StatusBadRequest, "missing role_id or permission_id")
		return
	}
	if h.isProtectedSuperAdminPermissionRemoval(w, r, roleID, permID) {
		return
	}
	if err := h.roleRepo.UnassignPermission(r.Context(), roleID, permID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByRole(r.Context(), roleID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "unassigned"})
}

func (h *AdminHandler) ListRolePermissions(w http.ResponseWriter, r *http.Request) {
	roleID := r.PathValue("id")
	if roleID == "" {
		respondError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	perms, err := h.roleRepo.ListPermissionsByRole(r.Context(), roleID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"permissions": perms})
}

// ── Permission CRUD ──

func (h *AdminHandler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	listQuery := parseAdminListQuery(r)
	mod := r.URL.Query().Get("module")

	perms, total, err := h.roleRepo.ListPermissions(r.Context(), repository.ListPermissionsParams{
		Page: listQuery.Page, Size: listQuery.PerPage, Module: mod,
	})
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminList(w, r, perms, total, listQuery.Page, listQuery.PerPage)
}

func (h *AdminHandler) CreatePermission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		Module    string `json:"module"`
		Resource  string `json:"resource"`
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" || req.Module == "" || req.Resource == "" || req.Operation == "" {
		respondError(w, r, http.StatusBadRequest, "code, module, resource, operation required")
		return
	}

	p := &domain.Permission{
		Code: req.Code, Name: req.Name, Module: req.Module,
		Resource: req.Resource, Operation: req.Operation,
	}
	if err := h.roleRepo.CreatePermission(r.Context(), p); err != nil {
		respondError(w, r, http.StatusConflict, "permission may already exist: "+err.Error())
		return
	}
	respondJSON(w, r, http.StatusCreated, p)
}

func (h *AdminHandler) DeletePermission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing permission id")
		return
	}
	perm, err := h.roleRepo.GetPermissionByID(r.Context(), id)
	if err != nil || perm == nil {
		respondError(w, r, http.StatusNotFound, "permission not found")
		return
	}
	if perm.Code == system.SuperAdminPermissionCode {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminPermissionProtected, "")
		return
	}
	if err := h.userRepo.BumpAuthVersionByPermission(r.Context(), id); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.roleRepo.DeletePermission(r.Context(), id); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *AdminHandler) isProtectedSuperAdmin(w http.ResponseWriter, r *http.Request, userID string) bool {
	hasRole, err := h.userRepo.UserHasRoleCode(r.Context(), userID, system.SuperAdminRoleCode)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if !hasRole {
		return false
	}
	count, err := h.userRepo.CountActiveUsersWithRoleCode(r.Context(), system.SuperAdminRoleCode)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if count <= 1 {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminLastActive, "")
		return true
	}
	return false
}

func (h *AdminHandler) isProtectedSuperAdminRoleRemoval(w http.ResponseWriter, r *http.Request, userID, roleID string) bool {
	role, err := h.roleRepo.GetByID(r.Context(), roleID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if role == nil || role.Code != system.SuperAdminRoleCode {
		return false
	}
	if userID == system.SuperAdminUserID {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return true
	}
	count, err := h.userRepo.CountActiveUsersWithRoleCode(r.Context(), system.SuperAdminRoleCode)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	userHasRole, err := h.userRepo.UserHasRoleCode(r.Context(), userID, system.SuperAdminRoleCode)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if !userHasRole {
		return false
	}
	if count <= 1 {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminLastActive, "")
		return true
	}
	return false
}

func (h *AdminHandler) isProtectedSuperAdminPermissionRemoval(w http.ResponseWriter, r *http.Request, roleID, permID string) bool {
	role, err := h.roleRepo.GetByID(r.Context(), roleID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if role == nil || role.Code != system.SuperAdminRoleCode {
		return false
	}
	perm, err := h.roleRepo.GetPermissionByID(r.Context(), permID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if perm == nil {
		return false
	}
	if perm.Code == system.SuperAdminPermissionCode {
		respondRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminPermissionProtected, "")
		return true
	}
	return false
}

func (h *AdminHandler) groupHasSuperAdminRole(w http.ResponseWriter, r *http.Request, groupID string) bool {
	roles, err := h.groupRepo.ListRoles(r.Context(), groupID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	for _, role := range roles {
		if role.Code == system.SuperAdminRoleCode {
			return true
		}
	}
	return false
}

func (h *AdminHandler) auditAdmin(r *http.Request, eventType, action, resource, result string, details map[string]any) {
	if h.audit == nil {
		return
	}
	h.audit.Event(r.Context(), &domain.AuthEvent{
		EventType: eventType,
		Subject:   r.Header.Get("X-User-Id"),
		Action:    action,
		Resource:  resource,
		Result:    result,
		Details:   details,
		ClientIP:  extractIP(r),
		UserAgent: r.UserAgent(),
		RequestID: r.Header.Get("X-Request-Id"),
	})
}
