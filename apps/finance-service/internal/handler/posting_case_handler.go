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

// createPostingCaseBody is the request contract: flow + posting_request in
// the documented snake_case JSON shape (protojson accepts both spellings of
// proto fields, so the FE contract and the proto stay aligned).
type createPostingCaseBody struct {
	Flow           string          `json:"flow"`
	PostingRequest json.RawMessage `json:"posting_request"`
}

// CreatePostingCase handles POST /api/finance/posting-cases: structural +
// COA validation, then the FIN_SINGLE_ENTRY_V2 / FIN_DOUBLE_ENTRY_V2 case is
// created and submitted with the posting riding the two-phase lifecycle.
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

	result, err := h.svc.CreatePostingCase(r.Context(), tenantID, actor, service.PostingCaseInput{
		Flow:           in.Flow,
		PostingRequest: &posting,
	})
	if err != nil {
		writePostingCaseError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{
		"case_id":   result.CaseID,
		"case_code": result.CaseCode,
	})
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
