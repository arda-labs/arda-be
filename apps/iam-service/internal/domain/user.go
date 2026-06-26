package domain

import "time"

// User represents an IAM user.
type User struct {
	ID           string
	Subject      string
	Username     string
	Email        string
	DisplayName  string
	PasswordHash string
	Status       string
	Source       string // "internal", "entra_id", "google", "ldap"...
	TenantID     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Role represents a role assigned to users.
type Role struct {
	ID        string
	Code      string
	Name      string
	Status    string
	TenantID  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Permission represents a granular permission.
type Permission struct {
	ID         string
	Code       string
	Name       string
	Module     string
	Resource   string
	Operation  string
	CreatedAt  time.Time
}

// Organization represents a tenant/org.
type Organization struct {
	ID   string
	Code string
	Name string
}

// UserContext is the enriched user profile returned to gateways and services.
type UserContext struct {
	UserID      string   `json:"userId"`
	Subject     string   `json:"subject"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	TenantID    string   `json:"tenantId"`
	OrgIDs      []string `json:"orgIds"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

// IdentityMapping links an external identity to an internal user.
type IdentityMapping struct {
	ID             string
	ProviderID     string
	ExternalID     string
	InternalUserID string
	IsActive       bool
	LastLoginAt    time.Time
	CreatedAt      time.Time
}

// AuthEvent represents an auditable authentication event.
type AuthEvent struct {
	ID          string
	Timestamp   time.Time
	EventType   string
	Subject     string
	Action      string
	Resource    string
	Result      string // success, failure, denied
	Details     map[string]any
	ClientIP    string
	UserAgent   string
	RequestID   string
	ServiceName string
}
