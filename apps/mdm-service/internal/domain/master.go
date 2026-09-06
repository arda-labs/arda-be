package domain

import (
	"encoding/json"
	"time"
)

// CatalogItem is the storage shape shared by every simple master-data catalog
// in mdm-service. Domain-specific fields live in `attributes` (jsonb) and are
// validated per catalog by the service layer; this keeps the table shape
// uniform across catalogs while EPAS-style extra columns (risk ratios, limits,
// provisioning) stay typed at the API boundary through the service registry.
type CatalogItem struct {
	ID          string          `json:"id"`
	TenantID    *string         `json:"tenant_id,omitempty"`
	Code        string          `json:"code"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	IsActive    bool            `json:"is_active"`
	Attributes  json.RawMessage `json:"attributes,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// InterestRate is a versioned master-data rate header (central/loan/deposit).
// Concrete values live in tiers keyed by validity window and amount band,
// mirroring EPAS CtgCfgInterestRate + _Dtl without the cross-module copies.
type InterestRate struct {
	ID           string    `json:"id"`
	TenantID     *string   `json:"tenant_id,omitempty"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	RateType     string    `json:"rate_type"`
	ApplyType    string    `json:"apply_type"`
	CurrencyCode *string   `json:"currency_code,omitempty"`
	Description  *string   `json:"description,omitempty"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// InterestRateTier is one rate value valid on [effective_from, effective_to]
// inside an optional amount band [amount_from, amount_to].
type InterestRateTier struct {
	ID            string     `json:"id"`
	RateID        string     `json:"rate_id"`
	EffectiveFrom string     `json:"effective_from"`
	EffectiveTo   *string    `json:"effective_to,omitempty"`
	AmountFrom    *float64   `json:"amount_from,omitempty"`
	AmountTo      *float64   `json:"amount_to,omitempty"`
	RateValue     float64    `json:"rate_value"`
	MinRate       *float64   `json:"min_rate,omitempty"`
	MaxRate       *float64   `json:"max_rate,omitempty"`
	DecisionNo    *string    `json:"decision_no,omitempty"`
	DecisionDate  *string    `json:"decision_date,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
