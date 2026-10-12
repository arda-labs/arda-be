package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// BatchHandler exposes the iteration-13 batch flows (1 hồ sơ — N hợp đồng)
// over HTTP: batch disbursement register/complete, batch collection, and the
// finance posting-rules proxy the maker screen uses to preview rule cards.
type BatchHandler struct {
	disb *service.BatchDisbursementService
	col  *service.BatchCollectionService
	fin  *financeclient.Client
}

func NewBatchHandler(disb *service.BatchDisbursementService, col *service.BatchCollectionService, fin *financeclient.Client) *BatchHandler {
	return &BatchHandler{disb: disb, col: col, fin: fin}
}

// batchListSpec is the ParseListRequest contract for the batch ledgers:
// status filter only (plus flow_type on the disbursement side), no q —
// batches are few and paged by created_at.
var batchListSpec = ardahttp.ListSpec{
	DefaultPerPage: 20,
	MaxPerPage:     ardahttp.MaxPerPage,
	SortFields:     []string{"created_at"},
}

// batchStatuses is the whitelisted status filter set.
var batchStatuses = map[string]bool{
	"DRAFT": true, "PENDING_APPROVAL": true, "APPROVED": true,
	"SUBMIT_FAILED": true, "REJECTED": true, "CANCELLED": true, "POSTED": true,
}

func batchStatusFilter(w http.ResponseWriter, raw string) (string, bool) {
	status := strings.ToUpper(strings.TrimSpace(raw))
	if status != "" && !batchStatuses[status] {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput,
			"status must be one of: DRAFT, PENDING_APPROVAL, APPROVED, REJECTED, CANCELLED, POSTED")
		return "", false
	}
	return status, true
}

// ListDisbursementBatches handles GET /api/loan/disbursement-batches.
// Optional filters: status, flow_type (REGISTER|COMPLETE).
func (h *BatchHandler) ListDisbursementBatches(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), batchListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	status, ok := batchStatusFilter(w, r.URL.Query().Get("status"))
	if !ok {
		return
	}
	flowType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("flow_type")))
	switch flowType {
	case "", "REGISTER", "COMPLETE":
	default:
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput,
			"flow_type must be one of: REGISTER, COMPLETE")
		return
	}
	items, total, err := h.disb.List(r.Context(), tenantID, status, flowType, listReq.Page, listReq.PerPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(listReq.Page, listReq.PerPage, total, items))
}

// CreateBatchRegister handles POST /api/loan/disbursement-batches.
func (h *BatchHandler) CreateBatchRegister(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req service.CreateBatchInput
	if !decodeBody(w, r, &req) {
		return
	}
	created, err := h.disb.CreateBatchRegister(r.Context(), tenantID, actorOf(r), orgScopeFromRequest(r).ActiveOrg(), &req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, draftCreatedResponse(created))
}

// SubmitDisbursementBatch queues workflow work and returns without waiting for Zeebe.
func (h *BatchHandler) SubmitDisbursementBatch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req BatchVersionRequest
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.disb.Submit(r.Context(), tenantID, actorOf(r), r.PathValue("id"), req.DataVersion)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusAccepted, BatchSubmitAccepted{BatchID: item.ID, ReferenceNo: item.ID, Status: item.Status, DataVersion: item.DataVersion})
}

// UpdateDisbursementDraft updates a DRAFT batch with optimistic concurrency.
func (h *BatchHandler) UpdateDisbursementDraft(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req service.CreateBatchInput
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.disb.UpdateDraft(r.Context(), tenantID, actorOf(r), r.PathValue("id"), &req)
	writeResult(w, r, item, err)
}

// CancelDisbursementDraft cancels a saved draft with an optimistic version guard.
func (h *BatchHandler) CancelDisbursementDraft(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req BatchVersionRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := h.disb.CancelDraft(r.Context(), tenantID, actorOf(r), r.PathValue("id"), req.DataVersion); err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, BatchCancelled{BatchID: r.PathValue("id"), Status: "CANCELLED"})
}

// CreateBatchComplete handles POST /api/loan/disbursement-batches/complete.
// source_batch_id (body) references the POSTED REGISTER batch.
func (h *BatchHandler) CreateBatchComplete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req BatchCompleteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	created, err := h.disb.CreateBatchComplete(r.Context(), tenantID, actorOf(r), orgScopeFromRequest(r).ActiveOrg(), req.SourceBatchID, &req.CreateBatchInput)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, draftCreatedResponse(created))
}

func draftCreatedResponse(batch *domain.DisbursementBatch) BatchDraftCreated {
	return BatchDraftCreated{ID: batch.ID, BatchID: batch.ID, Status: batch.Status, DataVersion: batch.DataVersion}
}

