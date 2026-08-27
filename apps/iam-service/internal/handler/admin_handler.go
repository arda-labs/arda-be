package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/audit"
	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	"github.com/arda-labs/arda/apps/iam-service/internal/system"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardaexport "github.com/arda-labs/arda/libs/go/arda-export"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// AdminHandler manages users, roles, permissions.
type AdminHandler struct {
	userRepo   *repository.UserRepository
	roleRepo   *repository.RoleRepository
	groupRepo  *repository.GroupRepository
	tenantRepo *repository.TenantRepository
	userSvc    *service.AdminUserService
	audit      *audit.Logger
	logger     *slog.Logger
}

// NewAdminHandler creates an admin handler.
func NewAdminHandler(userRepo *repository.UserRepository, roleRepo *repository.RoleRepository, groupRepo *repository.GroupRepository, userSvc *service.AdminUserService, auditLogger *audit.Logger, tenantRepos ...*repository.TenantRepository) *AdminHandler {
	var tenantRepo *repository.TenantRepository
	if len(tenantRepos) > 0 {
		tenantRepo = tenantRepos[0]
	}
	return &AdminHandler{
		userRepo:   userRepo,
		roleRepo:   roleRepo,
		groupRepo:  groupRepo,
		tenantRepo: tenantRepo,
		userSvc:    userSvc,
		audit:      auditLogger,
		logger:     slog.Default(),
	}
}

func (h *AdminHandler) requireActiveTenant(r *http.Request, tenantID string) error {
	if h.tenantRepo == nil {
		return nil
	}
	exists, err := h.tenantRepo.Exists(r.Context(), tenantID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("tenant does not exist or is not active")
	}
	return nil
}

// ── User CRUD ──

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	listQuery := parseAdminListQuery(r)
	status := r.URL.Query().Get("status")
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}

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
		respondAdminRequestError(w, r, http.StatusInternalServerError, ardaerrors.CodeInternal, "list users failed")
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

// ExportUsers handles large-scale direct streaming export of users in XLSX or CSV format.
func (h *AdminHandler) ExportUsers(w http.ResponseWriter, r *http.Request) {
	listQuery := parseAdminListQuery(r)
	status := r.URL.Query().Get("status")
	formatStr := r.URL.Query().Get("format")
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}

	format := ardaexport.NormalizeFormat(formatStr)
	filename := fmt.Sprintf("users_export_%s", time.Now().Format("20060102_150405"))

	cols := []ardaexport.Column{
		{Header: "ID", Key: "id", Type: ardaexport.CellTypeCode},
		{Header: "Tên đăng nhập", Key: "username", Type: ardaexport.CellTypeString},
		{Header: "Email", Key: "email", Type: ardaexport.CellTypeString},
		{Header: "Tên hiển thị", Key: "displayName", Type: ardaexport.CellTypeString},
		{Header: "Họ", Key: "firstName", Type: ardaexport.CellTypeString},
		{Header: "Tên", Key: "lastName", Type: ardaexport.CellTypeString},
		{Header: "Số điện thoại", Key: "phoneNumber", Type: ardaexport.CellTypeCode},
		{
			Header: "Trạng thái",
			Key:    "status",
			Type:   ardaexport.CellTypeString,
			Formatter: func(v any) any {
				if s, ok := v.(string); ok {
					if s == "ACTIVE" {
						return "Đang hoạt động"
					}
					return "Đã vô hiệu"
				}
				return v
			},
		},
		{Header: "Ngày tạo", Key: "createdAt", Type: ardaexport.CellTypeDate},
	}

	opts := ardaexport.StreamOptions{
		Title:     "BÁO CÁO DANH SÁCH NGƯỜI DÙNG HỆ THỐNG",
		SheetName: "Users",
		Columns:   cols,
		Locale:    "vi-VN",
	}

	err := ardaexport.ServeStreamHTTP(w, r, format, filename, func(ctx context.Context, out io.Writer) error {
		rows, err := h.userRepo.StreamUsers(ctx, repository.ListUsersParams{
			Status:    status,
			Search:    listQuery.Q,
			TenantID:  tenantID,
			SortField: listQuery.Sort,
			SortOrder: listSortOrder(listQuery),
		})
		if err != nil {
			return err
		}
		defer rows.Close()

		supplier := func() ([]any, error) {
			if !rows.Next() {
				if rows.Err() != nil {
					return nil, rows.Err()
				}
				return nil, io.EOF
			}
			var id, username, email, displayName, firstName, lastName, phoneNumber, userStatus string
			var createdAt time.Time
			if err := rows.Scan(&id, &username, &email, &displayName, &firstName, &lastName, &phoneNumber, &userStatus, &createdAt); err != nil {
				return nil, err
			}
			return []any{id, username, email, displayName, firstName, lastName, phoneNumber, userStatus, createdAt}, nil
		}

		if format == ardaexport.FormatCSV {
			return ardaexport.StreamCSV(ctx, out, opts, supplier)
		}
		return ardaexport.StreamXLSX(ctx, out, opts, supplier)
	})

	if err != nil {
		h.logger.Error("export users streaming failed", "err", err)
	}
}

