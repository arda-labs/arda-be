package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/handler"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

// stubReportingLoanSource stands in for LoanService behind the reporting
// handler so the signed-request tests run without a database.
type stubReportingLoanSource struct {
	items      []domain.ReportingAgreement
	seenTenant string
	seenOrg    string
}

func (s *stubReportingLoanSource) ListAgreementsForReporting(_ context.Context, tenantID, orgCode string) ([]domain.ReportingAgreement, error) {
	if tenantID == "" {
		return nil, errMissingAITenant
	}
	s.seenTenant = tenantID
	s.seenOrg = orgCode
	return s.items, nil
}

func (s *stubReportingLoanSource) ListCollateralsForReporting(_ context.Context, tenantID string) ([]domain.ReportingCollateral, error) {
	if tenantID == "" {
		return nil, errMissingAITenant
	}
	s.seenTenant = tenantID
	return nil, nil
}

func reportingTestRouter() http.Handler {
	source := &stubReportingLoanSource{items: []domain.ReportingAgreement{{
		AgreementCode:       "AG-001",
		ContractCode:        "LN-2026-001",
		CustomerCode:        "CIF-9001",
		ProductCode:         "LOAN-SME-01",
		OrgCode:             "ORG-01",
		DisburseDate:        "2026-01-15",
		MaturityDate:        "2027-01-15",
		DebtGroupCode:       "GROUP_1",
		Status:              "ACTIVE",
		CurrencyCode:        "VND",
		DisburseAmtMinor:    500000000,
		OutstandingAmtMinor: 420000000,
		ProvisionAmtMinor:   8400000,
	}}}
	return NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, handler.NewInternalReportingHandler(source), nil)
}

func reportingSignedRequest(t *testing.T, router http.Handler, path, tenantID, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("X-Service-Auth", token)
	}
	if tenantID != "" {
		req.Header.Set("X-Tenant-Id", tenantID)
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

// TestInternalReporting_RequiresStatisticalServiceSource locks the caller
// boundary: the reporting ETL is statistical-service only — an ai-service
// token (the chat surface) must not read the reporting projection.
func TestInternalReporting_RequiresStatisticalServiceSource(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := reportingTestRouter()

	statistical, err := identity.Issue(aiTestSecret, "statistical-service", "loan-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue statistical token: %v", err)
	}
	aiToken, err := identity.Issue(aiTestSecret, "ai-service", "loan-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue ai token: %v", err)
	}

	tests := []struct {
		name   string
		token  string
		tenant string
		status int
	}{
		{name: "missing token", status: http.StatusUnauthorized},
		{name: "ai-service token rejected", token: aiToken, tenant: "tenant-1", status: http.StatusForbidden},
		{name: "statistical token without tenant", token: statistical, status: http.StatusBadRequest},
		{name: "statistical signed request", token: statistical, tenant: "tenant-1", status: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := reportingSignedRequest(t, router, "/internal/reporting/loan-agreements", tt.tenant, tt.token)
			if res.Code != tt.status {
				t.Errorf("status = %d, want %d (body %s)", res.Code, tt.status, res.Body.String())
			}
		})
	}
}

// TestInternalReporting_ReturnsFullProjection proves the reporting surface is
// NOT redacted: org_code and the measurement columns must survive (unlike the
// AI surface, which drops them).
func TestInternalReporting_ReturnsFullProjection(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := reportingTestRouter()

	token, err := identity.Issue(aiTestSecret, "statistical-service", "loan-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue statistical token: %v", err)
	}
	res := reportingSignedRequest(t, router, "/internal/reporting/loan-agreements?org_code=ORG-01&as_of=2026-09-22", "tenant-1", token)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var envelope struct {
		Result struct {
			AsOf  string                      `json:"as_of"`
			Items []domain.ReportingAgreement `json:"items"`
		} `json:"result"`
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !envelope.Success || len(envelope.Result.Items) != 1 {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	item := envelope.Result.Items[0]
	if item.OrgCode != "ORG-01" || item.OutstandingAmtMinor != 420000000 || item.ProvisionAmtMinor != 8400000 {
		t.Fatalf("reporting projection incomplete: %+v", item)
	}
	if envelope.Result.AsOf != "2026-09-22" {
		t.Errorf("as_of not echoed: %q", envelope.Result.AsOf)
	}
	if !strings.Contains(res.Body.String(), "org_code") {
		t.Errorf("reporting body must carry org_code: %s", res.Body.String())
	}
}
