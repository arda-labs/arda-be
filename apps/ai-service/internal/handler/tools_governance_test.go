package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeGovernance struct {
	overrides map[string]bool
	available bool
}

func newFakeGovernance() *fakeGovernance {
	return &fakeGovernance{overrides: map[string]bool{}, available: true}
}

func (f *fakeGovernance) EnsureFresh(context.Context) error { return nil }

func (f *fakeGovernance) Snapshot() map[string]bool {
	out := make(map[string]bool, len(f.overrides))
	for method, enabled := range f.overrides {
		out[method] = enabled
	}
	return out
}

func (f *fakeGovernance) Available() bool { return f.available }

func (f *fakeGovernance) SetOverride(_ context.Context, methodName string, enabled bool, _ string) (time.Time, error) {
	f.overrides[methodName] = enabled
	return time.Now().UTC(), nil
}

func (f *fakeGovernance) ClearOverride(_ context.Context, methodName string, _ string) (time.Time, error) {
	delete(f.overrides, methodName)
	return time.Now().UTC(), nil
}

func governanceTestOptions() RouterOptions {
	return RouterOptions{
		CatalogTools: []CatalogToolDTO{
			{
				MethodName: "crm.getCustomer", SDKPath: "arda.crm.getCustomer", Domain: "crm",
				Kind: "read", Risk: "low", ContractEnabled: true, Source: "internal",
			},
			{
				MethodName: "hrm.listEmployees", SDKPath: "arda.hrm.listEmployees", Domain: "hrm",
				Kind: "read", Risk: "medium", ContractEnabled: true, Source: "internal",
			},
			{
				MethodName: "crm.exportCustomer", SDKPath: "arda.crm.exportCustomer", Domain: "crm",
				Kind: "confirm", Risk: "medium", ContractEnabled: false, Source: "internal",
			},
		},
	}
}

func decodeToolsBody(t *testing.T, body string) []CatalogToolDTO {
	t.Helper()
	var items []CatalogToolDTO
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode tools body: %v (body=%s)", err, body)
	}
	return items
}

func TestListToolsIncludesGovernanceState(t *testing.T) {
	gov := newFakeGovernance()
	gov.overrides["hrm.listEmployees"] = false
	options := governanceTestOptions()
	options.ToolGovernance = gov

	router := NewRouterWithOptions(nil, nil, options)
	req := httptest.NewRequest(http.MethodGet, "/api/ai/tools", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}

	items := decodeToolsBody(t, res.Body.String())
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	byName := map[string]CatalogToolDTO{}
	for _, item := range items {
		byName[item.MethodName] = item
	}
	if got := byName["crm.getCustomer"]; !got.Enabled || !got.ContractEnabled || got.OverrideEnabled != nil {
		t.Fatalf("crm.getCustomer state = %+v, want enabled contract default", got)
	}
	got := byName["hrm.listEmployees"]
	if got.Enabled || got.OverrideEnabled == nil || *got.OverrideEnabled {
		t.Fatalf("hrm.listEmployees state = %+v, want runtime-disabled override", got)
	}
	if disabled := byName["crm.exportCustomer"]; disabled.Enabled {
		t.Fatalf("crm.exportCustomer state = %+v, want contract-disabled", disabled)
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/api/ai/tools?enabled=false", nil)
	filterRes := httptest.NewRecorder()
	router.ServeHTTP(filterRes, filterReq)
	filtered := decodeToolsBody(t, filterRes.Body.String())
	if len(filtered) != 2 {
		t.Fatalf("enabled=false items = %d, want 2", len(filtered))
	}
}

func TestUpdateToolEndpointSetAndClear(t *testing.T) {
	gov := newFakeGovernance()
	options := governanceTestOptions()
	options.ToolGovernance = gov
	router := NewRouterWithOptions(nil, nil, options)

	setReq := httptest.NewRequest(http.MethodPatch, "/api/ai/tools/crm.getCustomer", strings.NewReader(`{"enabled":false}`))
	setAIIdentityHeaders(setReq)
	setRes := httptest.NewRecorder()
	router.ServeHTTP(setRes, setReq)
	if setRes.Code != http.StatusOK {
		t.Fatalf("set status = %d, body = %s", setRes.Code, setRes.Body.String())
	}
	var updated CatalogToolDTO
	if err := json.Unmarshal(setRes.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode set response: %v", err)
	}
	if updated.Enabled || updated.OverrideEnabled == nil || *updated.OverrideEnabled {
		t.Fatalf("set response = %+v, want disabled with override false", updated)
	}
	if updated.UpdatedBy != "user-1" || updated.UpdatedAt == "" {
		t.Fatalf("set response audit fields = %q/%q, want user-1/timestamp", updated.UpdatedBy, updated.UpdatedAt)
	}

	clearReq := httptest.NewRequest(http.MethodPatch, "/api/ai/tools/crm.getCustomer", strings.NewReader(`{"clearOverride":true}`))
	setAIIdentityHeaders(clearReq)
	clearRes := httptest.NewRecorder()
	router.ServeHTTP(clearRes, clearReq)
	if clearRes.Code != http.StatusOK {
		t.Fatalf("clear status = %d, body = %s", clearRes.Code, clearRes.Body.String())
	}
	var cleared CatalogToolDTO
	if err := json.Unmarshal(clearRes.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("decode clear response: %v", err)
	}
	if !cleared.Enabled || cleared.OverrideEnabled != nil {
		t.Fatalf("clear response = %+v, want contract default", cleared)
	}
}

func TestUpdateToolEndpointRejectsInvalidRequests(t *testing.T) {
	gov := newFakeGovernance()
	options := governanceTestOptions()
	options.ToolGovernance = gov
	router := NewRouterWithOptions(nil, nil, options)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "unknown tool", method: http.MethodPatch, path: "/api/ai/tools/nope.missing", body: `{"enabled":false}`, status: http.StatusNotFound},
		{name: "enable beyond contract", method: http.MethodPatch, path: "/api/ai/tools/crm.exportCustomer", body: `{"enabled":true}`, status: http.StatusConflict},
		{name: "both fields", method: http.MethodPatch, path: "/api/ai/tools/crm.getCustomer", body: `{"enabled":false,"clearOverride":true}`, status: http.StatusBadRequest},
		{name: "neither field", method: http.MethodPatch, path: "/api/ai/tools/crm.getCustomer", body: `{}`, status: http.StatusBadRequest},
		{name: "clear false", method: http.MethodPatch, path: "/api/ai/tools/crm.getCustomer", body: `{"clearOverride":false}`, status: http.StatusBadRequest},
		{name: "wrong method", method: http.MethodGet, path: "/api/ai/tools/crm.getCustomer", body: "", status: http.StatusMethodNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			setAIIdentityHeaders(req)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status = %d, want %d (body=%s)", res.Code, tc.status, res.Body.String())
			}
		})
	}
}

func TestUpdateToolEndpointWithoutGovernance(t *testing.T) {
	router := NewRouterWithOptions(nil, nil, governanceTestOptions())
	req := httptest.NewRequest(http.MethodPatch, "/api/ai/tools/crm.getCustomer", strings.NewReader(`{"enabled":false}`))
	setAIIdentityHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), "ai.tool_persistence_unavailable") {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}
