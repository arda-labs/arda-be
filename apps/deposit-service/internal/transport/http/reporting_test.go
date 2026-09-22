package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/deposit-service/internal/handler"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const reportingTestSecret = "01234567890123456789012345678901"

type stubReportingSavingsSource struct {
	items      []repository.Savings
	seenTenant string
	seenOrg    string
}

func (s *stubReportingSavingsSource) ListSavingsForReporting(_ context.Context, tenantID, orgCode string) ([]repository.Savings, error) {
	if tenantID == "" {
		return nil, context.Canceled
	}
	s.seenTenant = tenantID
	s.seenOrg = orgCode
	return s.items, nil
}

func reportingTestRouter() http.Handler {
	source := &stubReportingSavingsSource{items: []repository.Savings{{
		SavingsCode:    "SAV-001",
		CustomerCode:   "CIF-9001",
		ProductCode:    "DPM-12M",
		OrgCode:        "ORG-01",
		OpenDate:       "2026-01-15",
		MaturityDate:   "2027-01-15",
		PrincipalMinor: 100000000,
		AccruedMinor:   2500000,
		CurrencyCode:   "VND",
		Status:         "ACTIVE",
	}}}
	return NewRouter(nil, nil, handler.NewInternalReportingHandler(source))
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
// boundary: statistical-service only; an ai-service token is rejected.
func TestInternalReporting_RequiresStatisticalServiceSource(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", reportingTestSecret)
	router := reportingTestRouter()

	statistical, err := identity.Issue(reportingTestSecret, "statistical-service", "deposit-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue statistical token: %v", err)
	}
	aiToken, err := identity.Issue(reportingTestSecret, "ai-service", "deposit-service", time.Now(), time.Minute)
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
		{name: "statistical token without tenant", token: statistical, status: http.StatusForbidden},
		{name: "statistical signed request", token: statistical, tenant: "tenant-1", status: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := reportingSignedRequest(t, router, "/internal/reporting/deposit-savings", tt.tenant, tt.token)
			if res.Code != tt.status {
				t.Errorf("status = %d, want %d (body %s)", res.Code, tt.status, res.Body.String())
			}
		})
	}
}

// TestInternalReporting_ReturnsOrgAndMeasurements proves the reporting surface
// keeps org_code and the measurement columns (the AI surface drops org_code).
func TestInternalReporting_ReturnsOrgAndMeasurements(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", reportingTestSecret)
	router := reportingTestRouter()

	token, err := identity.Issue(reportingTestSecret, "statistical-service", "deposit-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue statistical token: %v", err)
	}
	res := reportingSignedRequest(t, router, "/internal/reporting/deposit-savings?org_code=ORG-01&as_of=2026-09-22", "tenant-1", token)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var envelope struct {
		Result struct {
			AsOf  string `json:"as_of"`
			Items []struct {
				SavingsCode    string `json:"savings_code"`
				OrgCode        string `json:"org_code"`
				PrincipalMinor int64  `json:"principal_minor"`
			} `json:"items"`
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
	if item.OrgCode != "ORG-01" || item.PrincipalMinor != 100000000 {
		t.Fatalf("reporting projection incomplete: %+v", item)
	}
	if envelope.Result.AsOf != "2026-09-22" {
		t.Errorf("as_of not echoed: %q", envelope.Result.AsOf)
	}
	if strings.Contains(res.Body.String(), "workflow_case_id") || strings.Contains(res.Body.String(), "journal_entry_id") {
		t.Errorf("reporting body must not leak linkage ids: %s", res.Body.String())
	}
}
