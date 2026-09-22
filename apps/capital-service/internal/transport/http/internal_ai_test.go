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

	"github.com/arda-labs/arda/apps/capital-service/internal/handler"
	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const aiTestSecret = "01234567890123456789012345678901"

// errAIMissingTenant is a stub-level guard: if the AI handler ever loses the
// delegated-tenant re-check, the stub fails the request instead of serving
// tenant data.
var errAIMissingTenant = errors.New("stub: missing tenant scope")

// stubCapitalAISource stands in for CapitalService behind the
// InternalAIHandler so the signed-request tests run without a database. It
// records the verified tenant and the contract filters it was handed.
type stubCapitalAISource struct {
	fundTypes []repository.FundType
	products  []repository.CapitalProduct
	contracts []repository.CapitalContract
	total     int

	lastTenantID string
	lastParams   repository.ListContractsParams
	listCalls    int
}

func (s *stubCapitalAISource) ListFundTypes(_ context.Context, tenantID string, _ bool) ([]repository.FundType, error) {
	if tenantID == "" {
		return nil, errAIMissingTenant
	}
	s.lastTenantID = tenantID
	return s.fundTypes, nil
}

func (s *stubCapitalAISource) ListProducts(_ context.Context, tenantID string, _ bool) ([]repository.CapitalProduct, error) {
	if tenantID == "" {
		return nil, errAIMissingTenant
	}
	s.lastTenantID = tenantID
	return s.products, nil
}

func (s *stubCapitalAISource) ListContracts(_ context.Context, params repository.ListContractsParams) ([]repository.CapitalContract, int, error) {
	if params.TenantID == "" {
		return nil, 0, errAIMissingTenant
	}
	s.lastTenantID = params.TenantID
	s.lastParams = params
	s.listCalls++
	total := s.total
	if total == 0 {
		total = len(s.contracts)
	}
	return s.contracts, total, nil
}

func aiStrPtrCapital(value string) *string { return &value }

func aiTestSource() *stubCapitalAISource {
	return &stubCapitalAISource{
		fundTypes: []repository.FundType{{
			ID:        "cfcft_1",
			TenantID:  "tenant-1",
			Code:      "FT-VND",
			Name:      "Vốn huy động VND",
			IsActive:  true,
			CreatedBy: "user-internal",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
		products: []repository.CapitalProduct{{
			ID:           "cfcprd_1",
			TenantID:     "tenant-1",
			Code:         "PRD-12M",
			Name:         "Tiền gửi 12 tháng",
			FundTypeCode: "FT-VND",
			TermMonths:   12,
			InterestRate: 5.5,
			CurrencyCode: "VND",
			IsActive:     true,
			CreatedBy:    "user-internal",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}},
		contracts: []repository.CapitalContract{{
			ID:               "cfc_1",
			TenantID:         "tenant-1",
			ContractCode:     "HDV-2026-001",
			FundTypeCode:     "FT-VND",
			ProductCode:      "PRD-12M",
			CounterpartyCode: "CP-001",
			ContractDate:     "2026-09-01",
			MaturityDate:     "2027-09-01",
			AmountMinor:      100000000,
			InterestRate:     5.5,
			CurrencyCode:     "VND",
			Status:           "ACTIVE",
			OrgCode:          "ORG-1",
			WorkflowCaseID:   aiStrPtrCapital("case-internal"),
			JournalEntryID:   aiStrPtrCapital("je-internal"),
			CreatedBy:        "user-internal",
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}},
	}
}

func aiCapitalRouter(source *stubCapitalAISource) http.Handler {
	return NewRouter(handler.NewCapitalHandler(&fakeCapitalService{}), handler.NewInternalAIHandler(source), nil)
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
	token, err := identity.Issue(aiTestSecret, "ai-service", "capital-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue ai-service token: %v", err)
	}
	return token
}

func aiDecodeList(t *testing.T, res *httptest.ResponseRecorder) aiListEnvelope {
	t.Helper()
	var envelope aiListEnvelope
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return envelope
}

func TestInternalAI_RequiresSignedAIServiceRequest(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiCapitalRouter(aiTestSource())

	valid := aiValidToken(t)
	wrongSource, err := identity.Issue(aiTestSecret, "workflow-service", "capital-service", time.Now(), time.Minute)
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
		{name: "missing token", tenant: "tenant-1", status: http.StatusUnauthorized},
		{name: "wrong source token", token: wrongSource, tenant: "tenant-1", status: http.StatusForbidden},
		{name: "valid token without tenant", token: valid, status: http.StatusBadRequest},
		{name: "valid signed request", token: valid, tenant: "tenant-1", status: http.StatusOK, wantEnv: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := aiSignedRequest(t, router, "/internal/ai/contracts", tt.tenant, tt.token)
			if res.Code != tt.status {
				t.Errorf("status = %d, want %d (body %s)", res.Code, tt.status, res.Body.String())
			}
			if !tt.wantEnv {
				return
			}
			envelope := aiDecodeList(t, res)
			if !envelope.Success || len(envelope.Result.Items) != 1 {
				t.Fatalf("unexpected envelope: %+v", envelope)
			}
			if envelope.Result.Items[0]["contract_code"] != "HDV-2026-001" {
				t.Errorf("allowlisted field missing: %v", envelope.Result.Items[0])
			}
		})
	}
}

