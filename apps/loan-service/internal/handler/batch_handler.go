package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
)

// BatchHandler exposes the iteration-13 batch flows (1 hồ sơ — N hợp đồng)
// over HTTP: batch disbursement register/complete, batch collection, and the
// finance posting-rules proxy the maker screen uses to preview rule cards.
type BatchHandler struct {
	disb  *service.BatchDisbursementService
	col   *service.BatchCollectionService
	fin   *financeclient.Client
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
	"DRAFT": true, "SUBMITTED": true, "APPROVED": true,
	"REJECTED": true, "CANCELLED": true, "POSTED": true,
}

func batchStatusFilter(w http.ResponseWriter, raw string) (string, bool) {
	status := strings.ToUpper(strings.TrimSpace(raw))
	if status != "" && !batchStatuses[status] {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput,
			"status must be one of: DRAFT, SUBMITTED, APPROVED, REJECTED, CANCELLED, POSTED")
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
	writeResult(w, r, created, err)
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
	writeResult(w, r, created, err)
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
				"document_type":       docType,
				"line_no":             rule.GetLineNo(),
				"direction":           rule.GetDirection(),
				"resolution_type":     rule.GetResolutionType(),
				"account_ref":         rule.GetAccountRef(),
				"acc_classification":  rule.GetAccClassification(),
				"required_dimensions": rule.GetRequiredDimensions(),
				"description_template": rule.GetDescriptionTemplate(),
			})
		}
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"items": items, "document_type": docType})
}
