package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// PostingCaseHandler exposes POST /api/finance/posting-cases — the manual
// posting entry point (FAC-native bút toán lẻ / bút toán kép). The handler
// owns the wire contract; orchestration lives in PostingCaseService.
type PostingCaseHandler struct {
	svc *service.PostingCaseService
}

func NewPostingCaseHandler(svc *service.PostingCaseService) *PostingCaseHandler {
	return &PostingCaseHandler{svc: svc}
}

// createCancellationTraderBody mirrors the trader block the FE sends for
// manual postings (the person raising the cancellation).
type createCancellationTraderBody struct {
	ObjectType string `json:"object_type"`
	ObjectCode string `json:"object_code"`
	ObjectName string `json:"object_name"`
	IDNumber   string `json:"id_number"`
	IssueDate  string `json:"issue_date"`
	IssuePlace string `json:"issue_place"`
	Address    string `json:"address"`
}

// createCancellationBody is the CANCELLATION flow request: the POSTED entry
// to reverse (by human entry_no) + reason; accounting_date is the optional
// reversal business date; idempotency_key is FE-pinned or server-generated.
type createCancellationBody struct {
	ReferenceEntryNo string                        `json:"reference_entry_no"`
	Reason           string                        `json:"reason"`
	AccountingDate   string                        `json:"accounting_date"`
	Trader           *createCancellationTraderBody `json:"trader"`
	IdempotencyKey   string                        `json:"idempotency_key"`
}

// createClosingRowBody is one maker-picked closing line: the INC/EXP
// account and the amount to close (the FE prefills it from the candidate
// balance of GET /api/finance/closing/accounts).
type createClosingRowBody struct {
	AccCode     string `json:"acc_code"`
	AccPurpose  string `json:"acc_purpose"`
	AmountMinor int64  `json:"amount_minor"`
}

// createClosingBody is the CLOSING flow request (iteration 11 — kết chuyển
// thu chi): the period closing date + granularity, the optional trader
// block and the INC/EXP rows. finance-service builds the balanced posting
// lines server-side (dest 4211 from the FIN_CLOSING_*_DEST rules).
type createClosingBody struct {
	AccountingDate string                        `json:"accounting_date"`
	PeriodType     string                        `json:"period_type"`
	Description    string                        `json:"description"`
	Trader         *createCancellationTraderBody `json:"trader"`
	IdempotencyKey string                        `json:"idempotency_key"`
	Rows           []createClosingRowBody        `json:"rows"`
}

// createFundBody is the FUND flow request (trích lập/sử dụng quỹ): the fund
// code, action and amount; finance-service builds the lines from the FUND_*
// class maps and rides the standard two-phase lifecycle.
type createFundBody struct {
	AccountingDate string                        `json:"accounting_date"`
	Action         string                        `json:"action"`
	FundCode       string                        `json:"fund_code"`
	AmountMinor    int64                         `json:"amount_minor"`
	Description    string                        `json:"description"`
	Trader         *createCancellationTraderBody `json:"trader"`
	IdempotencyKey string                        `json:"idempotency_key"`
}

// createPostingCaseBody is the request contract: flow + posting_request /
// cancellation_request / closing_request in the documented snake_case JSON
// shape. posting_request is raw protojson (protojson accepts both spellings
// of proto fields, so the FE contract and the proto stay aligned);
// CANCELLATION carries cancellation_request instead and ignores
// posting_request; CLOSING carries closing_request.
type createPostingCaseBody struct {
	Flow                string                  `json:"flow"`
	PostingRequest      json.RawMessage         `json:"posting_request"`
	CancellationRequest *createCancellationBody `json:"cancellation_request"`
	ClosingRequest      *createClosingBody      `json:"closing_request"`
	FundRequest         *createFundBody         `json:"fund_request"`
}

// CreatePostingCase handles POST /api/finance/posting-cases: structural +
// COA validation, then the FIN_SINGLE_ENTRY_V2 / FIN_DOUBLE_ENTRY_V2 /
// FIN_OFF_BALANCE_V2 case is created and submitted with the posting riding
// the two-phase lifecycle; FIN_TXN_CANCEL_V2 (CANCELLATION) carries no
// posting and reverses the referenced POSTED entry on approval.
func (h *PostingCaseHandler) CreatePostingCase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	actor := r.Header.Get("X-User-Id")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	var in createPostingCaseBody
	if err := json.Unmarshal(body, &in); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	var posting financev1.PostingRequest
	if len(in.PostingRequest) > 0 {
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(in.PostingRequest, &posting); err != nil {
			respondError(w, r, http.StatusBadRequest, "invalid posting_request")
			return
		}
	}

	input := service.PostingCaseInput{Flow: in.Flow, PostingRequest: &posting}
	if in.Flow == service.FlowCancellation {
		input.Cancellation = mapCancellationInput(in.CancellationRequest)
	}
	if in.Flow == service.FlowClosing {
		input.Closing = mapClosingInput(in.ClosingRequest)
	}
	if in.Flow == service.FlowFund {
		input.Fund = mapFundInput(in.FundRequest)
	}

	result, err := h.svc.CreatePostingCase(r.Context(), tenantID, actor, input)
	if err != nil {
		writePostingCaseError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{
		"case_id":   result.CaseID,
		"case_code": result.CaseCode,
	})
}

