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

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
	"github.com/arda-labs/arda/apps/mdm-service/internal/handler"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const aiTestSecret = "01234567890123456789012345678901"

func aiStrPtr(value string) *string { return &value }

// errUnknownAICatalog / errMissingTenant are stub-level guards: if the router
// ever routes a foreign catalog at the AI handler, or the handler forgets the
// tenant re-check, the stub fails the request instead of serving data.
var (
	errUnknownAICatalog = errors.New("stub: unknown ai catalog")
	errMissingTenant    = errors.New("stub: missing tenant scope")
)

// stubCatalogSource / stubRateSource stand in for MasterService /
// InterestRateService behind the InternalAIHandler so the signed-request test
// runs without a database.
type stubCatalogSource struct{ items []domain.CatalogItem }

func (s *stubCatalogSource) List(_ context.Context, catalog, tenantID, q string, includeInactive bool) ([]domain.CatalogItem, error) {
	if catalog != "currencies" && catalog != "countries" {
		return nil, errUnknownAICatalog
	}
	if tenantID == "" {
		return nil, errMissingTenant
	}
	if q != "" && !strings.Contains(s.items[0].Name, q) {
		return []domain.CatalogItem{}, nil
	}
	return s.items, nil
}

type stubRateSource struct{ items []domain.InterestRate }

func (s *stubRateSource) List(_ context.Context, tenantID, q string, includeInactive bool) ([]domain.InterestRate, error) {
	if tenantID == "" {
		return nil, errMissingTenant
	}
	return s.items, nil
}

func aiTestRouter() http.Handler {
	catalogs := &stubCatalogSource{items: []domain.CatalogItem{{
		ID:         "cur_1",
		TenantID:   aiStrPtr("tenant-1"),
		Code:       "VND",
		Name:       "Việt Nam Đồng",
		IsActive:   true,
		Attributes: json.RawMessage(`{"symbol":"₫","decimal_places":0,"secret_field":"leak"}`),
	}}}
	rates := &stubRateSource{items: []domain.InterestRate{{
		ID:        "rate_1",
		TenantID:  aiStrPtr("tenant-1"),
		Code:      "LOAN-VND",
		Name:      "Lãi cho vay VND",
		RateType:  "loan",
		ApplyType: "by_term",
		IsActive:  true,
	}}}
	return NewRouter(nil, nil, handler.NewInternalAIHandler(catalogs, rates), nil)
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
	token, err := identity.Issue(aiTestSecret, "ai-service", "mdm-service", time.Now(), time.Minute)
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

func TestInternalAI_RequiresSignedAIServiceRequest(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	valid, err := identity.Issue(aiTestSecret, "ai-service", "mdm-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue valid token: %v", err)
	}

	tests := []struct {
		name    string
		token   string
		tenant  string
		status  int
		wantEnv bool // expect {"result":...} success envelope on 200s only
	}{
		{name: "missing token", status: http.StatusUnauthorized},
		{name: "valid token without tenant", token: valid, status: http.StatusBadRequest},
		{name: "wrong source token", token: func() string {
			token, _ := identity.Issue(aiTestSecret, "workflow-service", "mdm-service", time.Now(), time.Minute)
			return token
		}(), status: http.StatusForbidden},
		{name: "valid signed request", token: valid, tenant: "tenant-1", status: http.StatusOK, wantEnv: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := aiSignedRequest(t, router, "/internal/ai/currencies", tt.tenant, tt.token)
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

func TestInternalAI_ListCurrencies_RedactsTenantAndAttributes(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	res := aiSignedRequest(t, router, "/internal/ai/currencies", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	if strings.Contains(body, "tenant_id") {
		t.Errorf("response leaks tenant_id: %s", body)
	}
	var envelope aiListEnvelope
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	attrs, ok := envelope.Result.Items[0]["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("expected attributes object, got %v", envelope.Result.Items[0]["attributes"])
	}
	for _, key := range []string{"symbol", "decimal_places"} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("attributes missing allowlisted key %q: %v", key, attrs)
		}
	}
	for key := range attrs {
		if key != "symbol" && key != "decimal_places" {
			t.Errorf("attributes leaked non-allowlisted key %q", key)
		}
	}
}

func TestInternalAI_ListInterestRates_RedactsTenantAndTimestamps(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	res := aiSignedRequest(t, router, "/internal/ai/interest-rates", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	body := res.Body.String()
	for _, leaked := range []string{"tenant_id", "created_at", "updated_at", "decision_no", "decision_date"} {
		if strings.Contains(body, leaked) {
			t.Errorf("response leaks %s: %s", leaked, body)
		}
	}
}

func TestInternalAI_PaginationClampedToTwenty(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", aiTestSecret)
	router := aiTestRouter()

	res := aiSignedRequest(t, router, "/internal/ai/currencies?page=2&per_page=500", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("per_page=500 status = %d, want %d", res.Code, http.StatusBadRequest)
	}

	res = aiSignedRequest(t, router, "/internal/ai/countries", "tenant-1", aiValidToken(t))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var envelope aiListEnvelope
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !envelope.Success || envelope.Result.Page != 1 || envelope.Result.PerPage != 10 || envelope.Result.Total != 1 {
		t.Fatalf("unexpected default page envelope: %+v", envelope)
	}
	if strings.Contains(res.Body.String(), "tenant_id") {
		t.Errorf("countries response leaks tenant_id: %s", res.Body.String())
	}
}
