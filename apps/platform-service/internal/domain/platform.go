package domain

import "time"

const (
	ScopeGlobal     = "global"
	ScopeTenant     = "tenant"
	ScopeOrg        = "org"
	ScopeBranch     = "branch"
	ScopeDepartment = "department"
)

type Parameter struct {
	ID          string    `json:"id"`
	TenantID    *string   `json:"tenant_id,omitempty"`
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	ValueType   string    `json:"value_type"`
	ScopeType   string    `json:"scope_type"`
	ScopeID     *string   `json:"scope_id,omitempty"`
	Description *string   `json:"description,omitempty"`
	IsSecret    bool      `json:"is_secret"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type LookupCategory struct {
	ID          string    `json:"id"`
	TenantID    *string   `json:"tenant_id,omitempty"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	ScopeType   string    `json:"scope_type"`
	ScopeID     *string   `json:"scope_id,omitempty"`
	IsSystem    bool      `json:"is_system"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type LookupValue struct {
	ID         string    `json:"id"`
	CategoryID string    `json:"category_id"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	SortOrder  int       `json:"sort_order"`
	IsActive   bool      `json:"is_active"`
	Metadata   *string   `json:"metadata,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Organization struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	ParentID      *string   `json:"parent_id,omitempty"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	AdminUnitCode *string   `json:"admin_unit_code,omitempty"`
	Address       *string   `json:"address,omitempty"`
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type GeoAdminUnit struct {
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	FullName      *string   `json:"full_name,omitempty"`
	ParentCode    *string   `json:"parent_code,omitempty"`
	Level         int       `json:"level"`
	UnitType      string    `json:"unit_type"`
	CountryCode   string    `json:"country_code"`
	RegionCode    *string   `json:"region_code,omitempty"`
	EffectiveFrom string    `json:"effective_from"`
	EffectiveTo   *string   `json:"effective_to,omitempty"`
	IsActive      bool      `json:"is_active"`
	Metadata      *string   `json:"metadata,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type FileTemplate struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	Description   *string    `json:"description,omitempty"`
	FileType      string     `json:"file_type"`
	FileURL       string     `json:"file_url"`
	MappingConfig *string    `json:"mapping_config,omitempty"` // String representing JSON mapping configuration
	IsActive      bool       `json:"is_active"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
