package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/deposit-service/internal/handler"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const aiTestSecret = "01234567890123456789012345678901"

// errMissingAITenant is a stub-level guard: if the handler ever forgets the
// tenant re-check the stub fails the request instead of serving data.
var errMissingAITenant = errors.New("stub: missing tenant scope")

// stubAISavingsSource stands in for the settlement + interest services behind
// the InternalAIHandler so the signed-request tests run without a database.
type stubAISavingsSource struct {
	savings    []repository.Savings
	detail     *service.SavingsDetail
	seenTenant string
	seenCode   string
}

func (s *stubAISavingsSource) ListSavings(_ context.Context, tenantID string, orgCodes []string, status, q string) ([]repository.Savings, error) {
	if tenantID == "" {
		return nil, errMissingAITenant
	}
	s.seenTenant = tenantID
	return s.savings, nil
}

func (s *stubAISavingsSource) GetSavingsDetail(_ context.Context, tenantID, code string) (*service.SavingsDetail, error) {
	if tenantID == "" {
		return nil, errMissingAITenant
	}
	if s.detail == nil || s.detail.Savings == nil || s.detail.Savings.SavingsCode != code {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "savings not found")
	}
	s.seenTenant = tenantID
	s.seenCode = code
	return s.detail, nil
}

type stubAIRateSource struct {
	rates []repository.InterestRate
}

func (s *stubAIRateSource) ListInterestRates(_ context.Context, tenantID, productCode string) ([]repository.InterestRate, error) {
	if tenantID == "" {
		return nil, errMissingAITenant
	}
	return s.rates, nil
}

// aiTestSavings carries every field the AI surface must never forward:
// tenant_id, internal row id, org/workflow/journal linkage and audit fields.
func aiTestSavings() repository.Savings {
	caseID := "case-secret"
	entryID := "journal-secret"
	return repository.Savings{
		ID:             "sav_secret",
		TenantID:       "tenant-1",
		SavingsCode:    "SAV-2026-001",
		CustomerCode:   "CIF-9001",
		ProductCode:    "DPM-12M",
		OpenDate:       "2026-01-15",
		MaturityDate:   "2027-01-15",
		PrincipalMinor: 100000000,
		AccruedMinor:   250000,
		CurrencyCode:   "VND",
		OrgCode:        "ORG-SECRET",
		Status:         "ACTIVE",
		WorkflowCaseID: &caseID,
		JournalEntryID: &entryID,
		CreatedBy:      "user-secret",
	}
}

func aiTestDetail(txns int) *service.SavingsDetail {
	rows := make([]repository.DepositTxn, 0, txns)
	for i := 0; i < txns; i++ {
		rows = append(rows, repository.DepositTxn{
			ID:           "txn-secret",
			TenantID:     "tenant-1",
			SavingsID:    "sav_secret",
			TxnType:      "DEPOSIT",
			AmountMinor:  1000000,
			CurrencyCode: "VND",
			TxnDate:      "2026-02-15",
			Status:       "POSTED",
			CreatedBy:    "user-secret",
		})
	}
	savings := aiTestSavings()
	return &service.SavingsDetail{Savings: &savings, Txns: rows}
}

func aiTestRouter() http.Handler {
	savings := &stubAISavingsSource{
		savings: []repository.Savings{aiTestSavings()},
		detail:  aiTestDetail(25),
	}
	rates := &stubAIRateSource{rates: []repository.InterestRate{{
		ID:            "rate-secret",
		TenantID:      "tenant-1",
		ProductCode:   "DPM-12M",
		TermMonths:    12,
		Method:        "SIMPLE",
		Denominator:   365,
		Rate:          5.5,
		EffectiveFrom: "2026-01-01",
		IsActive:      true,
		CreatedBy:     "user-secret",
	}}}
	return NewRouter(nil, handler.NewInternalAIHandler(savings, savings, rates))
}

func aiSignedRequest(t *testing.T, router http.Handler, path, tenantID, token string) *httptest.ResponseRecorder {
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

func aiValidToken(t *testing.T) string {
	t.Helper()
	token, err := identity.Issue(aiTestSecret, "ai-service", "deposit-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue ai-service token: %v", err)
	}
	return token
}

type aiListEnvelope struct {
	Result struct {
		Items   []map[string]any `json:"items"`
		Page    int              `json:"page"`
		PerPage int              `json:"per_page"`
		Total   int              `json:"total"`
	} `json:"result"`
	Success bool `json:"success"`
}

