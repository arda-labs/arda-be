package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// InternalReportingHandler serves the /internal/reporting/* surface consumed
// by statistical-service's reporting ETL. It is a service-to-service ETL
// source (signed caller assertion, tenant re-checked here), not a chat tool:
// it returns full measurement + dimension columns including org_code.
type InternalReportingHandler struct {
	loans reportingLoanSource
}

// reportingLoanSource is the slice of LoanService the reporting surface needs.
type reportingLoanSource interface {
	ListAgreementsForReporting(ctx context.Context, tenantID, orgCode string) ([]domain.ReportingAgreement, error)
	ListCollateralsForReporting(ctx context.Context, tenantID string) ([]domain.ReportingCollateral, error)
}

func NewInternalReportingHandler(loans reportingLoanSource) *InternalReportingHandler {
	return &InternalReportingHandler{loans: loans}
}

type reportingAgreementResponse struct {
	AsOf  string                      `json:"as_of"`
	Items []domain.ReportingAgreement `json:"items"`
}

type reportingCollateralResponse struct {
	AsOf  string                       `json:"as_of"`
	Items []domain.ReportingCollateral `json:"items"`
}

// InternalReportingAgreements serves GET /internal/reporting/loan-agreements
// for statistical-service. Tenant comes from X-Tenant-Id; org_code is an
// optional narrowing filter. as_of is echoed back (the current projection is a
// point-in-time snapshot the caller stamps with its own business_date).
func (h *InternalReportingHandler) InternalReportingAgreements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorCode(w, http.StatusMethodNotAllowed, ardaerrors.CodeMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	orgCode := strings.TrimSpace(r.URL.Query().Get("org_code"))
	asOf := strings.TrimSpace(r.URL.Query().Get("as_of"))
	items, err := h.loans.ListAgreementsForReporting(r.Context(), tenantID, orgCode)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, reportingAgreementResponse{AsOf: asOf, Items: items})
}

// InternalReportingCollaterals serves GET /internal/reporting/loan-collaterals
// for statistical-service.
func (h *InternalReportingHandler) InternalReportingCollaterals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorCode(w, http.StatusMethodNotAllowed, ardaerrors.CodeMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	asOf := strings.TrimSpace(r.URL.Query().Get("as_of"))
	items, err := h.loans.ListCollateralsForReporting(r.Context(), tenantID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, reportingCollateralResponse{AsOf: asOf, Items: items})
}
