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
	var req struct {
		DataVersion int64 `json:"data_version"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.disb.Submit(r.Context(), tenantID, actorOf(r), r.PathValue("id"), req.DataVersion)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusAccepted, map[string]any{"batch_id": item.ID, "reference_no": item.ID, "status": item.Status, "data_version": item.DataVersion})
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
	var req struct {
		DataVersion int64 `json:"data_version"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if err := h.disb.CancelDraft(r.Context(), tenantID, actorOf(r), r.PathValue("id"), req.DataVersion); err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"batch_id": r.PathValue("id"), "status": "CANCELLED"})
}

// CreateBatchComplete handles POST /api/loan/disbursement-batches/complete.
// source_batch_id (body) references the POSTED REGISTER batch.
func (h *BatchHandler) CreateBatchComplete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req struct {
		service.CreateBatchInput
		SourceBatchID string `json:"source_batch_id"`
	}
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

func draftCreatedResponse(batch *domain.DisbursementBatch) map[string]any {
	return map[string]any{"id": batch.ID, "batch_id": batch.ID, "status": batch.Status, "data_version": batch.DataVersion}
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
	rules, err := h.fin.ListPostingRules(r.Context(), "LNM_DISB_REGISTER")
	if err != nil {
		writeServiceError(w, r, err)
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
				CardLine: 1, Fallback: "LNM_LOAN_PRINCIPAL", Direction: "DEBIT",
				AmountMinor: row.GetAmountMinor(), Description: description,
				Analytics: &financev1.Analytics{DebtGroupCode: row.GetDebtGroupCode(), OrgUnitCode: row.GetOrgUnitCode(), CustomerCode: row.GetCustomerCode(), ContractCode: row.GetContractCode(), Dimensions: map[string]string{"agreement_code": row.GetAgreementCode(), "batch_id": detail.GetBatchId()}},
			},
			financeclient.PostingLeg{
				CardLine: 2, Fallback: "FUND_DISBURSEMENT_IN_TRANSIT", Direction: "CREDIT",
				AmountMinor: row.GetAmountMinor(), Description: description,
				Analytics: &financev1.Analytics{OrgUnitCode: row.GetOrgUnitCode(), ContractCode: row.GetContractCode(), Dimensions: map[string]string{"batch_id": detail.GetBatchId()}},
			},
		)
	}
	request := &financev1.PostingRequest{
		IdempotencyKey:    "preview-lnm-disb-register-" + detail.GetBatchId(),
		AccountingDate:    detail.GetTxnDate(),
		CurrencyCode:      detail.GetCurrencyCode(),
		Description:       detail.GetDescription(),
		BusinessReference: &financev1.BusinessReference{Domain: "lnm", DocumentType: "LNM_DISB_REGISTER", DocumentId: detail.GetBatchId(), DocumentCode: detail.GetBatchCode()},
		Lines:             financeclient.PostingLinesFromRules(rules, legs, detail.GetCurrencyCode()),
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
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"valid": result.GetValid(), "coa_version_id": result.GetCoaVersionId(), "global_errors": result.GetGlobalErrors(), "lines": result.GetLines(), "headroom": headroom,
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
	items := []map[string]any{}
	if h.fin != nil {
		rules, err := h.fin.ListPostingRules(r.Context(), docType)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		for _, rule := range rules {
			items = append(items, map[string]any{
				"document_type":        docType,
				"line_no":              rule.GetLineNo(),
				"direction":            rule.GetDirection(),
				"resolution_type":      rule.GetResolutionType(),
				"account_ref":          rule.GetAccountRef(),
				"acc_classification":   rule.GetAccClassification(),
				"required_dimensions":  rule.GetRequiredDimensions(),
				"description_template": rule.GetDescriptionTemplate(),
			})
		}
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"items": items, "document_type": docType})
}
