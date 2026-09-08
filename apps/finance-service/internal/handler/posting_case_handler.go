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

// createPostingCaseBody is the request contract: flow + posting_request /
// cancellation_request in the documented snake_case JSON shape. posting_request
// is raw protojson (protojson accepts both spellings of proto fields, so the
// FE contract and the proto stay aligned); CANCELLATION carries
// cancellation_request instead and ignores posting_request.
type createPostingCaseBody struct {
	Flow                string                  `json:"flow"`
	PostingRequest      json.RawMessage         `json:"posting_request"`
	CancellationRequest *createCancellationBody `json:"cancellation_request"`
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
