package domain

import (
	"encoding/json"
	"time"
)

// Collection statuses.
const (
	CollectionDraft     = "DRAFT"
	CollectionSubmitted = "SUBMITTED"
	CollectionApproved  = "APPROVED"
	CollectionRejected  = "REJECTED"
	CollectionCancelled = "CANCELLED"
	CollectionPosted    = "POSTED"
)

// Collection is one principal + interest receipt against an agreement
// (EPAS LNM.301.02 Thu nợ). Approved flow posts the 4-line LNM_COLLECTION
// rule card then reduces agreement outstanding/collected counters.
type Collection struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	ContractCode     string          `json:"contract_code"`
	AgreementCode    string          `json:"agreement_code"`
	CollectionDate   string          `json:"collection_date"`
	PrincipalMinor   int64           `json:"principal_minor"`
	InterestMinor    int64           `json:"interest_minor"`
	// OverdueInterestMinor is the overdue-interest share collected with this
	// receipt (EPAS 301 iteration 13; posted with the interest legs).
	OverdueInterestMinor int64       `json:"overdue_interest_minor,omitempty"`
	CurrencyCode     string          `json:"currency_code"`
	// BatchID links the row to its batch dossier (iteration 13); empty on
	// legacy single-row collections.
	BatchID          string          `json:"batch_id,omitempty"`
	Status           string          `json:"status"`
	Payload          json.RawMessage `json:"payload,omitempty"`
	WorkflowCaseID   *string         `json:"workflow_case_id,omitempty"`
	WorkflowCaseCode string          `json:"workflow_case_code,omitempty"`
	JournalEntryID   *string         `json:"journal_entry_id,omitempty"`
	OrgCode          string          `json:"org_code,omitempty"`
	CreatedBy        string          `json:"created_by"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}
