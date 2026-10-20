package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeSetVariablesCall struct {
	scope     int64
	variables map[string]any
	local     bool
}

// fakeOperateEngine counts the Zeebe mutations the Operate handlers send.
type fakeOperateEngine struct {
	setCalls  []fakeSetVariablesCall
	cancelled []int64
	setErr    error
	cancelErr error
}

func (f *fakeOperateEngine) SetVariables(_ context.Context, scope int64, variables map[string]any, local bool) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.setCalls = append(f.setCalls, fakeSetVariablesCall{scope: scope, variables: variables, local: local})
	return nil
}

func (f *fakeOperateEngine) CancelWorkflow(_ context.Context, key int64) error {
	if f.cancelErr != nil {
		return f.cancelErr
	}
	f.cancelled = append(f.cancelled, key)
	return nil
}

// fakeOperateReadModel maps process instance → element instance keys and
// serves the current variable values.
type fakeOperateReadModel struct {
	elements  map[int64][]string
	variables map[string]any
}

func (f *fakeOperateReadModel) ListElementInstances(_ context.Context, key int64) ([]service.ZeebeElementInstance, error) {
	var out []service.ZeebeElementInstance
	for _, element := range f.elements[key] {
		out = append(out, service.ZeebeElementInstance{ElementInstanceKey: element})
	}
	return out, nil
}

func (f *fakeOperateReadModel) ProcessVariables(context.Context, int64) (map[string]any, error) {
	return f.variables, nil
}

// operateRequest is a tenant-1 Operate request from the verified operator op-1.
func operateRequest(target, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("X-User-Id", "op-1")
	return r.WithContext(ardametadata.AppendToOutgoing(r.Context(), ardametadata.Context{
		TenantID:    "tenant-1",
		AuthChecked: "true",
	}))
}

func newOperateHandler(scope *fakeWorkflowCaseScope, engine *fakeOperateEngine, model operateReadModel) *WorkflowHandler {
	h := &WorkflowHandler{caseScopeOverride: scope, operateEngineOverride: engine}
	if model != nil {
		h.operateReadModelOverride = model
	}
	return h
}

const setVariablesPath = "/api/workflow/operate/process-instances/100/variables"