// GetDisbursementBatch handles GET /api/loan/disbursement-batches/{id}
// (header + rows).
func (h *BatchHandler) GetDisbursementBatch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.disb.Get(r.Context(), tenantID, r.PathValue("id"))
	writeResult(w, r, item, err)
}

// PreviewDisbursementBatch resolves the accounting rules and COA for a saved
// REGISTER draft. Finance Validate is read-only; this endpoint never reserves
// or posts a journal entry.
func (h *BatchHandler) PreviewDisbursementBatch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	if h.fin == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, ardaerrors.CodeBadGateway, "finance preview is unavailable")
		return
	}
	batch, err := h.disb.Get(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if batch.FlowType != domain.FlowRegister || (batch.Status != domain.BatchDraft && batch.Status != domain.BatchSubmitted) {
		writeErrorCode(w, http.StatusConflict, ardaerrors.CodeInvalidInput, "posting preview is only available for REGISTER drafts and pending approvals")
		return
	}
	detail, err := h.disb.BatchPostingDetail(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if detail.GetBatchType() != "DISB_REGISTER" {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "posting preview is only available for REGISTER batches")
		return
	}
	if len(detail.GetRows()) == 0 {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, "batch has no rows")
		return
	}
	legs := make([]financeclient.PostingLeg, 0, len(detail.GetRows())*2)
	for _, row := range detail.GetRows() {
		if row.GetAmountMinor() <= 0 {
			writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, fmt.Sprintf("agreement %s must have a positive amount", row.GetAgreementCode()))
			return
		}
		description := fmt.Sprintf("HĐ %s — %s", row.GetContractCode(), row.GetAgreementCode())
		if row.GetPlanCode() != "" {
			description += " — " + row.GetPlanCode()
		}
		legs = append(legs,
			financeclient.PostingLeg{
				CardLine: 1, Direction: "DEBIT",
				AmountMinor: row.GetAmountMinor(), Description: description,
				Analytics: &financev1.Analytics{DebtGroupCode: row.GetDebtGroupCode(), OrgUnitCode: row.GetOrgUnitCode(), CustomerCode: row.GetCustomerCode(), ContractCode: row.GetContractCode(), Dimensions: map[string]string{"agreement_code": row.GetAgreementCode(), "batch_id": detail.GetBatchId()}},
			},
			financeclient.PostingLeg{
				CardLine: 2, Direction: "CREDIT",
				AmountMinor: row.GetAmountMinor(), Description: description,
				Analytics: &financev1.Analytics{OrgUnitCode: row.GetOrgUnitCode(), ContractCode: row.GetContractCode(), Dimensions: map[string]string{"batch_id": detail.GetBatchId()}},
			},
		)
	}
	lines, err := financeclient.BuildPostingLines(r.Context(), h.fin, "LNM_DISB_REGISTER", legs, detail.GetCurrencyCode())
	if err != nil {
		if _, business := financeclient.AsPostingError(err); business {
			writeErrorCode(w, http.StatusUnprocessableEntity, ardaerrors.CodeInvalidInput, err.Error())
			return
		}
		writeServiceError(w, r, err)
		return
	}
	request := &financev1.PostingRequest{
		IdempotencyKey:    "preview-lnm-disb-register-" + detail.GetBatchId(),
		AccountingDate:    detail.GetTxnDate(),
		CurrencyCode:      detail.GetCurrencyCode(),
		Description:       detail.GetDescription(),
		BusinessReference: &financev1.BusinessReference{Domain: "lnm", DocumentType: "LNM_DISB_REGISTER", DocumentId: detail.GetBatchId(), DocumentCode: detail.GetBatchCode()},
		Lines:             lines,
		Metadata:          map[string]string{"org_code": detail.GetOrgUnitCode()},
	}
	result, err := h.fin.Validate(r.Context(), request)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	headroom, err := h.disb.BatchHeadroom(r.Context(), tenantID, batch.ID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, BatchPostingPreview{
		Valid: result.GetValid(), CoaVersionID: result.GetCoaVersionId(), GlobalErrors: result.GetGlobalErrors(), Lines: result.GetLines(), Headroom: headroom,
	})
}

// ListCollectionBatches handles GET /api/loan/collection-batches.
func (h *BatchHandler) ListCollectionBatches(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), batchListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	status, ok := batchStatusFilter(w, r.URL.Query().Get("status"))
	if !ok {
		return
	}
	items, total, err := h.col.List(r.Context(), tenantID, status, listReq.Page, listReq.PerPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(listReq.Page, listReq.PerPage, total, items))
}

