package domain

import "time"

// User represents an IAM user.
type User struct {
	ID               string
	Subject          string
	KratosIdentityID string
	Username         string
	Email            string
	DisplayName      string
	Nickname         string
	FirstName        string
	LastName         string
	PhoneNumber      string
	Birthdate        string
	Gender           string
	Address          string
	Country          string
	Status           string
	Source           string // "internal", "entra_id", "google", "ldap"...
	TenantID         string
	AvatarFileID     string
	PictureURL       string
	CoverFileID      string
	CoverImageURL    string
	Department       string
	Position         string
	EmployeeID       string
	ApprovalLevel    string
	DailyLimit       string
	Bio              string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Role represents a role assigned to users.
type Role struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	TenantID  string    `json:"tenantId"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Permission represents a granular permission.
type Permission struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Module    string    `json:"module"`
	Resource  string    `json:"resource"`
	Operation string    `json:"operation"`
	CreatedAt time.Time `json:"createdAt"`
}

type Group struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	TenantID    string    `json:"tenantId"`
	IsSystem    bool      `json:"isSystem"`
	MemberCount int       `json:"memberCount"`
	RoleCount   int       `json:"roleCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type RoleAssignment struct {
	ID            string    `json:"id"`
	PrincipalType string    `json:"principalType"`
	PrincipalID   string    `json:"principalId"`
	RoleID        string    `json:"roleId"`
	RoleCode      string    `json:"roleCode,omitempty"`
	RoleName      string    `json:"roleName,omitempty"`
	ScopeType     string    `json:"scopeType"`
	ScopeID       string    `json:"scopeId,omitempty"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
}

// Organization represents a tenant/org.
type Organization struct {
	ID   string
	Code string
	Name string
}

// UserContext is the enriched user profile returned to gateways and services.
type UserContext struct {
	UserID        string   `json:"userId"`
	Subject       string   `json:"subject"`
	Username      string   `json:"username"`
	Email         string   `json:"email"`
	DisplayName   string   `json:"displayName,omitempty"`
	Nickname      string   `json:"nickname,omitempty"`
	FirstName     string   `json:"firstName,omitempty"`
	LastName      string   `json:"lastName,omitempty"`
	PhoneNumber   string   `json:"phoneNumber,omitempty"`
	Birthdate     string   `json:"birthdate,omitempty"`
	Gender        string   `json:"gender,omitempty"`
	Address       string   `json:"address,omitempty"`
	Country       string   `json:"country,omitempty"`
	PictureURL    string   `json:"picture,omitempty"`
	AvatarFileID  string   `json:"avatarFileId,omitempty"`
	CoverImageURL string   `json:"coverImage,omitempty"`
	CoverFileID   string   `json:"coverFileId,omitempty"`
	TenantID      string   `json:"tenantId"`
	OrgIDs        []string `json:"orgIds"`
	GroupIDs      []string `json:"groupIds"`
	Roles         []string `json:"roles"`
	Permissions   []string `json:"permissions"`
	AuthVersion   int64    `json:"authVersion"`
	Department    string   `json:"department,omitempty"`
	Position      string   `json:"position,omitempty"`
	EmployeeID    string   `json:"employeeId,omitempty"`
	ApprovalLevel string   `json:"approvalLevel,omitempty"`
	DailyLimit    string   `json:"dailyLimit,omitempty"`
	Bio           string   `json:"bio,omitempty"`
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
	ID          string         `json:"id"`
	Timestamp   time.Time      `json:"timestamp"`
	EventType   string         `json:"eventType"`
	Subject     string         `json:"subject"`
	Action      string         `json:"action"`
	Resource    string         `json:"resource"`
	Result      string         `json:"result"` // success, failure, denied
	Details     map[string]any `json:"details"`
	ClientIP    string         `json:"clientIp"`
	UserAgent   string         `json:"userAgent"`
	RequestID   string         `json:"requestId"`
	ServiceName string         `json:"serviceName"`
}