// mapClosingInput translates the CLOSING wire body onto the service input.
func mapClosingInput(body *createClosingBody) *service.ClosingCaseInput {
	if body == nil {
		return nil
	}
	in := &service.ClosingCaseInput{
		AccountingDate: body.AccountingDate,
		PeriodType:     body.PeriodType,
		Description:    body.Description,
		IdempotencyKey: body.IdempotencyKey,
	}
	if body.Trader != nil {
		in.Trader = &service.CancellationTrader{
			ObjectType: body.Trader.ObjectType,
			ObjectCode: body.Trader.ObjectCode,
			ObjectName: body.Trader.ObjectName,
			IDNumber:   body.Trader.IDNumber,
			IssueDate:  body.Trader.IssueDate,
			IssuePlace: body.Trader.IssuePlace,
			Address:    body.Trader.Address,
		}
	}
	for _, row := range body.Rows {
		in.Rows = append(in.Rows, service.ClosingCaseRow{
			AccCode:     row.AccCode,
			AccPurpose:  row.AccPurpose,
			AmountMinor: row.AmountMinor,
		})
	}
	return in
}

// ListClosingAccounts handles GET /api/finance/closing/accounts?accounting_date=
// — the closing candidate picker: INC/EXP accounts with a positive natural
// balance as of the date, each item prefilled with its closing amount.
// ImportPostingCases handles POST /api/finance/posting-cases/import — an
// XLSX posting sheet (multipart/form-data, field "file") is parsed into a
// manual posting case. form fields: flow (default SINGLE_ENTRY),
// accounting_date (fallback when the sheet has no accounting_date column).
func (h *PostingCaseHandler) ImportPostingCases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	actor := r.Header.Get("X-User-Id")

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	result, err := h.svc.ImportPostingSheet(r.Context(), tenantID, actor,
		r.FormValue("flow"), r.FormValue("accounting_date"), file)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{
		"case_id":         result.CaseID,
		"case_code":       result.CaseCode,
		"line_count":      result.LineCount,
		"accounting_date": result.AccountingDate,
	})
}

func (h *PostingCaseHandler) ListClosingAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListClosingCandidates(r.Context(), tenantID, r.URL.Query().Get("accounting_date"))
	if err != nil {
		writePostingCaseError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// mapFundInput maps the FUND flow body to the service shape.
func mapFundInput(body *createFundBody) *service.FundCaseInput {
	if body == nil {
		return nil
	}
	in := &service.FundCaseInput{
		AccountingDate: body.AccountingDate,
		Action:         body.Action,
		FundCode:       body.FundCode,
		AmountMinor:    body.AmountMinor,
		Description:    body.Description,
		IdempotencyKey: body.IdempotencyKey,
	}
	if body.Trader != nil {
		in.Trader = &service.CancellationTrader{
			ObjectType: body.Trader.ObjectType,
			ObjectCode: body.Trader.ObjectCode,
			ObjectName: body.Trader.ObjectName,
			IDNumber:   body.Trader.IDNumber,
			IssueDate:  body.Trader.IssueDate,
			IssuePlace: body.Trader.IssuePlace,
			Address:    body.Trader.Address,
		}
	}
	return in
}

// mapCancellationInput translates the wire body onto the service input.
func mapCancellationInput(body *createCancellationBody) *service.CancellationCaseInput {
	if body == nil {
		return nil
	}
	in := &service.CancellationCaseInput{
		ReferenceEntryNo: body.ReferenceEntryNo,
		Reason:           body.Reason,
		AccountingDate:   body.AccountingDate,
		IdempotencyKey:   body.IdempotencyKey,
	}
	if body.Trader != nil {
		in.Trader = &service.CancellationTrader{
			ObjectType: body.Trader.ObjectType,
			ObjectCode: body.Trader.ObjectCode,
			ObjectName: body.Trader.ObjectName,
			IDNumber:   body.Trader.IDNumber,
			IssueDate:  body.Trader.IssueDate,
			IssuePlace: body.Trader.IssuePlace,
			Address:    body.Trader.Address,
		}
	}
	return in
}

// writePostingCaseError keeps the canonical problem envelope: typed service
// errors (invalid input / bad gateway / internal) map through the shared
// resolver, anything else is a 400 with the raw message.
func writePostingCaseError(w http.ResponseWriter, r *http.Request, err error) {
	var typed *ardaerrors.Error
	if errors.As(err, &typed) {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	respondError(w, r, http.StatusBadRequest, err.Error())
}
