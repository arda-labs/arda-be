package domain

import "time"

// Tenant is an Arda business tenant. The system control-plane scope is not a
// Tenant and must never be used as a business tenant fallback.
type Tenant struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TenantMembership describes a user's application access to one tenant.
type TenantMembership struct {
	TenantID     string `json:"tenantId"`
	TenantCode   string `json:"tenantCode"`
	TenantName   string `json:"tenantName"`
	TenantStatus string `json:"tenantStatus"`
	Status       string `json:"status"`
	IsDefault    bool   `json:"isDefault"`
}

type TenantMember struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
	IsDefault   bool   `json:"isDefault"`
}
