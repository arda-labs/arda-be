package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// InternalReportingHandler serves the /internal/reporting/* surface consumed
// by statistical-service's reporting ETL (signed caller, tenant re-checked).
type InternalReportingHandler struct {
	source reportingCapitalSource
}

type reportingCapitalSource interface {
	ListContractsForReporting(ctx context.Context, tenantID, orgCode string) ([]repository.ReportingCapitalContract, error)
	ListMovementsForReporting(ctx context.Context, tenantID string) ([]repository.ReportingCapitalMovement, error)
}

func NewInternalReportingHandler(source reportingCapitalSource) *InternalReportingHandler {
	return &InternalReportingHandler{source: source}
}

type reportingContractResponse struct {
	AsOf  string                                `json:"as_of"`
	Items []repository.ReportingCapitalContract `json:"items"`
}

type reportingMovementResponse struct {
	AsOf  string                                `json:"as_of"`
	Items []repository.ReportingCapitalMovement `json:"items"`
}

// InternalReportingContracts serves GET /internal/reporting/capital-contracts.
func (h *InternalReportingHandler) InternalReportingContracts(w http.ResponseWriter, r *http.Request) {
	tenantID, asOf, ok := reportingRequest(w, r)
	if !ok {
		return
	}
	orgCode := strings.TrimSpace(r.URL.Query().Get("org_code"))
	items, err := h.source.ListContractsForReporting(r.Context(), tenantID, orgCode)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, reportingContractResponse{AsOf: asOf, Items: items})
}

// InternalReportingMovements serves GET /internal/reporting/capital-movements.
func (h *InternalReportingHandler) InternalReportingMovements(w http.ResponseWriter, r *http.Request) {
	tenantID, asOf, ok := reportingRequest(w, r)
	if !ok {
		return
	}
	items, err := h.source.ListMovementsForReporting(r.Context(), tenantID)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, reportingMovementResponse{AsOf: asOf, Items: items})
}

func reportingRequest(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if r.Method != http.MethodGet {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return "", "", false
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "verified tenant scope is required"))
		return "", "", false
	}
	return tenantID, strings.TrimSpace(r.URL.Query().Get("as_of")), true
}
