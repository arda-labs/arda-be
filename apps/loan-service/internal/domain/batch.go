package domain

import (
	"time"
)

// Batch statuses (shared by lnm_disbursement_batches / lnm_collection_batches
// — same lifecycle as the per-row tables).
const (
	BatchDraft     = "DRAFT"
	BatchSubmitted = "SUBMITTED"
	BatchApproved  = "APPROVED"
	BatchRejected  = "REJECTED"
	BatchCancelled = "CANCELLED"
	BatchPosted    = "POSTED"
)

// Batch types dispatched by the gRPC surface and the workflow workers.
const (
	BatchTypeDisbRegister = "DISB_REGISTER"
	BatchTypeDisbComplete = "DISB_COMPLETE"
	BatchTypeCollection   = "COLLECTION"
)

// DisbursementBatch is one batch disbursement dossier (1 hồ sơ — N hợp đồng,
// EPAS iteration 13). flow_type REGISTER | COMPLETE mirrors the two-phase
// drawdown; COMPLETE rows reference the POSTED REGISTER batch via
// source_batch_id. The workflow case rides LNM_DISB_BATCH_REGISTER_V2 /
// LNM_DISB_BATCH_COMPLETE_V2; rows are lnm_disbursements with batch_id.
type DisbursementBatch struct {
	ID               string            `json:"id"`
	TenantID         string            `json:"tenant_id"`
	OrgCode          string            `json:"org_code,omitempty"`
	FlowType         string            `json:"flow_type"`
	SourceBatchID    string            `json:"source_batch_id,omitempty"`
	TxnDate          string            `json:"txn_date"`
	PaymentMethod    string            `json:"payment_method,omitempty"`
	AccountCode      string            `json:"account_code,omitempty"`
	CurrencyCode     string            `json:"currency_code,omitempty"`
	TotalAmtMinor    int64             `json:"total_amt_minor"`
	Description      string            `json:"description,omitempty"`
	Trader           map[string]string `json:"trader,omitempty"`
	Status           string            `json:"status"`
	WorkflowCaseID   *string           `json:"workflow_case_id,omitempty"`
	WorkflowCaseCode string            `json:"workflow_case_code,omitempty"`
	JournalEntryID   *string           `json:"journal_entry_id,omitempty"`
	CreatedBy        string            `json:"created_by,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	Rows             []Disbursement    `json:"rows,omitempty"`
}

// CollectionBatch is one batch receipt dossier (1 hồ sơ thu nợ — N hợp đồng,
// EPAS LNM.301 iteration 13). Rows are lnm_collections with batch_id; the
// workflow case rides LNM_COLLECTION_BATCH_V2.
type CollectionBatch struct {
	ID                 string            `json:"id"`
	TenantID           string            `json:"tenant_id"`
	OrgCode            string            `json:"org_code,omitempty"`
	TxnDate            string            `json:"txn_date"`
	PaymentMethod      string            `json:"payment_method,omitempty"`
	AccountCode        string            `json:"account_code,omitempty"`
	CurrencyCode       string            `json:"currency_code,omitempty"`
	TotalPrincipalMinor int64            `json:"total_principal_minor"`
	TotalInterestMinor int64             `json:"total_interest_minor"`
	Description        string            `json:"description,omitempty"`
	Trader             map[string]string `json:"trader,omitempty"`
	Status             string            `json:"status"`
	WorkflowCaseID     *string           `json:"workflow_case_id,omitempty"`
	WorkflowCaseCode   string            `json:"workflow_case_code,omitempty"`
	JournalEntryID     *string           `json:"journal_entry_id,omitempty"`
	CreatedBy          string            `json:"created_by,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	Rows               []Collection      `json:"rows,omitempty"`
}