func (h *AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	detail, err := h.userSvc.GetUser(r.Context(), id, tenantID)
	if err != nil || detail == nil || detail.User == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return
	}
	respondAdminJSON(w, r, http.StatusOK, toAdminUserDetailJSON(detail.User, detail.Roles))
}

func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		respondAdminError(w, r, http.StatusBadRequest, "username, email, and password required")
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	if req.TenantID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "tenant_id is required for the target user")
		return
	}
	if _, ok := validateAdminTargetTenant(w, r, req.TenantID); !ok {
		return
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
		respondAdminError(w, r, http.StatusConflict, err.Error())
		return
	}

	h.logger.Info("user created", "username", req.Username, "id", created.ID)
	h.auditAdmin(r, "admin.user.create", "create", "user", "success", map[string]any{
		"target_user_id": created.ID,
		"username":       created.Username,
		"tenant_id":      created.TenantID,
	})

	respondAdminJSON(w, r, http.StatusCreated, toAdminUserDetailJSON(created, nil))
}

func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if req.TenantID != nil && strings.TrimSpace(*req.TenantID) != tenantID {
		respondAdminError(w, r, http.StatusBadRequest, "tenant_id must match the target resource scope")
		return
	}

	if req.Status != nil && *req.Status != "ACTIVE" && h.isProtectedSuperAdmin(w, r, id) {
		return
	}

	u, err := h.userSvc.UpdateUser(r.Context(), id, tenantID, service.UpdateAdminUserInput{
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
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return
	}
	h.auditAdmin(r, "admin.user.update", "update", "user", "success", map[string]any{
		"target_user_id": u.ID,
		"username":       u.Username,
		"tenant_id":      u.TenantID,
	})

	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	u, err := h.userRepo.GetUserByIDScoped(r.Context(), id, tenantID)
	if err != nil || u == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return
	}
	if u.ID == system.SuperAdminUserID || u.Username == system.SuperAdminUsername {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminSystemUserProtected, "")
		return
	}
	if h.isProtectedSuperAdmin(w, r, u.ID) {
		return
	}

	if _, err := h.userSvc.DeleteUser(r.Context(), id, tenantID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.user.delete", "delete", "user", "success", map[string]any{
		"target_user_id": u.ID,
		"username":       u.Username,
		"tenant_id":      u.TenantID,
	})

	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *AdminHandler) SetUserStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != "ACTIVE" && req.Status != "DISABLED" {
		respondAdminError(w, r, http.StatusBadRequest, "status must be ACTIVE or DISABLED")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if req.Status != "ACTIVE" && h.isProtectedSuperAdmin(w, r, id) {
		return
	}
	u, err := h.userSvc.SetStatus(r.Context(), id, tenantID, req.Status)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return
	}
	h.auditAdmin(r, "admin.user.status", "update_status", "user", "success", map[string]any{
		"target_user_id": u.ID,
		"username":       u.Username,
		"status":         u.Status,
		"tenant_id":      u.TenantID,
	})
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *AdminHandler) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Password == "" {
		respondAdminError(w, r, http.StatusBadRequest, "password is required")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	u, err := h.userSvc.ResetPassword(r.Context(), id, tenantID, req.Password)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return
	}
	h.auditAdmin(r, "admin.user.password_reset", "reset_password", "user", "success", map[string]any{
		"target_user_id": u.ID,
		"username":       u.Username,
		"tenant_id":      u.TenantID,
	})
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "password_reset"})
}

