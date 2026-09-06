package domain

import "time"

// COA v2 definition layer (EPAS be_fac four-layer design): version → chart
// tree → abstract classification map → account-number structure. Business
// flows resolve a classification + date (+ optional debt group / currency)
// to a concrete COA account code for posting.

type CoaVersion struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenantId"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	Scope         string    `json:"scope"`
	ParentCode    *string   `json:"parentCode,omitempty"`
	EffectiveDate string    `json:"effectiveDate"`
	IsDefault     bool      `json:"isDefault"`
	IsActive      bool      `json:"isActive"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type CoaAccount struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenantId"`
	VersionCode   string    `json:"versionCode"`
	AccCode       string    `json:"accCode"`
	Name          string    `json:"name"`
	AccType       string    `json:"accType"`
	AccNature     string    `json:"accNature"`
	ParentCode    *string   `json:"parentCode,omitempty"`
	IsInternal    bool      `json:"isInternal"`
	IsPostable    bool      `json:"isPostable"`
	EffectiveDate string    `json:"effectiveDate"`
	ExpiryDate    *string   `json:"expiryDate,omitempty"`
	Description   *string   `json:"description,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type AccClassCoaMap struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenantId"`
	Classification string    `json:"classification"`
	CoaVersion     string    `json:"coaVersion"`
	CoaAccCode     string    `json:"coaAccCode"`
	DebtGroupCode  string    `json:"debtGroupCode"`
	CurrencyCode   string    `json:"currencyCode"`
	EffectiveDate  string    `json:"effectiveDate"`
	ExpiryDate     *string   `json:"expiryDate,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type AccStructure struct {
	ID        string                 `json:"id"`
	TenantID  string                 `json:"tenantId"`
	Code      string                 `json:"code"`
	Name      string                 `json:"name"`
	AccType   string                 `json:"accType"`
	TotalLength int                  `json:"totalLength"`
	IsActive  bool                   `json:"isActive"`
	Segments  []AccStructureSegment  `json:"segments,omitempty"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
}

type AccStructureSegment struct {
	ID          string `json:"id"`
	StructureID string `json:"structureId"`
	SeqNo       int    `json:"seqNo"`
	Name        string `json:"name"`
	Length      int    `json:"length"`
	Source      string `json:"source"` // FIXED, ORG, CURRENCY, PRODUCT, TERM, SEQUENCE
	FixedValue  string `json:"fixedValue,omitempty"`
	IsRequired  bool   `json:"isRequired"`
}

// ResolvedCoaAccount is the posting-resolution result for a business event.
type ResolvedCoaAccount struct {
	Classification string `json:"classification"`
	CoaVersion     string `json:"coaVersion"`
	CoaAccCode     string `json:"coaAccCode"`
	AccType        string `json:"accType"`
	AccNature      string `json:"accNature"`
}
