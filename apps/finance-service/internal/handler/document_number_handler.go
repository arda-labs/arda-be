package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	"github.com/arda-labs/arda/libs/go/arda-docno"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

type DocumentNumberHandler struct {
	svc *service.DocumentNumberService
}

func NewDocumentNumberHandler(svc *service.DocumentNumberService) *DocumentNumberHandler {
	return &DocumentNumberHandler{svc: svc}
}

type renumberRequestBody struct {
	DocumentID string `json:"document_id,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	DisplayNo  string `json:"display_no"`
	Reason     string `json:"reason"`
}

func (h *DocumentNumberHandler) RequestRenumber(w http.ResponseWriter, r *http.Request) {
	tenantID, actor := strings.TrimSpace(r.Header.Get("X-Tenant-Id")), strings.TrimSpace(r.Header.Get("X-User-Id"))
	var in renumberRequestBody
	if tenantID == "" || actor == "" || json.NewDecoder(r.Body).Decode(&in) != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, "tenant, actor, and valid request body are required"))
		return
	}
	created, err := h.svc.RequestRenumber(r.Context(), tenantID, in.DocumentID, actor, in.DisplayNo, in.Reason)
	if err != nil {
		writeDocumentNumberError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

func (h *DocumentNumberHandler) ApproveRenumber(allowClosed bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, actor := strings.TrimSpace(r.Header.Get("X-Tenant-Id")), strings.TrimSpace(r.Header.Get("X-User-Id"))
		if tenantID == "" || actor == "" {
			ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, "tenant and actor are required"))
			return
		}
		var in renumberRequestBody
		if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.RequestID) == "" {
			ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "request_id is required"))
			return
		}
		approved, err := h.svc.ApproveRenumber(r.Context(), tenantID, in.RequestID, actor, allowClosed)
		if err != nil {
			writeDocumentNumberError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusOK, approved)
	}
}

func writeDocumentNumberError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusInternalServerError, ardaerrors.CodeInternal
	switch {
	case errors.Is(err, service.ErrDocumentNumberNotFound), errors.Is(err, service.ErrRenumberRequestNotFound):
		status, code = http.StatusNotFound, ardaerrors.CodeNotFound
	case errors.Is(err, service.ErrRenumberMakerChecker), errors.Is(err, service.ErrRenumberClosedPeriod):
		status, code = http.StatusForbidden, ardaerrors.CodeForbidden
	case errors.Is(err, docno.ErrDocumentNumberExists):
		status, code = http.StatusConflict, ardaerrors.CodeConflict
	case strings.Contains(err.Error(), "duplicate key"), strings.Contains(err.Error(), "unique constraint"):
		status, code = http.StatusConflict, ardaerrors.CodeConflict
	case strings.Contains(err.Error(), "required"), strings.Contains(err.Error(), "unchanged"), strings.Contains(err.Error(), "pending"), strings.Contains(err.Error(), "changed after request"):
		status, code = http.StatusBadRequest, ardaerrors.CodeInvalidInput
	}
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, err.Error()))
}
