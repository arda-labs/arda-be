package service

import (
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
)

// Batch case types (iteration 13): the batch dossier rides its own v2 case
// type — the per-row single-disbursement case types stay untouched.
const (
	BatchDisbRegisterCaseType = "LNM_DISB_BATCH_REGISTER_V2"
	BatchDisbCompleteCaseType = "LNM_DISB_BATCH_COMPLETE_V2"
	BatchCollectionCaseType   = "LNM_COLLECTION_BATCH_V2"
)

// BatchRowInput is one drawdown row of a batch register request.
type BatchRowInput struct {
	ContractCode  string `json:"contract_code"`
	AgreementCode string `json:"agreement_code"`
	// PlanCode is optional input metadata; the authoritative value comes
	// from the agreement (lnm_agreements.plan_code snapshot on the row's
	// contract context — the agreement carries the group key).
	PlanCode    string `json:"plan_code,omitempty"`
	AmountMinor int64  `json:"amount_minor"`
	// IsClosed marks a closing row: no cash moves (amount 0), the contract
	// is CLOSED after settle. Only valid on the COMPLETE batch.
	IsClosed bool `json:"is_closed,omitempty"`
}

// CreateBatchInput is the batch register/complete request body.
type CreateBatchInput struct {
	OrgCode       string            `json:"org_code,omitempty"`
	TxnDate       string            `json:"txn_date"`
	PaymentMethod string            `json:"payment_method,omitempty"`
	AccountCode   string            `json:"account_code,omitempty"`
	Description   string            `json:"description,omitempty"`
	Trader        map[string]string `json:"trader,omitempty"`
	Rows          []BatchRowInput   `json:"rows"`
}

// BatchDisbursementService runs the batch (1 hồ sơ — N hợp đồng) two-phase
// drawdown: one batch dossier per case, its rows are the per-agreement
// lnm_disbursements legs settled by the same side effects as the single-row
// path. Posting rides the finance two-phase lifecycle worker-side (1
// PostingRequest, N DEBIT/CREDIT line pairs).
type BatchDisbursementService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
}

func NewBatchDisbursementService(repo *repository.LoanRepository, workflow AdjustmentSubmitter) *BatchDisbursementService {
	return &BatchDisbursementService{repo: repo, workflow: workflow}
}

// batchRowVars is the camelCase row shape stamped into the case variables
// (mirrors the batch input the FE submitted).
type batchRowVars struct {
	ContractCode  string `json:"contractCode"`
	AgreementCode string `json:"agreementCode"`
	PlanCode      string `json:"planCode,omitempty"`
	AmountMinor   int64  `json:"amountMinor"`
	IsClosed      bool   `json:"isClosed,omitempty"`
	RowID         string `json:"rowId,omitempty"`
}
