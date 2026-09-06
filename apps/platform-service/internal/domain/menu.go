package domain

import "time"

// MenuItem is one DB-driven navigation entry (EPAS com_cfg_menu parity).
type MenuItem struct {
	ID                 string    `json:"id"`
	TenantID           *string   `json:"tenant_id,omitempty"`
	ParentID           *string   `json:"parent_id,omitempty"`
	Code               string    `json:"code"`
	Title              string    `json:"title"`
	Path               string    `json:"path"`
	Icon               string    `json:"icon"`
	Remote             string    `json:"remote"`
	RequiredPermission string    `json:"required_permission"`
	SortOrder          int       `json:"sort_order"`
	IsActive           bool      `json:"is_active"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
