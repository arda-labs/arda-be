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

	"github.com/arda-labs/arda/apps/statistical-service/internal/handler"
	"github.com/arda-labs/arda/apps/statistical-service/internal/reports"
	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const aiTestSecret = "01234567890123456789012345678901"

// errAIMissingTenant is a stub-level guard: if the AI handler ever loses the
// delegated-tenant re-check, the stub fails the request instead of serving
// tenant data.
var errAIMissingTenant = errors.New("stub: missing tenant scope")

// stubStatisticalAISource stands in for StatisticalService behind the
// InternalAIHandler so the signed-request tests run without a database. It
// records the verified tenant and the filters it was handed.
type stubStatisticalAISource struct {
	definitions      []repository.ReportDefinition
	indicators       []repository.Indicator
	submissions      []repository.ReportSubmission
	indicatorResults []repository.IndicatorResult
	reportRun        [][]any
	total            int

	lastTenantID         string
	lastDefinitionQ      string
	lastSubmissionParams repository.ListSubmissionsParams
	submissionCalls      int
}

func (s *stubStatisticalAISource) ListIndicatorResults(_ context.Context, params repository.ListIndicatorResultsParams) ([]repository.IndicatorResult, error) {
	if params.TenantID == "" {
		return nil, errAIMissingTenant
	}
	s.lastTenantID = params.TenantID
	return s.indicatorResults, nil
}

func (s *stubStatisticalAISource) ListAlerts(_ context.Context, tenantID, _, _ string) ([]repository.IndicatorAlert, error) {
	if tenantID == "" {
		return nil, errAIMissingTenant
	}
	s.lastTenantID = tenantID
	return []repository.IndicatorAlert{}, nil
}

func (s *stubStatisticalAISource) RunReport(_ context.Context, tenantID, code string, params map[string]string) (*repository.ReportDefinition, *reports.ReportQuery, [][]any, error) {
	if tenantID == "" {
		return nil, nil, nil, errAIMissingTenant
	}
	s.lastTenantID = tenantID
	if s.reportRun == nil {
		return nil, nil, nil, errors.New("stub: report not found")
	}
	return &repository.ReportDefinition{Code: code, Name: "Stub " + code},
		&reports.ReportQuery{QueryID: code, Columns: []string{"segment", "customer_count"}}, s.reportRun, nil
}

func (s *stubStatisticalAISource) ListReportDefinitions(_ context.Context, params repository.ListReportDefinitionsParams) ([]repository.ReportDefinition, error) {
	if params.TenantID == "" {
		return nil, errAIMissingTenant
	}
	s.lastTenantID = params.TenantID
	s.lastDefinitionQ = params.Q
	return s.definitions, nil
}

func (s *stubStatisticalAISource) ListIndicators(_ context.Context, params repository.ListIndicatorsParams) ([]repository.Indicator, error) {
	if params.TenantID == "" {
		return nil, errAIMissingTenant
	}
	s.lastTenantID = params.TenantID
	return s.indicators, nil
}

func (s *stubStatisticalAISource) ListSubmissions(_ context.Context, params repository.ListSubmissionsParams) ([]repository.ReportSubmission, int, error) {
	if params.TenantID == "" {
		return nil, 0, errAIMissingTenant
	}
	s.lastTenantID = params.TenantID
	s.lastSubmissionParams = params
	s.submissionCalls++
	total := s.total
	if total == 0 {
		total = len(s.submissions)
	}
	return s.submissions, total, nil
}

func aiStatTimePtr(value time.Time) *time.Time { return &value }

