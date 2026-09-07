package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// PostingHandler exposes the HTTP read/preview surface of the posting stack:
// journal listing, posting validation (contract §8.3 preview) and opening
// balances. Writes go through gRPC PostingService; HTTP exists for the
// finance remote and BFF.
type PostingHandler struct {
	svc *service.PostingService
}

func NewPostingHandler(svc *service.PostingService) *PostingHandler {
	return &PostingHandler{svc: svc}
}

// ValidatePosting handles POST /api/finance/posting/validate — the FE
// posting preview: resolved account code/name per line + errors. The body is
// the proto JSON shape of PostingRequest.
func (h *PostingHandler) ValidatePosting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	var req financev1.PostingRequest
	if err := protojson.Unmarshal(body, &req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	result, err := h.svc.ValidatePosting(r.Context(), tenantID, &req)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, json.RawMessage(protojson.Format(result)))
}

// journalListSpec is the public list contract for GET /api/finance/
// journal-entries: q + page/per_page + a sort whitelist kept in sync with the
// FE list definition (entry_no | accounting_date).
var journalListSpec = ardahttp.ListSpec{
	DefaultPerPage: 50,
	MaxPerPage:     200,
	SortFields:     []string{"entry_no", "accounting_date"},
}

// ListJournalEntries handles GET /api/finance/journal-entries. Adds the
// standard list contract (q ILIKE document type / document code /
// description, whitelisted sort, page/per_page) on top of the legacy
// `limit` param, which stays honored when no paging params are given.
func (h *PostingHandler) ListJournalEntries(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), journalListSpec)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	perPage := listReq.PerPage
	if r.URL.Query().Get("per_page") == "" && r.URL.Query().Get("page") == "" {
		if legacy, convErr := strconv.Atoi(r.URL.Query().Get("limit")); convErr == nil && legacy > 0 {
			perPage = min(legacy, 200)
		}
	}
	entries, total, err := h.svc.ListJournalPaged(r.Context(), tenantID, service.JournalListFilter{
		FromDate:     r.URL.Query().Get("from"),
		ToDate:       r.URL.Query().Get("to"),
		DocumentType: r.URL.Query().Get("document_type"),
		Search:       listReq.Q,
		Sort:         listReq.Sort,
		Order:        listReq.Order,
		Page:         listReq.Page,
		PerPage:      perPage,
	})
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, ardahttp.NewListResponse(listReq.Page, perPage, total, entries))
}

// UpsertOpeningBalance handles POST /api/finance/opening-balances (P1a.5).
func (h *PostingHandler) UpsertOpeningBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	userID := r.Header.Get("X-User-Id")
	var req struct {
		AccountingDate string `json:"accounting_date"`
		CoaVersion     string `json:"coa_version"`
		AccountCode    string `json:"account_code"`
		CurrencyCode   string `json:"currency_code"`
		Direction      string `json:"direction"`
		AmountMinor    int64  `json:"amount_minor"`
		Description    string `json:"description"`
		SourceKey      string `json:"source_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	_, err := h.svc.UpsertOpeningBalance(r.Context(), tenantID, service.OpeningBalanceInput{
		AccountingDate: req.AccountingDate,
		CoaVersion:     req.CoaVersion,
		AccountCode:    req.AccountCode,
		CurrencyCode:   req.CurrencyCode,
		Direction:      req.Direction,
		AmountMinor:    req.AmountMinor,
		Description:    req.Description,
		SourceKey:      req.SourceKey,
		Actor:          userID,
	})
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"saved": true})
}

// ListOpeningBalances handles GET /api/finance/opening-balances.
func (h *PostingHandler) ListOpeningBalances(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	onDate := r.URL.Query().Get("as_of")
	if onDate == "" {
		onDate = time.Now().Format("2006-01-02")
	}
	items, err := h.svc.ListOpeningBalances(r.Context(), tenantID, onDate)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondList(w, r, items, nil)
}
