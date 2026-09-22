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

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const aiTestSecret = "01234567890123456789012345678901"

// errMissingAITenant is a stub-level guard: if the handler ever forgets the
// tenant re-check the stub fails the request instead of serving data.
var errMissingAITenant = errors.New("stub: missing tenant scope")

// stubAILoanSource stands in for LoanService behind the InternalAIHandler so
// the signed-request tests run without a database.
type stubAILoanSource struct {
	contracts  []domain.Contract
	plans      []domain.RepayPlan
	seenTenant string
	seenID     string
	seenCode   string
}

func (s *stubAILoanSource) ListContractsPaged(_ context.Context, tenantID, status, q, sort, order string, page, perPage int) ([]domain.Contract, int, error) {
	if tenantID == "" {
		return nil, 0, errMissingAITenant
	}
	s.seenTenant = tenantID
	return s.contracts, len(s.contracts), nil
}

func (s *stubAILoanSource) GetContract(_ context.Context, tenantID, id string) (domain.Contract, error) {
	if tenantID == "" {
		return domain.Contract{}, errMissingAITenant
	}
	s.seenTenant = tenantID
	s.seenID = id
	for _, item := range s.contracts {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.Contract{}, ardaerrors.New(ardaerrors.CodeNotFound, "contract not found")
}

func (s *stubAILoanSource) ListRepayPlans(_ context.Context, tenantID, contractCode, agreementCode string) ([]domain.RepayPlan, error) {
	if tenantID == "" {
		return nil, errMissingAITenant
	}
	if contractCode == "" && agreementCode == "" {
		return nil, errors.New("stub: schedule key is required")
	}
	s.seenTenant = tenantID
	s.seenCode = contractCode
	return s.plans, nil
}

// aiTestContract carries every field the AI surface must never forward:
// tenant_id, staff and org linkage, workflow ids and audit fields.
func aiTestContract() domain.Contract {
	caseID := "case-secret"
	return domain.Contract{
		ID:               "ctrt_1",
		TenantID:         "tenant-1",
		ContractCode:     "LN-2026-001",
		ContractNo:       "01/2026/HDTD",
		CustomerCode:     "CIF-9001",
		EmployeeCode:     "EMP-SECRET",
		ContractTypeCode: "SHORT_TERM",
		ProductCode:      "LOAN-SME-01",
		InterestRate:     9.5,
		InterestRateType: "FIXED",
		PurposeCode:      "PURPOSE-SECRET",
		ContractDate:     "2026-01-15",
		LoanTerm:         12,
		TermUnit:         "MONTH",
		MaturityDate:     "2027-01-15",
		LoanAmt:          500000000,
		Status:           "ACTIVE",
		WorkflowCaseID:   &caseID,
		WorkflowCaseCode: "CASE-SECRET",
		OrgCode:          "ORG-SECRET",
		CreatedBy:        "user-secret",
	}
}

func aiTestRouter() http.Handler {
	source := &stubAILoanSource{
		contracts: []domain.Contract{aiTestContract()},
		plans: []domain.RepayPlan{{
			ID:               "plan-secret",
			TenantID:         "tenant-1",
			ContractCode:     "LN-2026-001",
			AgreementCode:    "AG-001",
			PlanNo:           1,
			TermNo:           1,
			FromDate:         "2026-02-15",
			ToDate:           "2026-03-15",
			InterestRate:     9.5,
			PlanPrincipalAmt: 40000000,
			PlanInterestAmt:  4000000,
			IsActive:         true,
		}},
	}
	return NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, handler.NewInternalAIHandler(source), nil, nil)
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
	token, err := identity.Issue(aiTestSecret, "ai-service", "loan-service", time.Now(), time.Minute)
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

func aiDetailEnvelope(t *testing.T, res *httptest.ResponseRecorder) map[string]any {
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

// aiForbiddenFields are the fields the loan AI surface must never expose,
// regardless of what the service layer holds.
var aiForbiddenFields = []string{
	"tenant_id", "tenant-1", "employee_code", "EMP-SECRET",
	"workflow_case", "CASE-SECRET", "case-secret",
	"org_code", "ORG-SECRET", "created_by", "user-secret",
	"created_at", "updated_at", "plan-secret",
}

func TestInternalAI_RequiresSignedAIServiceRequest(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	valid := aiValidToken(t)
	wrongSource, err := identity.Issue(aiTestSecret, "workflow-service", "loan-service", time.Now(), time.Minute)
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
		{name: "valid token without tenant", token: valid, status: http.StatusBadRequest},
		{name: "valid signed request", token: valid, tenant: "tenant-1", status: http.StatusOK, wantEnv: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := aiSignedRequest(t, router, "/internal/ai/contracts", tt.tenant, tt.token)
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

func TestInternalAI_ListContracts_ReturnsRedactedPage(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	res := aiSignedRequest(t, router, "/internal/ai/contracts?q=LN-2026&per_page=5", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range aiForbiddenFields {
		if strings.Contains(body, leaked) {
			t.Errorf("contract list leaks %q: %s", leaked, body)
		}
	}
	var envelope aiListEnvelope
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Result.Items) != 1 || envelope.Result.PerPage != 5 {
		t.Fatalf("unexpected list envelope: %+v", envelope)
	}
	if envelope.Result.Items[0]["contract_code"] != "LN-2026-001" {
		t.Errorf("contract_code missing from redacted item: %v", envelope.Result.Items[0])
	}
}

func TestInternalAI_GetContract_RedactsInternalFields(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := &stubAILoanSource{contracts: []domain.Contract{aiTestContract()}}
	router := NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, handler.NewInternalAIHandler(source), nil, nil)

	res := aiSignedRequest(t, router, "/internal/ai/contracts/ctrt_1", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range aiForbiddenFields {
		if strings.Contains(body, leaked) {
			t.Errorf("contract detail leaks %q: %s", leaked, body)
		}
	}
	detail := aiDetailEnvelope(t, res)
	if detail["contract_code"] != "LN-2026-001" || detail["customer_code"] != "CIF-9001" {
		t.Errorf("redacted detail missing business keys: %v", detail)
	}
	if source.seenTenant != "tenant-1" || source.seenID != "ctrt_1" {
		t.Errorf("service received (%q, %q), want (tenant-1, ctrt_1)", source.seenTenant, source.seenID)
	}
}

func TestInternalAI_GetContract_NotFound(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	res := aiSignedRequest(t, router, "/internal/ai/contracts/ctrt_missing", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestInternalAI_ListRepayPlans_RequiresScheduleKeyAndRedacts(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	res := aiSignedRequest(t, router, "/internal/ai/repay-plans", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing schedule key status = %d, want %d", res.Code, http.StatusBadRequest)
	}

	res = aiSignedRequest(t, router, "/internal/ai/repay-plans?contract_code=LN-2026-001", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range aiForbiddenFields {
		if strings.Contains(body, leaked) {
			t.Errorf("repay plan leaks %q: %s", leaked, body)
		}
	}
	var envelope aiListEnvelope
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Result.Items) != 1 || envelope.Result.Items[0]["term_no"] == nil {
		t.Fatalf("unexpected repay-plan envelope: %+v", envelope)
	}
}

func TestInternalAI_RejectsBadParameters(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()
	token := aiValidToken(t)

	longID := strings.Repeat("x", 129)
	paths := []string{
		"/internal/ai/contracts?per_page=500",
		"/internal/ai/contracts?page=0",
		"/internal/ai/contracts?all=true",
		"/internal/ai/contracts/" + longID,
		"/internal/ai/repay-plans?contract_code=LN-2026-001&per_page=50",
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
