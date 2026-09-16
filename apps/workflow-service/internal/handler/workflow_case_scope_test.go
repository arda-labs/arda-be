package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// fakeWorkflowCaseScope is a tenant-scoped registry stub: only the keys seeded
// for the verified tenant resolve, everything else behaves like another
// tenant's key (nil case).
type fakeWorkflowCaseScope struct {
	cases     map[string]*repository.BusinessCase
	workItems map[int64]*repository.WorkItem
	status    []string
	err       error
}

func (f *fakeWorkflowCaseScope) GetCase(_ context.Context, id string) (*repository.BusinessCase, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.cases[id], nil
}

func (f *fakeWorkflowCaseScope) GetCaseByProcessInstanceKey(_ context.Context, key int64) (*repository.BusinessCase, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.cases[strconv.FormatInt(key, 10)], nil
}

func (f *fakeWorkflowCaseScope) FindWorkItemByJobKey(_ context.Context, jobKey int64) (*repository.WorkItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.workItems[jobKey], nil
}

func (f *fakeWorkflowCaseScope) SetCaseStatusByProcessKey(_ context.Context, key int64, status string) error {
	if f.err != nil {
		return f.err
	}
	if f.cases[strconv.FormatInt(key, 10)] == nil {
		return repository.ErrNotFound
	}
	f.status = append(f.status, fmt.Sprintf("%d:%s", key, status))
	return nil
}

func ownedCaseScope(caseID string, processInstanceKey int64) *fakeWorkflowCaseScope {
	scope := &fakeWorkflowCaseScope{cases: map[string]*repository.BusinessCase{}}
	scope.cases[strconv.FormatInt(processInstanceKey, 10)] = &repository.BusinessCase{
		ID:                 caseID,
		TenantID:           "tenant-1",
		Status:             "IN_REVIEW",
		ProcessInstanceKey: &processInstanceKey,
	}
	scope.cases[caseID] = scope.cases[strconv.FormatInt(processInstanceKey, 10)]
	return scope
}