func aiTestSource() *stubStatisticalAISource {
	submittedAt := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	return &stubStatisticalAISource{
		definitions: []repository.ReportDefinition{{
			ID:             "rptdef_1",
			TenantID:       "tenant-1",
			Code:           "BC01",
			Name:           "Báo cáo tháng 01",
			GroupCode:      "MONTHLY",
			QueryID:        "monthly_summary_v1",
			ParamSchema:    json.RawMessage(`{"period_code":"string","secret_param":"internal"}`),
			TemplateFileID: func() *string { v := "media-internal"; return &v }(),
			OutputFormat:   "XLSX",
			IsActive:       true,
			CreatedBy:      "user-internal",
			UpdatedBy:      "user-internal",
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}},
		indicators: []repository.Indicator{{
			ID:        "ind_1",
			TenantID:  "tenant-1",
			Code:      "IND-01",
			Name:      "Tổng dư nợ",
			Unit:      "VND",
			GroupCode: "CREDIT",
			IsActive:  true,
			CreatedBy: "user-internal",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
		submissions: []repository.ReportSubmission{{
			ID:             "rptsub_1",
			TenantID:       "tenant-1",
			ReportCode:     "BC01",
			PeriodCode:     "2026-08",
			Status:         "SUBMITTED",
			Payload:        json.RawMessage(`{"account_number":"1234567890","customer_name":"Nguyễn Văn A"}`),
			WorkflowCaseID: func() *string { v := "case-internal"; return &v }(),
			SubmittedBy:    "user-internal",
			SubmittedAt:    aiStatTimePtr(submittedAt),
			CreatedBy:      "user-internal",
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}},
	}
}

func aiStatisticalRouter(source *stubStatisticalAISource) http.Handler {
	return NewRouter(handler.NewStatisticalHandler(nil), handler.NewInternalAIHandler(source), nil, nil, nil)
}

// aiStatisticalRouterWithRun also carries report-run and indicator-result data
// so the NL-routing tools have something to return.
func aiStatisticalRouterWithRun() http.Handler {
	source := aiTestSource()
	source.reportRun = [][]any{{"RETAIL", int64(3)}, {"CORP", int64(1)}}
	source.indicatorResults = []repository.IndicatorResult{{
		IndicatorCode: "60000.01",
		PeriodCode:    "2026-09",
		Value:         func() *float64 { v := 3.0; return &v }(),
		Source:        "COMPUTED",
	}}
	return aiStatisticalRouter(source)
}

// TestInternalAI_RunReportRequiresCodeAndPeriod keeps the routing contract
// strict: the assistant must name a catalogued report and a period.
func TestInternalAI_RunReportRequiresCodeAndPeriod(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiStatisticalRouterWithRun()
	token := aiValidToken(t)

	res := aiSignedRequest(t, router, "/internal/ai/report-run?period_code=2026-09", "tenant-1", token)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing report_code status = %d, want 400", res.Code)
	}
	res = aiSignedRequest(t, router, "/internal/ai/report-run?report_code=CUSTOMER_SUMMARY", "tenant-1", token)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing period_code status = %d, want 400", res.Code)
	}
}

// TestInternalAI_RunReportReturnsRowsWithoutWiring proves the run tool returns
// computed rows and never leaks the internal query wiring.
func TestInternalAI_RunReportReturnsRowsWithoutWiring(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiStatisticalRouterWithRun()

	res := aiSignedRequest(t, router,
		"/internal/ai/report-run?report_code=CUSTOMER_SUMMARY&period_code=2026-09", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range []string{"query_id", "tenant_id", "param_schema", "sql"} {
		if strings.Contains(body, leaked) {
			t.Errorf("report-run leaks %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, `"row_count":2`) {
		t.Errorf("report-run missing rows: %s", body)
	}
}

// TestInternalAI_ListIndicatorResultsExposesValuesOnly: values + dimension key
// are shared, the declarative formula is not.
func TestInternalAI_ListIndicatorResultsExposesValuesOnly(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiStatisticalRouterWithRun()

	res := aiSignedRequest(t, router, "/internal/ai/indicator-results?period_code=2026-09", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "60000.01") || !strings.Contains(body, `"value":3`) {
		t.Errorf("indicator results missing values: %s", body)
	}
	for _, leaked := range []string{"formula", "sources", "dimensions", "tenant_id", "created_by"} {
		if strings.Contains(body, leaked) {
			t.Errorf("indicator results leak %q: %s", leaked, body)
		}
	}
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
	token, err := identity.Issue(aiTestSecret, "ai-service", "statistical-service", time.Now(), time.Minute)
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
	router := aiStatisticalRouter(aiTestSource())

	valid := aiValidToken(t)
	wrongSource, err := identity.Issue(aiTestSecret, "workflow-service", "statistical-service", time.Now(), time.Minute)
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
			res := aiSignedRequest(t, router, "/internal/ai/report-definitions", tt.tenant, tt.token)
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
			if envelope.Result.Items[0]["code"] != "BC01" {
				t.Errorf("allowlisted field missing: %v", envelope.Result.Items[0])
			}
		})
	}
}