// ── Role assignment ──

func (h *AdminHandler) AuditIdentityConsistency(w http.ResponseWriter, r *http.Request) {
	issues, err := h.userSvc.AuditIdentityConsistency(r.Context())
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]any{
		"ok":     len(issues) == 0,
		"count":  len(issues),
		"issues": issues,
	})
}

func (h *AdminHandler) ProvisionUserIdentity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	var req provisionIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	u, identityID, err := h.userSvc.ProvisionIdentity(r.Context(), id, tenantID, req.TemporaryPassword)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return
	}
	h.auditAdmin(r, "admin.user.identity_provision", "provision_identity", "user", "success", map[string]any{
		"target_user_id":     u.ID,
		"username":           u.Username,
		"kratos_identity_id": identityID,
		"tenant_id":          u.TenantID,
	})
	respondAdminJSON(w, r, http.StatusOK, map[string]string{
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
		respondAdminError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.UserID == "" {
		req.UserID = r.PathValue("userId")
	}
	if req.UserID == "" || req.RoleID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "user_id and role_id required")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if user, err := h.userRepo.GetUserByIDScoped(r.Context(), req.UserID, tenantID); err != nil || user == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return
	}

	if err := h.userRepo.AssignRole(r.Context(), req.UserID, req.RoleID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.user.assign_role", "assign_role", "user", "success", map[string]any{
		"target_user_id": req.UserID,
		"role_id":        req.RoleID,
	})

	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "assigned"})
}

func (h *AdminHandler) UnassignUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	roleID := r.PathValue("roleId")
	if userID == "" || roleID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user_id or role_id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if user, err := h.userRepo.GetUserByIDScoped(r.Context(), userID, tenantID); err != nil || user == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return
	}
	if h.isProtectedSuperAdminRoleRemoval(w, r, userID, roleID, tenantID) {
		return
	}

	if err := h.userRepo.UnassignRole(r.Context(), userID, roleID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.user.unassign_role", "unassign_role", "user", "success", map[string]any{
		"target_user_id": userID,
		"role_id":        roleID,
	})

	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "unassigned"})
}

func (h *AdminHandler) ListUserRoles(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user id")
		return
	}

	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	roles, err := h.userRepo.GetUserRolesScoped(r.Context(), userID, tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondAdminJSON(w, r, http.StatusOK, map[string]any{"roles": roles})
}

// ── Role CRUD ──

func (h *AdminHandler) ListUserGroups(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing user id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	groups, err := h.groupRepo.ListUserGroupsScoped(r.Context(), userID, tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]any{"groups": groups})
}

func (h *AdminHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	listQuery := parseAdminListQuery(r)
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	groups, total, err := h.groupRepo.List(r.Context(), repository.ListGroupsParams{
		Page: listQuery.Page, Size: listQuery.PerPage,
		TenantID: tenantID,
		Status:   r.URL.Query().Get("status"),
		Search:   listQuery.Q,
	})
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminList(w, r, groups, total, listQuery.Page, listQuery.PerPage)
}