func aiDecode(t *testing.T, res *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Result  map[string]any `json:"result"`
		Success bool           `json:"success"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !envelope.Success {
		t.Fatalf("envelope success = false: %s", res.Body.String())
	}
	return envelope.Result
}

// aiForbiddenFields are the fields the deposit AI surface must never expose,
// regardless of what the service layer holds.
var aiForbiddenFields = []string{
	"tenant_id", "tenant-1", "sav_secret", "txn-secret", "rate-secret",
	"org_code", "ORG-SECRET", "workflow_case", "case-secret",
	"journal-secret", "created_by", "user-secret", "created_at", "updated_at",
}

func TestInternalAI_RequiresSignedAIServiceRequest(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	valid := aiValidToken(t)
	wrongSource, err := identity.Issue(aiTestSecret, "workflow-service", "deposit-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue wrong-source token: %v", err)
	}

	tests := []struct {
		name    string
		token   string
		tenant  string
		status  int
		wantEnv bool
	}{
		{name: "missing token", status: http.StatusUnauthorized},
		{name: "wrong source token", token: wrongSource, status: http.StatusForbidden},
		{name: "valid token without tenant", token: valid, status: http.StatusForbidden},
		{name: "valid signed request", token: valid, tenant: "tenant-1", status: http.StatusOK, wantEnv: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := aiSignedRequest(t, router, "/internal/ai/savings", tt.tenant, tt.token)
			if res.Code != tt.status {
				t.Errorf("status = %d, want %d", res.Code, tt.status)
			}
			if !tt.wantEnv {
				return
			}
			var envelope aiListEnvelope
			if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if !envelope.Success || len(envelope.Result.Items) != 1 {
				t.Fatalf("unexpected envelope: %+v", envelope)
			}
		})
	}
}

func TestInternalAI_ListSavings_RedactsInternalFields(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	res := aiSignedRequest(t, router, "/internal/ai/savings?q=SAV-2026", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range aiForbiddenFields {
		if strings.Contains(body, leaked) {
			t.Errorf("savings list leaks %q: %s", leaked, body)
		}
	}
	var envelope aiListEnvelope
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Result.Items) != 1 || envelope.Result.Items[0]["savings_code"] != "SAV-2026-001" {
		t.Fatalf("unexpected savings envelope: %+v", envelope)
	}
}

func TestInternalAI_GetSavingsDetail_RedactsAndCapsTransactions(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	savings := &stubAISavingsSource{savings: []repository.Savings{aiTestSavings()}, detail: aiTestDetail(25)}
	router := NewRouter(nil, handler.NewInternalAIHandler(savings, savings, &stubAIRateSource{}))

	res := aiSignedRequest(t, router, "/internal/ai/savings/SAV-2026-001", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range aiForbiddenFields {
		if strings.Contains(body, leaked) {
			t.Errorf("savings detail leaks %q: %s", leaked, body)
		}
	}
	detail := aiDecode(t, res)
	transactions, ok := detail["transactions"].([]any)
	if !ok {
		t.Fatalf("expected transactions array, got %v", detail["transactions"])
	}
	if len(transactions) != 20 || detail["transaction_count"] != float64(25) {
		t.Fatalf("expected 20 capped rows from 25, got %d (count %v)", len(transactions), detail["transaction_count"])
	}
	savingsRow, ok := detail["savings"].(map[string]any)
	if !ok || savingsRow["savings_code"] != "SAV-2026-001" || savingsRow["status"] != "ACTIVE" {
		t.Fatalf("unexpected redacted savings row: %v", detail["savings"])
	}
	if savings.seenTenant != "tenant-1" || savings.seenCode != "SAV-2026-001" {
		t.Errorf("service received (%q, %q), want (tenant-1, SAV-2026-001)", savings.seenTenant, savings.seenCode)
	}
}

func TestInternalAI_GetSavingsDetail_NotFound(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	res := aiSignedRequest(t, router, "/internal/ai/savings/SAV-MISSING", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestInternalAI_ListInterestRates_RedactsAuditFields(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	res := aiSignedRequest(t, router, "/internal/ai/interest-rates?product_code=DPM-12M", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range aiForbiddenFields {
		if strings.Contains(body, leaked) {
			t.Errorf("interest rates leak %q: %s", leaked, body)
		}
	}
	var envelope aiListEnvelope
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Result.Items) != 1 || envelope.Result.Items[0]["rate"] != 5.5 {
		t.Fatalf("unexpected rate envelope: %+v", envelope)
	}
}

func TestInternalAI_RejectsBadParameters(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()
	token := aiValidToken(t)

	longCode := strings.Repeat("x", 129)
	paths := []string{
		"/internal/ai/savings?per_page=500",
		"/internal/ai/savings?page=0",
		"/internal/ai/savings/" + longCode,
		"/internal/ai/interest-rates?product_code=" + longCode,
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			res := aiSignedRequest(t, router, path, "tenant-1", token)
			if res.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d (body %s)", res.Code, http.StatusBadRequest, res.Body.String())
			}
		})
	}
}