func TestInternalAI_ListReportDefinitions_DropsQueryIDAndParams(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiStatisticalRouter(aiTestSource())

	res := aiSignedRequest(t, router, "/internal/ai/report-definitions", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range []string{
		"tenant_id", "query_id", "param_schema", "template_file_id", "created_by", "updated_by",
		"created_at", "updated_at", "monthly_summary_v1", "secret_param", "media-internal", "user-internal",
	} {
		if strings.Contains(body, leaked) {
			t.Errorf("response leaks %q: %s", leaked, body)
		}
	}
	item := aiDecodeList(t, res).Result.Items[0]
	for _, key := range []string{"id", "code", "name", "group_code", "output_format", "is_active"} {
		if _, ok := item[key]; !ok {
			t.Errorf("allowlisted key %q missing: %v", key, item)
		}
	}
}

func TestInternalAI_ListIndicators_RedactsActorAndTimestamps(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiStatisticalRouter(aiTestSource())

	res := aiSignedRequest(t, router, "/internal/ai/indicators", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range []string{"tenant_id", "created_by", "created_at", "updated_at", "user-internal"} {
		if strings.Contains(body, leaked) {
			t.Errorf("response leaks %q: %s", leaked, body)
		}
	}
	item := aiDecodeList(t, res).Result.Items[0]
	if item["unit"] != "VND" || item["group_code"] != "CREDIT" {
		t.Errorf("allowlisted fields wrong: %v", item)
	}
}

func TestInternalAI_ListSubmissions_RedactsPayloadAndPII(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := aiTestSource()
	router := aiStatisticalRouter(source)

	res := aiSignedRequest(t,
		router,
		"/internal/ai/submissions?report_code=BC01&period_code=2026-08&status=submitted&tenant_id=tenant-evil",
		"tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range []string{
		"payload", "tenant_id", "workflow_case_id", "submitted_by", "created_by",
		"account_number", "customer_name", "Nguyễn Văn A", "1234567890",
		"case-internal", "user-internal",
	} {
		if strings.Contains(body, leaked) {
			t.Errorf("response leaks %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, `"submitted_at"`) {
		t.Errorf("submitted_at date should be exposed: %s", body)
	}
	if source.lastTenantID != "tenant-1" {
		t.Errorf("tenant must come from X-Tenant-Id, got %q", source.lastTenantID)
	}
	if source.lastSubmissionParams.Status != "SUBMITTED" ||
		source.lastSubmissionParams.ReportCode != "BC01" ||
		source.lastSubmissionParams.PeriodCode != "2026-08" {
		t.Errorf("submission filters not forwarded: %+v", source.lastSubmissionParams)
	}
}

func TestInternalAI_ListSubmissions_RejectsInvalidStatus(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := aiTestSource()
	router := aiStatisticalRouter(source)

	res := aiSignedRequest(t, router, "/internal/ai/submissions?status=NOT_A_STATUS", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", res.Code, res.Body.String())
	}
	if source.submissionCalls != 0 {
		t.Errorf("repo called for invalid status (%d calls)", source.submissionCalls)
	}
}

func TestInternalAI_PaginationClampedToTwenty(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiStatisticalRouter(aiTestSource())

	res := aiSignedRequest(t, router, "/internal/ai/report-definitions?per_page=500", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("per_page=500 status = %d, want %d", res.Code, http.StatusBadRequest)
	}

	res = aiSignedRequest(t, router, "/internal/ai/indicators?sort=name", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("sort status = %d, want %d", res.Code, http.StatusBadRequest)
	}

	res = aiSignedRequest(t, router, "/internal/ai/report-definitions", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("default page status = %d, want 200: %s", res.Code, res.Body.String())
	}
	envelope := aiDecodeList(t, res)
	if envelope.Result.Page != 1 || envelope.Result.PerPage != 10 || envelope.Result.Total != 1 {
		t.Fatalf("unexpected default page envelope: %+v", envelope)
	}
}

// TestInternalAI_QueryClampedRuneSafe ensures an oversized q never becomes an
// unbounded query and never breaks on multi-byte Vietnamese input.
func TestInternalAI_QueryClampedRuneSafe(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	source := aiTestSource()
	router := aiStatisticalRouter(source)

	res := aiSignedRequest(t, router, "/internal/ai/report-definitions?q="+strings.Repeat("ố", 200), "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
	}
	if len([]rune(source.lastDefinitionQ)) != 128 {
		t.Errorf("q rune length = %d, want 128", len([]rune(source.lastDefinitionQ)))
	}
}