// CreateBatchCollection handles POST /api/loan/collection-batches.
func (h *BatchHandler) CreateBatchCollection(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req service.CreateBatchInputCollection
	if !decodeBody(w, r, &req) {
		return
	}
	created, err := h.col.CreateBatchCollection(r.Context(), tenantID, actorOf(r), orgScopeFromRequest(r).ActiveOrg(), &req)
	writeResult(w, r, created, err)
}

// GetCollectionBatch handles GET /api/loan/collection-batches/{id}
// (header + rows).
func (h *BatchHandler) GetCollectionBatch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.col.Get(r.Context(), tenantID, r.PathValue("id"))
	writeResult(w, r, item, err)
}

// postingRuleDocTypes is the whitelist the proxy exposes — the LNM rule
// cards the maker screens preview (accrual/provision included for the EOD
// config screens).
var postingRuleDocTypes = map[string]bool{
	"LNM_DISB_REGISTER": true,
	"LNM_DISB_COMPLETE": true,
	"LNM_COLLECTION":    true,
	"LNM_ACCRUAL":       true,
	"LNM_PROVISION":     true,
}

// ListPostingRules handles GET /api/loan/posting-rules?document_type= —
// a thin proxy over finance ListPostingRules. A missing/unreachable finance
// client degrades to an empty items list (the rule cards are additive:
// flows keep their built-in fallback classifications).
func (h *BatchHandler) ListPostingRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireTenantID(w, r); !ok {
		return
	}
	docType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("document_type")))
	if !postingRuleDocTypes[docType] {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput,
			"document_type must be one of: LNM_DISB_REGISTER, LNM_DISB_COMPLETE, LNM_COLLECTION, LNM_ACCRUAL, LNM_PROVISION")
		return
	}
	items := []PostingRuleView{}
	if h.fin != nil {
		rules, err := h.fin.ListPostingRules(r.Context(), docType)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		for _, rule := range rules {
			items = append(items, PostingRuleView{
				DocumentType:        docType,
				LineNo:              rule.GetLineNo(),
				Direction:           rule.GetDirection(),
				ResolutionType:      rule.GetResolutionType(),
				AccountRef:          rule.GetAccountRef(),
				AccClassification:   rule.GetAccClassification(),
				RequiredDimensions:  rule.GetRequiredDimensions(),
				DescriptionTemplate: rule.GetDescriptionTemplate(),
			})
		}
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, PostingRules{Items: items, DocumentType: docType})
}

// Request and result bodies of the batch endpoints, named so the OpenAPI
// document (contracts/openapi/loan-v1.json) carries the exact wire shape; see
// internal/transport/http/openapi.go.

// BatchVersionRequest carries the optimistic row version for submit and cancel.
type BatchVersionRequest struct {
	DataVersion int64 `json:"data_version"`
}

// BatchCompleteRequest is the POST /api/loan/disbursement-batches/complete body.
type BatchCompleteRequest struct {
	service.CreateBatchInput
	SourceBatchID string `json:"source_batch_id"`
}

// BatchDraftCreated is returned when a disbursement batch draft is created.
type BatchDraftCreated struct {
	ID          string `json:"id"`
	BatchID     string `json:"batch_id"`
	Status      string `json:"status"`
	DataVersion int64  `json:"data_version"`
}

// BatchSubmitAccepted is returned when a batch submission is queued.
type BatchSubmitAccepted struct {
	BatchID     string `json:"batch_id"`
	ReferenceNo string `json:"reference_no"`
	Status      string `json:"status"`
	DataVersion int64  `json:"data_version"`
}

// BatchCancelled is returned when a batch draft is cancelled.
type BatchCancelled struct {
	BatchID string `json:"batch_id"`
	Status  string `json:"status"`
}

// BatchPostingPreview is the read-only finance validation of a REGISTER draft.
type BatchPostingPreview struct {
	Valid        bool                                `json:"valid"`
	CoaVersionID string                              `json:"coa_version_id"`
	GlobalErrors []string                            `json:"global_errors"`
	Lines        []*financev1.ValidationLine         `json:"lines"`
	Headroom     []service.DisbursementBatchHeadroom `json:"headroom"`
}

// PostingRuleView is one finance posting rule line a maker screen previews.
type PostingRuleView struct {
	DocumentType        string   `json:"document_type"`
	LineNo              int32    `json:"line_no"`
	Direction           string   `json:"direction"`
	ResolutionType      string   `json:"resolution_type"`
	AccountRef          string   `json:"account_ref"`
	AccClassification   string   `json:"acc_classification"`
	RequiredDimensions  []string `json:"required_dimensions"`
	DescriptionTemplate string   `json:"description_template"`
}

// PostingRules is the GET /api/loan/posting-rules result.
type PostingRules struct {
	Items        []PostingRuleView `json:"items"`
	DocumentType string            `json:"document_type"`
}
