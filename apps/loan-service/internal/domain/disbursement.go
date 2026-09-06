package domain

import (
	"encoding/json"
	"time"
)

// Disbursement statuses.
const (
	DisbursementDraft     = "DRAFT"
	DisbursementSubmitted = "SUBMITTED"
	DisbursementApproved  = "APPROVED"
	DisbursementRejected  = "REJECTED"
	DisbursementCancelled = "CANCELLED"
	DisbursementPosted    = "POSTED"
)

// Disbursement is one drawdown request against an agreement (EPAS LNM.300.02
// Giải ngân). Approved flow posts via the finance PostingService
// (LNM_DISBURSEMENT rule card) then settles the agreement outstanding.
type Disbursement struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	ContractCode     string    `json:"contract_code"`
	AgreementCode    string    `json:"agreement_code"`
	DisburseDate     string    `json:"disburse_date"`
	DisburseAmtMinor int64     `json:"disburse_amt_minor"`
	CurrencyCode     string    `json:"currency_code"`
	FundSourceCode   string    `json:"fund_source_code"`
	Status           string    `json:"status"`
	Payload          json.RawMessage `json:"payload,omitempty"`
	WorkflowCaseID   *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID   *string   `json:"journal_entry_id,omitempty"`
	OrgCode          string    `json:"org_code,omitempty"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
