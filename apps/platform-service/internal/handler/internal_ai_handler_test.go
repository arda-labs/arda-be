package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

func marshalMap(t *testing.T, v any) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestToAIOrganizations_RedactsTenant(t *testing.T) {
	parentID := "org-parent"
	parentName := "Trung tâm"
	items := []domain.Organization{{
		ID: "org-1", TenantID: "tenant-1", ParentID: &parentID, ParentName: &parentName,
		Code: "ORG-001", Name: "Phòng kinh doanh", AdminUnitCode: &parentID, Address: &parentName,
		IsActive: true,
	}}

	out := marshalMap(t, toAIOrganizations(items))
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	allowed := map[string]bool{"id": true, "code": true, "name": true, "parentId": true, "parentName": true, "isActive": true}
	for key := range out[0] {
		if !allowed[key] {
			t.Errorf("field %q leaked into the AI shape (allowlist violation)", key)
		}
	}
	if _, ok := out[0]["tenant_id"]; ok {
		t.Errorf("tenant_id must never be exposed to the AI surface")
	}
	if out[0]["code"] != "ORG-001" || out[0]["name"] != "Phòng kinh doanh" {
		t.Errorf("unexpected redacted payload: %v", out[0])
	}
}

func TestToAIParameters_FiltersSecrets(t *testing.T) {
	items := []domain.Parameter{
		{ID: "p1", Key: "ui.theme", Value: "dark", ValueType: "string", ScopeType: "tenant", IsSecret: false},
		{ID: "p2", Key: "smtp.password", Value: "s3cr3t", ValueType: "string", ScopeType: "tenant", IsSecret: true},
	}

	out := marshalMap(t, toAIParameters(items))
	if len(out) != 1 {
		t.Fatalf("expected the secret row to be filtered, got %d items: %v", len(out), out)
	}
	allowed := map[string]bool{"id": true, "key": true, "value": true, "valueType": true, "scopeType": true, "description": true}
	for key := range out[0] {
		if !allowed[key] {
			t.Errorf("field %q leaked into the AI shape (allowlist violation)", key)
		}
	}
	for _, item := range out {
		if item["value"] == "s3cr3t" {
			t.Errorf("secret parameter value leaked to the AI surface")
		}
	}
	if _, ok := out[0]["tenant_id"]; ok {
		t.Errorf("tenant_id must never be exposed to the AI surface")
	}
	if _, ok := out[0]["scope_id"]; ok {
		t.Errorf("scope_id must never be exposed to the AI surface")
	}
}

func TestToAILookupValues_RedactsInternalFields(t *testing.T) {
	metadata := "meta"
	items := []domain.LookupValue{{
		ID: "lv-1", CategoryID: "cat-1", Code: "CASH", Name: "Tiền mặt",
		SortOrder: 2, IsActive: true, Metadata: &metadata,
	}}

	out := marshalMap(t, toAILookupValues(items))
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	allowed := map[string]bool{"id": true, "code": true, "name": true, "sortOrder": true, "isActive": true}
	for key := range out[0] {
		if !allowed[key] {
			t.Errorf("field %q leaked into the AI shape (allowlist violation)", key)
		}
	}
}

func TestInternalAIHandlers_RejectNonGET(t *testing.T) {
	ph := &PlatformHandler{} // svc is nil: method check must fire before any repo call
	ch := &CalendarHandler{} // service is nil: method check must fire before any repo call

	rec := httptest.NewRecorder()
	ph.InternalAIListOrganizations(rec, httptest.NewRequest(http.MethodPost, "/internal/ai/organizations", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("organizations: expected 405 for POST, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	ph.InternalAIListParameters(rec, httptest.NewRequest(http.MethodPost, "/internal/ai/parameters", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("parameters: expected 405 for POST, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	ph.InternalAILookupValues(rec, httptest.NewRequest(http.MethodPost, "/internal/ai/lookups/UNITS/values", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("lookups: expected 405 for POST, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	ch.InternalAICalendarStatus(rec, httptest.NewRequest(http.MethodPost, "/internal/ai/calendar/status", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("calendar status: expected 405 for POST, got %d", rec.Code)
	}
}

func TestInternalAIHandlers_RequireTenantHeader(t *testing.T) {
	ph := &PlatformHandler{} // svc is nil: tenant guard must fire before any repo call
	ch := &CalendarHandler{}

	for _, tc := range []struct {
		name    string
		method  string
		path    string
		handler func(w http.ResponseWriter, r *http.Request)
	}{
		{"organizations", http.MethodGet, "/internal/ai/organizations", ph.InternalAIListOrganizations},
		{"parameters", http.MethodGet, "/internal/ai/parameters", ph.InternalAIListParameters},
		{"lookups", http.MethodGet, "/internal/ai/lookups/UNITS/values", ph.InternalAILookupValues},
		{"calendar status", http.MethodGet, "/internal/ai/calendar/status", ch.InternalAICalendarStatus},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handler(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 without X-Tenant-Id, got %d", rec.Code)
			}
		})
	}
}

func TestInternalAIHandlers_ValidateInputs(t *testing.T) {
	ph := &PlatformHandler{} // svc is nil: validation must fire before any repo call

	base := httptest.NewRequest(http.MethodGet, "/internal/ai/organizations", nil)
	base.Header.Set("X-Tenant-Id", "tenant-1")

	rec := httptest.NewRecorder()
	ph.InternalAIListOrganizations(rec, withQuery(base, "q="+strings.Repeat("a", aiMaxQueryLength+1)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for q longer than 128, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	ph.InternalAIListOrganizations(rec, withQuery(base, "per_page=0"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for per_page=0, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	ph.InternalAIListOrganizations(rec, withQuery(base, "per_page=21"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for per_page=21, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	ph.InternalAIListOrganizations(rec, withQuery(base, "page=abc"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for page=abc, got %d", rec.Code)
	}

	longCode := httptest.NewRequest(http.MethodGet, "/internal/ai/lookups/"+strings.Repeat("a", aiMaxLookupCode+1)+"/values", nil)
	longCode.Header.Set("X-Tenant-Id", "tenant-1")
	rec = httptest.NewRecorder()
	ph.InternalAILookupValues(rec, longCode)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for lookup code longer than 64, got %d", rec.Code)
	}
}

// withQuery returns a shallow clone of req with the given raw query string.
func withQuery(req *http.Request, query string) *http.Request {
	clone := req.Clone(req.Context())
	clone.URL = req.URL.JoinPath()
	clone.URL.RawQuery = query
	return clone
}