func (h *AdminHandler) GetGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	group, err := h.groupRepo.GetByIDScoped(r.Context(), id, tenantID)
	if err != nil || group == nil {
		respondAdminError(w, r, http.StatusNotFound, "group not found")
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]any{"group": group})
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
		respondAdminError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" || req.Name == "" {
		respondAdminError(w, r, http.StatusBadRequest, "code and name required")
		return
	}
	if req.Status == "" {
		req.Status = "ACTIVE"
	}
	if req.Status != "ACTIVE" && req.Status != "DISABLED" {
		respondAdminError(w, r, http.StatusBadRequest, "status must be ACTIVE or DISABLED")
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	if req.TenantID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "tenant_id is required for the target group")
		return
	}
	if _, ok := validateAdminTargetTenant(w, r, req.TenantID); !ok {
		return
	}
	if err := h.requireActiveTenant(r, req.TenantID); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	group := &domain.Group{
		Code: req.Code, Name: req.Name, Description: req.Description,
		Status: req.Status, TenantID: req.TenantID,
	}
	if err := h.groupRepo.Create(r.Context(), group); err != nil {
		respondAdminError(w, r, http.StatusConflict, "group code may already exist: "+err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.create", "create", "group", "success", map[string]any{"group_id": group.ID, "code": group.Code})
	respondAdminJSON(w, r, http.StatusCreated, group)
}

func (h *AdminHandler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	group, err := h.groupRepo.GetByIDScoped(r.Context(), id, tenantID)
	if err != nil || group == nil {
		respondAdminError(w, r, http.StatusNotFound, "group not found")
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
		TenantID    *string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid body")
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
			respondAdminError(w, r, http.StatusBadRequest, "status must be ACTIVE or DISABLED")
			return
		}
		group.Status = *req.Status
	}
	if req.TenantID != nil {
		group.TenantID = strings.TrimSpace(*req.TenantID)
		if group.TenantID == "" || group.TenantID != tenantID {
			respondAdminError(w, r, http.StatusBadRequest, "tenant_id cannot be empty")
			return
		}
	}
	if err := h.groupRepo.UpdateScoped(r.Context(), group, tenantID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByGroup(r.Context(), id); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.update", "update", "group", "success", map[string]any{"group_id": id, "code": group.Code})
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *AdminHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	group, err := h.groupRepo.GetByIDScoped(r.Context(), id, tenantID)
	if err != nil || group == nil {
		respondAdminError(w, r, http.StatusNotFound, "group not found")
		return
	}
	if group.IsSystem || h.groupHasSuperAdminRole(w, r, id, tenantID) {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if err := h.userRepo.BumpAuthVersionByGroup(r.Context(), id); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.groupRepo.DeleteScoped(r.Context(), id, tenantID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.delete", "delete", "group", "success", map[string]any{"group_id": id, "code": group.Code})
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *AdminHandler) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	if groupID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	members, err := h.groupRepo.ListMembersScoped(r.Context(), groupID, tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
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
	respondAdminJSONWithRequest(w, r, http.StatusOK, map[string]any{"items": items})
}

func (h *AdminHandler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if groupID == "" || req.UserID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "group id and user_id required")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if err := h.groupRepo.AddMemberScoped(r.Context(), groupID, req.UserID, tenantID, r.Header.Get("X-User-Id")); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersion(r.Context(), req.UserID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.add_member", "add_member", "group", "success", map[string]any{"group_id": groupID, "user_id": req.UserID})
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "added"})
}

func (h *AdminHandler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	userID := r.PathValue("userId")
	if groupID == "" || userID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing group id or user id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if h.groupHasSuperAdminRole(w, r, groupID, tenantID) {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if err := h.groupRepo.RemoveMemberScoped(r.Context(), groupID, userID, tenantID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersion(r.Context(), userID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.remove_member", "remove_member", "group", "success", map[string]any{"group_id": groupID, "user_id": userID})
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "removed"})
}

func (h *AdminHandler) ListGroupRoles(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	if groupID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing group id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	roles, err := h.groupRepo.ListRolesScoped(r.Context(), groupID, tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]any{"roles": roles})
}

func (h *AdminHandler) AssignGroupRole(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	var req struct {
		RoleID string `json:"role_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if groupID == "" || req.RoleID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "group id and role_id required")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if err := h.groupRepo.AssignRoleScoped(r.Context(), groupID, req.RoleID, tenantID, r.Header.Get("X-User-Id")); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByGroup(r.Context(), groupID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.assign_role", "assign_role", "group", "success", map[string]any{"group_id": groupID, "role_id": req.RoleID})
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "assigned"})
}

func (h *AdminHandler) UnassignGroupRole(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	roleID := r.PathValue("roleId")
	if groupID == "" || roleID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing group id or role id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	role, err := h.roleRepo.GetByIDScoped(r.Context(), roleID, tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if role != nil && role.Code == system.SuperAdminRoleCode {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if err := h.groupRepo.UnassignRoleScoped(r.Context(), groupID, roleID, tenantID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByGroup(r.Context(), groupID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	h.auditAdmin(r, "admin.group.unassign_role", "unassign_role", "group", "success", map[string]any{"group_id": groupID, "role_id": roleID})
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "unassigned"})
}

func (h *AdminHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	listQuery := parseAdminListQuery(r)
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}

	roles, total, err := h.roleRepo.List(r.Context(), repository.ListRolesParams{
		Page: listQuery.Page, Size: listQuery.PerPage, TenantID: tenantID, Search: listQuery.Q,
	})
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminList(w, r, roles, total, listQuery.Page, listQuery.PerPage)
}

func (h *AdminHandler) GetRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	role, err := h.roleRepo.GetByIDScoped(r.Context(), id, tenantID)
	if err != nil || role == nil {
		respondAdminError(w, r, http.StatusNotFound, "role not found")
		return
	}
	perms, _ := h.roleRepo.ListPermissionsByRoleScoped(r.Context(), id, tenantID)
	respondAdminJSON(w, r, http.StatusOK, map[string]any{"role": role, "permissions": perms})
}

func (h *AdminHandler) CreateRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" || req.Name == "" {
		respondAdminError(w, r, http.StatusBadRequest, "code and name required")
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	if req.TenantID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "tenant_id is required for the target role")
		return
	}
	if _, ok := validateAdminTargetTenant(w, r, req.TenantID); !ok {
		return
	}

	if err := h.requireActiveTenant(r, req.TenantID); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	role := &domain.Role{Code: req.Code, Name: req.Name, TenantID: req.TenantID}
	if err := h.roleRepo.Create(r.Context(), role); err != nil {
		respondAdminError(w, r, http.StatusConflict, "role code may already exist: "+err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusCreated, role)
}

func (h *AdminHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	var req struct {
		Name *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid body")
		return
	}

	role, err := h.roleRepo.GetByIDScoped(r.Context(), id, tenantID)
	if err != nil || role == nil {
		respondAdminError(w, r, http.StatusNotFound, "role not found")
		return
	}
	if role.Code == system.SuperAdminRoleCode {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if req.Name != nil {
		role.Name = *req.Name
	}
	if err := h.roleRepo.UpdateScoped(r.Context(), role, tenantID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *AdminHandler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	role, err := h.roleRepo.GetByIDScoped(r.Context(), id, tenantID)
	if err != nil || role == nil {
		respondAdminError(w, r, http.StatusNotFound, "role not found")
		return
	}
	if role.Code == system.SuperAdminRoleCode {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return
	}
	if err := h.userRepo.BumpAuthVersionByRole(r.Context(), id); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.roleRepo.DeleteScoped(r.Context(), id, tenantID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── Permission assignment ──

type assignPermissionRequest struct {
	PermissionID string `json:"permission_id"`
}

func (h *AdminHandler) AssignRolePermission(w http.ResponseWriter, r *http.Request) {
	roleID := r.PathValue("id")
	if roleID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	var req assignPermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondAdminError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if err := h.roleRepo.AssignPermissionScoped(r.Context(), roleID, req.PermissionID, tenantID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByRole(r.Context(), roleID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "assigned"})
}

func (h *AdminHandler) UnassignRolePermission(w http.ResponseWriter, r *http.Request) {
	roleID := r.PathValue("id")
	permID := r.PathValue("permId")
	if roleID == "" || permID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing role_id or permission_id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	if h.isProtectedSuperAdminPermissionRemoval(w, r, roleID, permID, tenantID) {
		return
	}
	if err := h.roleRepo.UnassignPermissionScoped(r.Context(), roleID, permID, tenantID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.userRepo.BumpAuthVersionByRole(r.Context(), roleID); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "unassigned"})
}

func (h *AdminHandler) ListRolePermissions(w http.ResponseWriter, r *http.Request) {
	roleID := r.PathValue("id")
	if roleID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing role id")
		return
	}
	tenantID, ok := requiredAdminTargetTenant(w, r)
	if !ok {
		return
	}
	perms, err := h.roleRepo.ListPermissionsByRoleScoped(r.Context(), roleID, tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]any{"permissions": perms})
}

// ── Permission CRUD ──

func (h *AdminHandler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	listQuery := parseAdminListQuery(r)
	mod := r.URL.Query().Get("module")

	perms, total, err := h.roleRepo.ListPermissions(r.Context(), repository.ListPermissionsParams{
		Page: listQuery.Page, Size: listQuery.PerPage, Module: mod,
	})
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(listQuery.Page, listQuery.PerPage, total, perms), "permissions listed")
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
		respondAdminError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" || req.Module == "" || req.Resource == "" || req.Operation == "" {
		respondAdminError(w, r, http.StatusBadRequest, "code, module, resource, operation required")
		return
	}

	p := &domain.Permission{
		Code: req.Code, Name: req.Name, Module: req.Module,
		Resource: req.Resource, Operation: req.Operation,
	}
	if err := h.roleRepo.CreatePermission(r.Context(), p); err != nil {
		respondAdminError(w, r, http.StatusConflict, "permission may already exist: "+err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusCreated, p)
}

func (h *AdminHandler) DeletePermission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondAdminError(w, r, http.StatusBadRequest, "missing permission id")
		return
	}
	perm, err := h.roleRepo.GetPermissionByID(r.Context(), id)
	if err != nil || perm == nil {
		respondAdminError(w, r, http.StatusNotFound, "permission not found")
		return
	}
	if perm.Code == system.SuperAdminPermissionCode {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminPermissionProtected, "")
		return
	}
	if err := h.userRepo.BumpAuthVersionByPermission(r.Context(), id); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.roleRepo.DeletePermission(r.Context(), id); err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondAdminJSON(w, r, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *AdminHandler) isProtectedSuperAdmin(w http.ResponseWriter, r *http.Request, userID string) bool {
	hasRole, err := h.userRepo.UserHasRoleCode(r.Context(), userID, system.SuperAdminRoleCode)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if !hasRole {
		return false
	}
	count, err := h.userRepo.CountActiveUsersWithRoleCode(r.Context(), system.SuperAdminRoleCode)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if count <= 1 {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminLastActive, "")
		return true
	}
	return false
}

func (h *AdminHandler) isProtectedSuperAdminRoleRemoval(w http.ResponseWriter, r *http.Request, userID, roleID, tenantID string) bool {
	user, err := h.userRepo.GetUserByIDScoped(r.Context(), userID, tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if user == nil {
		respondAdminError(w, r, http.StatusNotFound, "user not found")
		return true
	}
	role, err := h.roleRepo.GetByIDScoped(r.Context(), roleID, user.TenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if role == nil || role.Code != system.SuperAdminRoleCode {
		return false
	}
	if userID == system.SuperAdminUserID {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminRoleProtected, "")
		return true
	}
	count, err := h.userRepo.CountActiveUsersWithRoleCode(r.Context(), system.SuperAdminRoleCode)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	userHasRole, err := h.userRepo.UserHasRoleCode(r.Context(), userID, system.SuperAdminRoleCode)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if !userHasRole {
		return false
	}
	if count <= 1 {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminLastActive, "")
		return true
	}
	return false
}

func (h *AdminHandler) isProtectedSuperAdminPermissionRemoval(w http.ResponseWriter, r *http.Request, roleID, permID, tenantID string) bool {
	role, err := h.roleRepo.GetByIDScoped(r.Context(), roleID, tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if role == nil || role.Code != system.SuperAdminRoleCode {
		return false
	}
	perm, err := h.roleRepo.GetPermissionByID(r.Context(), permID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
		return true
	}
	if perm == nil {
		return false
	}
	if perm.Code == system.SuperAdminPermissionCode {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeSuperAdminPermissionProtected, "")
		return true
	}
	return false
}

func (h *AdminHandler) groupHasSuperAdminRole(w http.ResponseWriter, r *http.Request, groupID, tenantID string) bool {
	roles, err := h.groupRepo.ListRolesScoped(r.Context(), groupID, tenantID)
	if err != nil {
		respondAdminError(w, r, http.StatusInternalServerError, err.Error())
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
