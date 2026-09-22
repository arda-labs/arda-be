// Package domain holds the CRM business types. Member types model QTDND
// membership (thành viên / vốn góp cổ phần): a customer that holds an equity
// stake, with a maker-checker request pipeline for capital movements.
package domain

import "time"

// Member is one QTDND member (EPAS crm_inf_member).
type Member struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"tenant_id"`
	MemberCode        string    `json:"member_code"`
	CustomerCode      string    `json:"customer_code"`
	OrgCode           string    `json:"org_code,omitempty"`
	MemberBookNo      string    `json:"member_book_no,omitempty"`
	MemberTypeCode    string    `json:"member_type_code"`
	OpenDate          string    `json:"open_date"`
	EstbCapitalMinor  int64     `json:"estb_capital_minor"`
	AddCapitalMinor   int64     `json:"add_capital_minor"`
	TotalCapitalMinor int64     `json:"total_capital_minor"`
	MemberStatus      string    `json:"member_status"`
	LeaveDate         string    `json:"leave_date,omitempty"`
	WorkflowCaseID    *string   `json:"workflow_case_id,omitempty"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	// DataVersion is the optimistic-concurrency token (crm_members.version).
	DataVersion int64 `json:"data_version"`
}

// MemberRequestType is the capital-movement kind.
const (
	MemberRequestRegister   = "REGISTER"   // góp vốn xác lập tư cách thành viên
	MemberRequestAdditional = "ADDITIONAL" // góp vốn bổ sung
	MemberRequestWithdraw   = "WITHDRAW"   // rút vốn
)

// MemberRequestTypes is the closed set the API and workflow accept.
var MemberRequestTypes = []string{MemberRequestRegister, MemberRequestAdditional, MemberRequestWithdraw}

// MemberRequest is one capital-movement request (maker-checker).
type MemberRequest struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	MemberID       string     `json:"member_id"`
	RequestType    string     `json:"request_type"`
	ProductCode    string     `json:"product_code,omitempty"`
	AmountMinor    int64      `json:"amount_minor"`
	CurrencyCode   string     `json:"currency_code"`
	EffectiveDate  string     `json:"effective_date,omitempty"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason,omitempty"`
	WorkflowCaseID *string    `json:"workflow_case_id,omitempty"`
	SubmittedBy    string     `json:"submitted_by,omitempty"`
	SubmittedAt    *time.Time `json:"submitted_at,omitempty"`
	DecidedBy      string     `json:"decided_by,omitempty"`
	DecidedAt      *time.Time `json:"decided_at,omitempty"`
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DataVersion    int64      `json:"data_version"`
}

// MemberProduct is one share product (loại cổ phần: xác lập / bổ sung).
type MemberProduct struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	ShareType      string    `json:"share_type"`
	ParValueMinor  int64     `json:"par_value_minor"`
	MinAmountMinor int64     `json:"min_amount_minor"`
	MaxAmountMinor int64     `json:"max_amount_minor"`
	CurrencyCode   string    `json:"currency_code"`
	IsActive       bool      `json:"is_active"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// IsWithdraw reports whether the request reduces capital (guard direction).
func (r MemberRequest) IsWithdraw() bool { return r.RequestType == MemberRequestWithdraw }
