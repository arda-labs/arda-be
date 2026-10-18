package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

const caseVariablesPIK = 2251799829066558

func caseVariablesRequest(caseID string) (*http.Request, *httptest.ResponseRecorder) {
	r := workflowTenantRequest("GET", "/api/workflow/cases/"+caseID+"/variables", "tenant-1")
	return r, httptest.NewRecorder()
}

func variablesESResponse() string {
	return `{"hits":{"hits":[
		{"_source":{"position":10,"intent":"CREATED","valueType":"VARIABLE",
			"value":{"name":"caseCode","value":"\"DPM-20261018-000001\"","scopeKey":2251799829066558,"processInstanceKey":2251799829066558}}},
		{"_source":{"position":11,"intent":"CREATED","valueType":"VARIABLE",
			"value":{"name":"mcKind","value":"\"dpm.product-request\"","scopeKey":2251799829066558,"processInstanceKey":2251799829066558}}},
		{"_source":{"position":12,"intent":"UPDATED","valueType":"VARIABLE",
			"value":{"name":"mcKind","value":"\"dpm.product-request.v2\"","scopeKey":2251799829066558,"processInstanceKey":2251799829066558}}}
	]}}`
}

// The Zeebe 8.5 gateway has no /v2 REST API, so case variables come from the
// exporter read model and keep the CaseVariables response shape.
func TestCaseVariablesReadsFromExporterIndex(t *testing.T) {
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(variablesESResponse()))
	}))
	defer es.Close()
	h := &WorkflowHandler{
		caseScopeOverride: ownedCaseScope("case-1", caseVariablesPIK),
		MonitoringIndex:   service.NewZeebeMonitoringIndex(es.URL),
	}

	r, w := caseVariablesRequest("case-1")
	h.caseVariables(w, r, "case-1")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var envelope struct {
		Result CaseVariables `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	got := envelope.Result
	if got.CaseID != "case-1" || got.ProcessInstanceKey != "2251799829066558" {
		t.Fatalf("envelope = %+v", got)
	}
	if got.Variables["caseCode"] != "DPM-20261018-000001" || got.Variables["mcKind"] != "dpm.product-request.v2" {
		t.Fatalf("variables = %#v", got.Variables)
	}
}

func TestCaseVariablesEmptyWhenExporterHasNotIndexedYet(t *testing.T) {
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"hits":{"hits":[]}}`))
	}))
	defer es.Close()
	h := &WorkflowHandler{
		caseScopeOverride: ownedCaseScope("case-1", caseVariablesPIK),
		MonitoringIndex:   service.NewZeebeMonitoringIndex(es.URL),
	}
	r, w := caseVariablesRequest("case-1")
	h.caseVariables(w, r, "case-1")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"variables":{}`) {
		t.Fatalf("status = %d body = %s, want 200 with variables {}", w.Code, w.Body.String())
	}
}

func TestCaseVariablesElasticsearchDownIs503WithMessage(t *testing.T) {
	es := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "es red", http.StatusInternalServerError)
	}))
	defer es.Close()
	h := &WorkflowHandler{
		caseScopeOverride: ownedCaseScope("case-1", caseVariablesPIK),
		MonitoringIndex:   service.NewZeebeMonitoringIndex(es.URL),
	}
	r, w := caseVariablesRequest("case-1")
	h.caseVariables(w, r, "case-1")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Elasticsearch") {
		t.Fatalf("503 body should explain the read model is unavailable: %s", w.Body.String())
	}
}

func TestCaseVariablesWithoutIndexIs503(t *testing.T) {
	h := &WorkflowHandler{caseScopeOverride: ownedCaseScope("case-1", caseVariablesPIK)}
	r, w := caseVariablesRequest("case-1")
	h.caseVariables(w, r, "case-1")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "ZEEBE_ES_URL") {
		t.Fatalf("status = %d body = %s, want 503 mentioning ZEEBE_ES_URL", w.Code, w.Body.String())
	}
}

func TestCaseVariablesCaseStates(t *testing.T) {
	scope := ownedCaseScope("case-1", caseVariablesPIK)
	scope.cases["case-2"] = &repository.BusinessCase{ID: "case-2", TenantID: "tenant-1"}
	h := &WorkflowHandler{
		caseScopeOverride: scope,
		MonitoringIndex:   service.NewZeebeMonitoringIndex("http://127.0.0.1:1"),
	}

	r, w := caseVariablesRequest("case-9")
	h.caseVariables(w, r, "case-9")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown case status = %d, want 404", w.Code)
	}

	r, w = caseVariablesRequest("case-2")
	h.caseVariables(w, r, "case-2")
	if w.Code != http.StatusConflict {
		t.Fatalf("case without process instance status = %d, want 409", w.Code)
	}
}