// TestOperateSetVariablesScopesElementInstanceToProcess: instance 100 thuộc
// tenant-1; phần tử 900 thuộc instance của tenant khác (mọi tenant Arda dùng
// chung một tenant Zeebe). elementInstanceKey lạ bị 404 và không lệnh
// SetVariables nào tới Zeebe; phần tử của chính instance thì ghi đúng scope.
func TestOperateSetVariablesScopesElementInstanceToProcess(t *testing.T) {
	scope := ownedCaseScope("case-1", 100)
	engine := &fakeOperateEngine{}
	model := &fakeOperateReadModel{elements: map[int64][]string{100: {"100", "101"}, 200: {"900"}}}
	h := newOperateHandler(scope, engine, model)

	w := httptest.NewRecorder()
	h.OperateSetInstanceVariables(w, operateRequest(setVariablesPath, `{"variables":{"note":"x"},"elementInstanceKey":900,"reason":"sửa sự cố"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("foreign element instance status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	if len(engine.setCalls) != 0 || len(scope.audits) != 0 {
		t.Fatalf("foreign element instance reached Zeebe/audit: calls=%v audits=%v", engine.setCalls, scope.audits)
	}

	w = httptest.NewRecorder()
	h.OperateSetInstanceVariables(w, operateRequest(setVariablesPath, `{"variables":{"note":"x"},"elementInstanceKey":101,"reason":"sửa sự cố"}`))
	if w.Code != http.StatusOK || len(engine.setCalls) != 1 || engine.setCalls[0].scope != 101 {
		t.Fatalf("own element instance = %d calls=%v, want 200 on scope 101 (body %s)", w.Code, engine.setCalls, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.OperateSetInstanceVariables(w, operateRequest(setVariablesPath, `{"variables":{"note":"y"},"reason":"sửa sự cố"}`))
	if w.Code != http.StatusOK || len(engine.setCalls) != 2 || engine.setCalls[1].scope != 100 {
		t.Fatalf("root scope = %d calls=%v, want 200 on scope 100 (body %s)", w.Code, engine.setCalls, w.Body.String())
	}

	// Không có read model thì không chứng minh được quan hệ phần tử-instance:
	// từ chối thay vì gửi khóa của client sang Zeebe.
	noModel := newOperateHandler(ownedCaseScope("case-1", 100), &fakeOperateEngine{}, nil)
	w = httptest.NewRecorder()
	noModel.OperateSetInstanceVariables(w, operateRequest(setVariablesPath, `{"variables":{"note":"x"},"elementInstanceKey":900,"reason":"sửa sự cố"}`))
	if w.Code != http.StatusServiceUnavailable || len(noModel.operateEngineOverride.(*fakeOperateEngine).setCalls) != 0 {
		t.Fatalf("unverifiable element instance status = %d, want 503 without a Zeebe call", w.Code)
	}

	// Instance của tenant khác: 404 trước mọi thao tác.
	w = httptest.NewRecorder()
	h.OperateSetInstanceVariables(w, operateRequest("/api/workflow/operate/process-instances/200/variables", `{"variables":{"note":"x"},"reason":"sửa sự cố"}`))
	if w.Code != http.StatusNotFound || len(engine.setCalls) != 2 {
		t.Fatalf("cross-tenant instance status = %d calls=%d, want 404 and no new call", w.Code, len(engine.setCalls))
	}
}

// TestOperateSetVariablesRejectsProtectedKeys: khóa dành riêng và biến định
// tuyến/số tiền bị 400 nêu tên khóa, không khóa nào sang Zeebe.
func TestOperateSetVariablesRejectsProtectedKeys(t *testing.T) {
	for _, key := range []string{"tenantId", "caseId", "mcMakerRole", "mcCheckerRole", "actorUserId", "primaryObjectId", "amount", "pgdLimit", "gdLimit", "fundSource", "contractId", "approvalLevel"} {
		scope := ownedCaseScope("case-1", 100)
		engine := &fakeOperateEngine{}
		h := newOperateHandler(scope, engine, &fakeOperateReadModel{})
		w := httptest.NewRecorder()
		body := fmt.Sprintf(`{"variables":{%q:"x","note":"ok"},"reason":"sửa sự cố"}`, key)
		h.OperateSetInstanceVariables(w, operateRequest(setVariablesPath, body))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), key) {
			t.Fatalf("%s: status = %d body %s, want 400 naming the key", key, w.Code, w.Body.String())
		}
		if len(engine.setCalls) != 0 || len(scope.audits) != 0 {
			t.Fatalf("%s: protected key reached Zeebe/audit", key)
		}
	}
}

// TestOperateSetVariablesAuditsActorReasonBeforeAfter: lý do rỗng bị 400; mỗi
// lần ghi thành công có đúng một dòng timeline với actor, tên biến, trước/sau,
// lý do; không ghi được audit thì Zeebe không bị đụng.
func TestOperateSetVariablesAuditsActorReasonBeforeAfter(t *testing.T) {
	scope := ownedCaseScope("case-1", 100)
	engine := &fakeOperateEngine{}
	h := newOperateHandler(scope, engine, &fakeOperateReadModel{variables: map[string]any{"note": "old", "other": "z"}})

	w := httptest.NewRecorder()
	h.OperateSetInstanceVariables(w, operateRequest(setVariablesPath, `{"variables":{"note":"new"},"reason":"  "}`))
	if w.Code != http.StatusBadRequest || len(engine.setCalls) != 0 || len(scope.audits) != 0 {
		t.Fatalf("blank reason = %d calls=%d audits=%d, want 400 with no side effect", w.Code, len(engine.setCalls), len(scope.audits))
	}

	w = httptest.NewRecorder()
	unauth := operateRequest(setVariablesPath, `{"variables":{"note":"new"},"reason":"sửa sự cố"}`)
	unauth.Header.Del("X-User-Id")
	h.OperateSetInstanceVariables(w, unauth)
	if w.Code != http.StatusUnauthorized || len(engine.setCalls) != 0 {
		t.Fatalf("missing actor = %d, want 401 with no Zeebe call", w.Code)
	}

	w = httptest.NewRecorder()
	h.OperateSetInstanceVariables(w, operateRequest(setVariablesPath, `{"variables":{"note":"new"},"reason":"sửa sự cố INC-7"}`))
	if w.Code != http.StatusOK || len(engine.setCalls) != 1 {
		t.Fatalf("set variables = %d calls=%d, want 200 with one Zeebe call (body %s)", w.Code, len(engine.setCalls), w.Body.String())
	}
	if len(scope.audits) != 1 {
		t.Fatalf("audits = %v, want exactly one", scope.audits)
	}
	audit := scope.audits[0]
	if audit.CaseID != "case-1" || audit.EventType != operateEventVariablesSet || audit.Actor != "op-1" || audit.Reason != "sửa sự cố INC-7" {
		t.Fatalf("audit = %+v, want case-1/OPERATE_VARIABLES_SET/op-1/reason", audit)
	}
	if !reflect.DeepEqual(audit.Data["variables"], []string{"note"}) ||
		!reflect.DeepEqual(audit.Data["before"], map[string]any{"note": "old"}) ||
		!reflect.DeepEqual(audit.Data["after"], map[string]any{"note": "new"}) {
		t.Fatalf("audit data = %v, want names + before/after of note", audit.Data)
	}

	failing := ownedCaseScope("case-1", 100)
	failing.auditErr = errors.New("db down")
	failEngine := &fakeOperateEngine{}
	fh := newOperateHandler(failing, failEngine, &fakeOperateReadModel{})
	w = httptest.NewRecorder()
	fh.OperateSetInstanceVariables(w, operateRequest(setVariablesPath, `{"variables":{"note":"new"},"reason":"sửa sự cố"}`))
	if w.Code != http.StatusInternalServerError || len(failEngine.setCalls) != 0 {
		t.Fatalf("audit failure = %d calls=%d, want 500 with no Zeebe call", w.Code, len(failEngine.setCalls))
	}

	errEngine := &fakeOperateEngine{setErr: errors.New("zeebe unavailable")}
	eScope := ownedCaseScope("case-1", 100)
	eh := newOperateHandler(eScope, errEngine, &fakeOperateReadModel{})
	w = httptest.NewRecorder()
	eh.OperateSetInstanceVariables(w, operateRequest(setVariablesPath, `{"variables":{"note":"new"},"reason":"sửa sự cố"}`))
	if w.Code != http.StatusBadGateway || len(eScope.audits) != 2 || eScope.audits[1].EventType != operateEventVariablesSetFailed {
		t.Fatalf("engine failure = %d audits=%v, want 502 with SET + SET_FAILED rows", w.Code, eScope.audits)
	}
}

const cancelPath = "/api/workflow/operate/process-instances/100/cancel"

// TestOperateCancelRequiresOpenCaseReasonAndEngineSuccess: hồ sơ COMPLETED bị
// 409 và không đổi; thiếu lý do 400; Zeebe hủy lỗi thì 502 và hồ sơ vẫn mở;
// hồ sơ mở kèm lý do thì CANCELLED với một dòng timeline (actor, lý do).
func TestOperateCancelRequiresOpenCaseReasonAndEngineSuccess(t *testing.T) {
	completed := ownedCaseScope("case-1", 100)
	completed.cases["case-1"].Status = repository.CaseStatusCompleted
	engine := &fakeOperateEngine{}
	h := newOperateHandler(completed, engine, nil)
	w := httptest.NewRecorder()
	h.OperateCancelInstance(w, operateRequest(cancelPath, `{"reason":"hủy"}`))
	if w.Code != http.StatusConflict || completed.cases["case-1"].Status != repository.CaseStatusCompleted || len(engine.cancelled) != 0 || len(completed.status) != 0 {
		t.Fatalf("completed cancel = %d status=%s engine=%v, want 409 and unchanged", w.Code, completed.cases["case-1"].Status, engine.cancelled)
	}

	open := ownedCaseScope("case-1", 100)
	engine = &fakeOperateEngine{}
	h = newOperateHandler(open, engine, nil)
	w = httptest.NewRecorder()
	h.OperateCancelInstance(w, operateRequest(cancelPath, `{}`))
	if w.Code != http.StatusBadRequest || len(engine.cancelled) != 0 || len(open.status) != 0 {
		t.Fatalf("missing reason = %d, want 400 with no side effect", w.Code)
	}

	failEngine := &fakeOperateEngine{cancelErr: errors.New("gateway timeout")}
	h = newOperateHandler(open, failEngine, nil)
	w = httptest.NewRecorder()
	h.OperateCancelInstance(w, operateRequest(cancelPath, `{"reason":"hủy"}`))
	if w.Code != http.StatusBadGateway || open.cases["case-1"].Status != "IN_REVIEW" || len(open.status) != 0 {
		t.Fatalf("engine cancel failure = %d status=%s, want 502 and the case still open", w.Code, open.cases["case-1"].Status)
	}

	h = newOperateHandler(open, engine, nil)
	w = httptest.NewRecorder()
	h.OperateCancelInstance(w, operateRequest(cancelPath, `{"reason":"khách hàng rút hồ sơ"}`))
	if w.Code != http.StatusOK || open.cases["case-1"].Status != repository.CaseStatusCancelled || len(engine.cancelled) != 1 {
		t.Fatalf("open cancel = %d status=%s engine=%v, want 200 CANCELLED", w.Code, open.cases["case-1"].Status, engine.cancelled)
	}
	if len(open.audits) != 1 || open.audits[0].Actor != "op-1" || open.audits[0].Reason != "khách hàng rút hồ sơ" {
		t.Fatalf("cancel audits = %+v, want one row with actor and reason", open.audits)
	}

	// Instance đã không còn trong engine: chỉ đóng hồ sơ.
	gone := ownedCaseScope("case-1", 100)
	goneEngine := &fakeOperateEngine{cancelErr: fmt.Errorf("cancel workflow instance 100: %w", status.Error(codes.NotFound, "not found"))}
	h = newOperateHandler(gone, goneEngine, nil)
	w = httptest.NewRecorder()
	h.OperateCancelInstance(w, operateRequest(cancelPath, `{"reason":"instance đã mất"}`))
	if w.Code != http.StatusOK || gone.cases["case-1"].Status != repository.CaseStatusCancelled {
		t.Fatalf("engine NotFound cancel = %d status=%s, want 200 CANCELLED", w.Code, gone.cases["case-1"].Status)
	}
}
