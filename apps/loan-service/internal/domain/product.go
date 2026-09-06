package domain

import "time"

// LoanProduct is the credit product catalog (EPAS lnm_cfg_product + dtl,
// consolidated into one table): contracts reference it and inherit term/rate
// defaults; acc_classification ties postings to the finance COA class.
type LoanProduct struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"tenant_id"`
	Code              string    `json:"code"`
	Name              string    `json:"name"`
	ProductType       string    `json:"product_type"` // TERM, LIMIT
	CurrencyCode      string    `json:"currency_code"`
	InterestRateCode  string    `json:"interest_rate_code,omitempty"`
	InterestRate      *float64  `json:"interest_rate,omitempty"`
	LoanTermFrom      *int      `json:"loan_term_from,omitempty"`
	LoanTermTo        *int      `json:"loan_term_to,omitempty"`
	TermUnit          string    `json:"term_unit"`
	MinAmount         *float64  `json:"min_amount,omitempty"`
	MaxAmount         *float64  `json:"max_amount,omitempty"`
	AccClassification string    `json:"acc_classification,omitempty"`
	IsActive          bool      `json:"is_active"`
	Description       *string   `json:"description,omitempty"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
