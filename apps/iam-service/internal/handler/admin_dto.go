package handler

import (
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

type adminUserItemJSON struct {
	ID               string   `json:"id"`
	Username         string   `json:"username"`
	Email            string   `json:"email"`
	Name             string   `json:"name"`
	Nickname         string   `json:"nickname"`
	FirstName        string   `json:"first_name"`
	LastName         string   `json:"last_name"`
	Gender           string   `json:"gender"`
	Country          string   `json:"country"`
	Address          string   `json:"address"`
	Position         string   `json:"position"`
	Status           string   `json:"status"`
	Source           string   `json:"source"`
	KratosIdentityID string   `json:"kratos_identity_id"`
	Roles            []string `json:"roles"`
	TenantID         string   `json:"tenant_id"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
}

func toAdminUserItemJSON(u adminUserListFields) adminUserItemJSON {
	return adminUserItemJSON{
		ID: u.ID, Username: u.Username, Email: u.Email, Name: u.Name,
		Nickname: u.Nickname, FirstName: u.FirstName, LastName: u.LastName,
		Gender: u.Gender, Country: u.Country, Address: u.Address, Position: u.Position,
		Status: u.Status, Source: u.Source, KratosIdentityID: u.KratosIdentityID,
		Roles: u.Roles, TenantID: u.TenantID, CreatedAt: u.CreatedAt,
	}
}

type adminUserListFields struct {
	ID, Username, Email, Name, Nickname, FirstName, LastName string
	Gender, Country, Address, Position, Status, Source       string
	KratosIdentityID, TenantID, CreatedAt                      string
	Roles                                                      []string
}

func toAdminUserDetailJSON(u *domain.User, roles []string) adminUserItemJSON {
	if u == nil {
		return adminUserItemJSON{Roles: roles}
	}
	item := toAdminUserItemJSON(adminUserListFields{
		ID: u.ID, Username: u.Username, Email: u.Email, Name: u.DisplayName,
		Nickname: u.Nickname, FirstName: u.FirstName, LastName: u.LastName,
		Gender: u.Gender, Country: u.Country, Address: u.Address, Position: u.Position,
		Status: u.Status, Source: u.Source, KratosIdentityID: u.KratosIdentityID,
		Roles: roles, TenantID: u.TenantID,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	})
	item.UpdatedAt = u.UpdatedAt.Format(time.RFC3339)
	return item
}

type createUserRequest struct {
	Username  string   `json:"username"`
	Email     string   `json:"email"`
	Password  string   `json:"password"`
	Name      string   `json:"name"`
	Nickname  string   `json:"nickname"`
	FirstName string   `json:"first_name"`
	LastName  string   `json:"last_name"`
	Gender    string   `json:"gender"`
	Country   string   `json:"country"`
	Address   string   `json:"address"`
	Position  string   `json:"position"`
	TenantID  string   `json:"tenant_id"`
	RoleIDs   []string `json:"role_ids,omitempty"`
}

type updateUserRequest struct {
	Username  *string `json:"username,omitempty"`
	Email     *string `json:"email,omitempty"`
	Name      *string `json:"name,omitempty"`
	Nickname  *string `json:"nickname,omitempty"`
	FirstName *string `json:"first_name,omitempty"`
	LastName  *string `json:"last_name,omitempty"`
	Gender    *string `json:"gender,omitempty"`
	Country   *string `json:"country,omitempty"`
	Address   *string `json:"address,omitempty"`
	Position  *string `json:"position,omitempty"`
	Status    *string `json:"status,omitempty"`
	TenantID  *string `json:"tenant_id,omitempty"`
}

type provisionIdentityRequest struct {
	TemporaryPassword string `json:"temporary_password"`
}