func TestInternalAI_ListContracts_RedactsInternalFields(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := aiTestSource()
	router := aiCapitalRouter(source)

	res := aiSignedRequest(t, router, "/internal/ai/contracts", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range []string{
		"tenant_id", "created_by", "created_at", "updated_at",
		"workflow_case_id", "journal_entry_id", "user-internal", "case-internal", "je-internal",
	} {
		if strings.Contains(body, leaked) {
			t.Errorf("response leaks %q: %s", leaked, body)
		}
	}
	item := aiDecodeList(t, res).Result.Items[0]
	for _, key := range []string{
		"id", "contract_code", "fund_type_code", "product_code", "counterparty_code",
		"contract_date", "maturity_date", "amount_minor", "interest_rate", "currency_code",
		"status", "org_code",
	} {
		if _, ok := item[key]; !ok {
			t.Errorf("allowlisted key %q missing: %v", key, item)
		}
	}
	if source.lastTenantID != "tenant-1" {
		t.Errorf("verified tenant = %q, want tenant-1", source.lastTenantID)
	}
}

func TestInternalAI_ListFundTypesAndProducts_RedactAndPage(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := aiTestSource()
	router := aiCapitalRouter(source)

	res := aiSignedRequest(t, router, "/internal/ai/fund-types", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("fund-types status = %d, want 200: %s", res.Code, res.Body.String())
	}
	if body := res.Body.String(); strings.Contains(body, "tenant_id") || strings.Contains(body, "created_by") {
		t.Errorf("fund-types leaks internal fields: %s", body)
	}
	envelope := aiDecodeList(t, res)
	if !envelope.Success || envelope.Result.PerPage != 10 || envelope.Result.Total != 1 {
		t.Fatalf("unexpected fund-types envelope: %+v", envelope)
	}

	res = aiSignedRequest(t, router, "/internal/ai/products", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("products status = %d, want 200: %s", res.Code, res.Body.String())
	}
	product := aiDecodeList(t, res).Result.Items[0]
	if product["term_months"] != float64(12) || product["interest_rate"] != 5.5 {
		t.Errorf("product pricing fields wrong: %v", product)
	}
	if body := res.Body.String(); strings.Contains(body, "tenant_id") || strings.Contains(body, "created_at") {
		t.Errorf("products leaks internal fields: %s", body)
	}
}

func TestInternalAI_PaginationClampedToTwenty(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiCapitalRouter(aiTestSource())

	res := aiSignedRequest(t, router, "/internal/ai/contracts?per_page=500", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("per_page=500 status = %d, want %d", res.Code, http.StatusBadRequest)
	}

	res = aiSignedRequest(t, router, "/internal/ai/contracts?per_page=21", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("per_page=21 status = %d, want %d", res.Code, http.StatusBadRequest)
	}

	res = aiSignedRequest(t, router, "/internal/ai/fund-types?sort=name", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("sort on reference catalog status = %d, want %d", res.Code, http.StatusBadRequest)
	}
}

func TestInternalAI_ListContracts_FiltersAndScopes(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := aiTestSource()
	router := aiCapitalRouter(source)

	res := aiSignedRequest(t,
		router,
		"/internal/ai/contracts?status=active&q=HDV&sort=contract_date&order=desc&page=2&per_page=5&tenant_id=tenant-evil",
		"tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
	}
	if source.lastTenantID != "tenant-1" {
		t.Errorf("tenant must come from X-Tenant-Id, got %q", source.lastTenantID)
	}
	if source.lastParams.Status != "ACTIVE" || source.lastParams.Q != "HDV" ||
		source.lastParams.Sort != "contract_date" || source.lastParams.Order != "desc" {
		t.Errorf("contract filters not forwarded: %+v", source.lastParams)
	}
	if source.lastParams.Page != 5 || source.lastParams.Size != 5 {
		t.Errorf("paging not forwarded: %+v", source.lastParams)
	}
	envelope := aiDecodeList(t, res)
	if envelope.Result.Page != 2 || envelope.Result.PerPage != 5 || envelope.Result.Total != 1 {
		t.Fatalf("unexpected page envelope: %+v", envelope)
	}

	res = aiSignedRequest(t, router, "/internal/ai/contracts?status=NOT_A_STATUS", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want 400", res.Code)
	}
	if source.listCalls != 1 {
		t.Errorf("repo called for invalid status (%d calls)", source.listCalls)
	}
}

// TestInternalAI_QueryClampedRuneSafe ensures an oversized q never becomes an
// unbounded query and never breaks on multi-byte Vietnamese input.
func TestInternalAI_QueryClampedRuneSafe(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := aiTestSource()
	router := aiCapitalRouter(source)

	long := strings.Repeat("ố", 200)
	res := aiSignedRequest(t, router, "/internal/ai/contracts?q="+long, "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
	}
	if len([]rune(source.lastParams.Q)) != 128 {
		t.Errorf("q rune length = %d, want 128", len([]rune(source.lastParams.Q)))
	}
}