// TestOperateInstanceActionsRequireTenantOwnedCase proves pause/resume/cancel
// resolve the process instance to a caller-tenant case before mutating: a key
// that only exists for another tenant is rejected with 404 and never reaches
// the case update (or Zeebe, for cancel).
func TestOperateInstanceActionsRequireTenantOwnedCase(t *testing.T) {
	scope := ownedCaseScope("case-1", 100)
	h := &WorkflowHandler{caseScopeOverride: scope}

	pause := func(key int) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.OperatePauseInstance(w, workflowTenantRequest("POST", "/api/workflow/operate/process-instances/"+strconv.Itoa(key)+"/pause", "tenant-1"))
		return w
	}
	if w := pause(100); w.Code != http.StatusOK {
		t.Fatalf("owned pause status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if len(scope.status) != 1 || scope.status[0] != "100:SUSPENDED" {
		t.Fatalf("owned pause updates = %v, want [100:SUSPENDED]", scope.status)
	}
	if w := pause(200); w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant pause status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	if len(scope.status) != 1 {
		t.Fatalf("cross-tenant pause mutated the registry: %v", scope.status)
	}

	check := func(name string, handler func(http.ResponseWriter, *http.Request), key int, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		handler(w, workflowTenantRequest("POST", "/api/workflow/operate/process-instances/"+strconv.Itoa(key)+"/"+name, "tenant-1"))
		if w.Code != want {
			t.Fatalf("%s(%d) status = %d, want %d (body %s)", name, key, w.Code, want, w.Body.String())
		}
	}
	check("resume", h.OperateResumeInstance, 200, http.StatusNotFound)
	check("cancel", h.OperateCancelInstance, 200, http.StatusNotFound)
	if w := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.OperateResumeInstance(w, workflowTenantRequest("POST", "/api/workflow/operate/process-instances/100/resume", "tenant-1"))
		return w
	}(); w.Code != http.StatusOK {
		t.Fatalf("owned resume status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if w := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.OperateCancelInstance(w, workflowTenantRequest("POST", "/api/workflow/operate/process-instances/100/cancel", "tenant-1"))
		return w
	}(); w.Code != http.StatusOK {
		t.Fatalf("owned cancel status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if len(scope.status) != 3 || scope.status[1] != "100:ACTIVE" || scope.status[2] != "100:CANCELLED" {
		t.Fatalf("owned mutations = %v, want resume + cancel on the caller tenant", scope.status)
	}
}

// TestOperateInstancePauseSurfacesRepositoryNotFound covers the rows-affected
// conflict: the tenant-scoped update can still report ErrNotFound (case moved
// or deleted between resolve and update) and must not return 200.
func TestOperateInstancePauseSurfacesRepositoryNotFound(t *testing.T) {
	scope := ownedCaseScope("case-1", 100)
	scope.err = repository.ErrNotFound
	h := &WorkflowHandler{caseScopeOverride: scope}

	w := httptest.NewRecorder()
	h.OperatePauseInstance(w, workflowTenantRequest("POST", "/api/workflow/operate/process-instances/100/pause", "tenant-1"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when the case update matches no tenant row", w.Code)
	}
}

// TestRetryJobRequiresTenantOwnedCase proves the generic job retry authorizes
// jobKey through the tenant-scoped registry: an unmapped job is 404 (fail
// closed) while a mapped one proceeds to the Zeebe step (503 here because the
// gateway is not wired in the test).
func TestRetryJobRequiresTenantOwnedCase(t *testing.T) {
	scope := ownedCaseScope("case-1", 100)
	scope.workItems = map[int64]*repository.WorkItem{55: {CaseID: "case-1"}}
	h := &WorkflowHandler{caseScopeOverride: scope}

	retry := func(key int) int {
		t.Helper()
		w := httptest.NewRecorder()
		h.JobByKey(w, workflowTenantRequest("POST", "/api/workflow/jobs/"+strconv.Itoa(key)+"/retry", "tenant-1"))
		return w.Code
	}
	if code := retry(55); code != http.StatusServiceUnavailable {
		t.Fatalf("owned job retry status = %d, want 503 (Zeebe not wired)", code)
	}
	if code := retry(66); code != http.StatusNotFound {
		t.Fatalf("unmapped job retry status = %d, want 404", code)
	}

	update := func(key int) int {
		t.Helper()
		w := httptest.NewRecorder()
		h.OperateUpdateJobRetries(w, workflowTenantRequest("PUT", "/api/workflow/operate/jobs/"+strconv.Itoa(key)+"/retries", "tenant-1"))
		return w.Code
	}
	if code := update(55); code != http.StatusServiceUnavailable {
		t.Fatalf("owned job retries update status = %d, want 503 (Zeebe not wired)", code)
	}
	if code := update(66); code != http.StatusNotFound {
		t.Fatalf("unmapped job retries update status = %d, want 404", code)
	}
}

// TestOperateIncidentRetryRequiresTenantOwnedKey covers the degraded path
// (no exporter read model): the legacy inc-<key> action still has to map to a
// caller-tenant case before Zeebe is touched.
func TestOperateIncidentRetryRequiresTenantOwnedKey(t *testing.T) {
	scope := ownedCaseScope("case-1", 100)
	scope.workItems = map[int64]*repository.WorkItem{55: {CaseID: "case-1"}}
	h := &WorkflowHandler{caseScopeOverride: scope}

	retry := func(key int) int {
		t.Helper()
		w := httptest.NewRecorder()
		h.OperateRetryIncident(w, workflowTenantRequest("POST", "/api/workflow/operate/incidents/"+strconv.Itoa(key)+"/retry", "tenant-1"))
		return w.Code
	}
	if code := retry(55); code != http.StatusServiceUnavailable {
		t.Fatalf("owned incident retry status = %d, want 503 (Zeebe not wired)", code)
	}
	if code := retry(66); code != http.StatusNotFound {
		t.Fatalf("unmapped incident retry status = %d, want 404", code)
	}
}

// TestResolveCaseIncidentVerifiesTenantAndIncidentScope proves the path caseId
// is enforced (cross-tenant case → 404), that resolution fails closed without
// an incident read model, and that an incident of another process instance is
// rejected even when the case itself is owned.
func TestResolveCaseIncidentVerifiesTenantAndIncidentScope(t *testing.T) {
	scope := ownedCaseScope("case-1", 100)
	h := &WorkflowHandler{
		caseScopeOverride: scope,
		zeebeRest:         service.NewZeebeRestClient("http://127.0.0.1:1", "", nil),
	}

	resolve := func(caseID, incidentKey string) *httptest.ResponseRecorder {
		t.Helper()
		r := workflowTenantRequest("POST", "/api/workflow/cases/"+caseID+"/incidents/"+incidentKey+"/resolve", "tenant-1")
		r.SetPathValue("id", caseID)
		r.SetPathValue("incidentKey", incidentKey)
		w := httptest.NewRecorder()
		h.ResolveCaseIncident(w, r)
		return w
	}

	if w := resolve("case-9", "900"); w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant case status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	// No incident read model configured: the membership of the incident can
	// not be proven, so the resolve must fail closed instead of reaching Zeebe.
	if w := resolve("case-1", "900"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing incident read model status = %d, want 503 (body %s)", w.Code, w.Body.String())
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":{"hits":[
			{"_source":{"key":900,"timestamp":1000,"intent":"CREATED","valueType":"INCIDENT",
				"value":{"processInstanceKey":100,"elementId":"Task_A","jobKey":55,"errorType":"JOB_NO_RETRIES"}}}
		]}}`))
	}))
	defer server.Close()
	h.IncidentIndex = service.NewZeebeIncidentIndex(server.URL)

	// Incident 999 does not belong to case-1's process instance.
	if w := resolve("case-1", "999"); w.Code != http.StatusNotFound {
		t.Fatalf("foreign incident status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	// Incident 900 belongs to the case: the handler proceeds to Zeebe, which is
	// unreachable, so the upstream failure (502) proves authorization passed.
	if w := resolve("case-1", "900"); w.Code != http.StatusBadGateway {
		t.Fatalf("owned incident status = %d, want 502 from the unreachable Zeebe gateway (body %s)", w.Code, w.Body.String())
	}
}

// TestCaseScopeHelpersFailClosed pins the resolver contract: a missing
// registry is an error (never an implicit allow) and literal-zero keys resolve
// to no case.
func TestCaseScopeHelpersFailClosed(t *testing.T) {
	h := &WorkflowHandler{}
	if _, err := h.caseForProcessInstanceKey(context.Background(), 100); !errors.Is(err, errWorkflowScopeUnavailable) {
		t.Fatalf("missing registry err = %v, want errWorkflowScopeUnavailable", err)
	}
	if _, err := h.caseForCaseID(context.Background(), "case-1"); !errors.Is(err, errWorkflowScopeUnavailable) {
		t.Fatalf("missing registry err = %v, want errWorkflowScopeUnavailable", err)
	}
	if bc, err := h.caseForProcessInstanceKey(context.Background(), 0); err != nil || bc != nil {
		t.Fatalf("zero key = (%v, %v), want (nil, nil)", bc, err)
	}
	if bc, err := h.caseForJobKey(context.Background(), 0); err != nil || bc != nil {
		t.Fatalf("zero job key = (%v, %v), want (nil, nil)", bc, err)
	}
}

type fakeAssignmentSource struct {
	rules     *repository.WorkflowAssignmentRule
	tenantIDs []string
}

func (f *fakeAssignmentSource) FindAssignmentRule(_ context.Context, _, _ string) (*repository.WorkflowAssignmentRule, error) {
	return f.rules, nil
}

func (f *fakeAssignmentSource) ListActiveMembershipUsers(_ context.Context, tenantID, _ string) ([]string, error) {
	f.tenantIDs = append(f.tenantIDs, tenantID)
	return []string{"user-1", "user-2"}, nil
}

func (f *fakeAssignmentSource) ListActiveDelegationTargets(_ context.Context, _ string, _ string, _ []string) ([]string, error) {
	return nil, nil
}

// TestResolveAssignmentRulesUsesVerifiedTenant proves the admin dry-run reads
// the tenant from the verified scope (a different tenant_id query param is
// rejected) and the actor from the verified user header, not the query string.
func TestResolveAssignmentRulesUsesVerifiedTenant(t *testing.T) {
	source := &fakeAssignmentSource{rules: &repository.WorkflowAssignmentRule{
		RoleCode:                  "CHECKER",
		AssignmentMode:            "ANY",
		RequireSeparationOfDuties: true,
	}}
	h := &WorkflowHandler{AssignmentResolver: service.NewAssignmentResolver(source)}

	base := "/api/workflow/assignment-rules/resolve?case_type=CRM&step_code=REVIEW"
	// No verified tenant scope at all → 403.
	w := httptest.NewRecorder()
	h.ResolveAssignmentRules(w, httptest.NewRequest("GET", base, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing tenant status = %d, want 403", w.Code)
	}
	// Another tenant in the query param → 403.
	w = httptest.NewRecorder()
	h.ResolveAssignmentRules(w, workflowTenantRequest("GET", base+"&tenant_id=tenant-2", "tenant-1"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant query status = %d, want 403", w.Code)
	}
	if len(source.tenantIDs) != 0 {
		t.Fatalf("memberships were queried for rejected requests: %v", source.tenantIDs)
	}

	// Verified tenant wins; created_by stays the verified user, so the maker is
	// filtered out of the checker pool by separation of duties.
	r := workflowTenantRequest("GET", base+"&tenant_id=tenant-1&created_by=user-9", "tenant-1")
	r.Header.Set("X-User-Id", "user-1")
	w = httptest.NewRecorder()
	h.ResolveAssignmentRules(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("dry-run status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if len(source.tenantIDs) != 1 || source.tenantIDs[0] != "tenant-1" {
		t.Fatalf("membership tenant = %v, want [tenant-1]", source.tenantIDs)
	}
	var envelope struct {
		Result struct {
			CandidateUsers []string `json:"CandidateUsers"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode dry-run response: %v (body %s)", err, w.Body.String())
	}
	for _, user := range envelope.Result.CandidateUsers {
		if user == "user-1" {
			t.Fatalf("maker leaked into the checker pool: %v", envelope.Result.CandidateUsers)
		}
	}
	if len(envelope.Result.CandidateUsers) != 1 || envelope.Result.CandidateUsers[0] != "user-2" {
		t.Fatalf("candidate users = %v, want [user-2]", envelope.Result.CandidateUsers)
	}
}
