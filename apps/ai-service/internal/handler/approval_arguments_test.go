package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// argCapturingConfirmTool records the exact payload the execution path hands to
// the tool so tests can prove it is the approved original, not a
// sanitized/truncated copy.
type argCapturingConfirmTool struct {
	executed bool
	args     json.RawMessage
}

func (t *argCapturingConfirmTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "test.confirm", Version: 1, Kind: "confirm",
		RequiredPermissions: []string{"crm.customer.read"}, Risk: "medium",
	}
}

func (t *argCapturingConfirmTool) Execute(_ context.Context, _ tools.Context, arguments json.RawMessage) (tools.Result, error) {
	t.executed = true
	t.args = append(json.RawMessage(nil), arguments...)
	return tools.Result{Data: json.RawMessage(`{"prepared":true}`), Summary: "Đã chuẩn bị."}, nil
}

// hugeApprovalArguments exceeds the 16 KiB transcript bound and carries a
// bearer-looking secret, exercising both lossy transforms at once.
func hugeApprovalArguments() string {
	return `{"customerId":"c1","note":"` + strings.Repeat("x", 20*1024) + `","authorization":"Bearer live-secret-token"}`
}

// TestExecuteApprovedToolUsesStoredOriginalArguments locks task #1: a proposal
// created with >16 KiB arguments or a bearer-looking secret must execute the
// exact original payload, never the sanitized/truncated display copy.
func TestExecuteApprovedToolUsesStoredOriginalArguments(t *testing.T) {
	original := hugeApprovalArguments()
	store := &executionTestStore{arguments: original}
	tool := &argCapturingConfirmTool{}
	router := NewRouterWithOptions(store, tools.NewRegistry(tool), RouterOptions{EnableHITLProposals: true})

	req := httptest.NewRequest(http.MethodPost, "/api/ai/approvals/a1/execution", nil)
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if !tool.executed {
		t.Fatal("tool was not executed")
	}
	if string(tool.args) != original {
		t.Fatalf("executed arguments differ from the approved original:\ngot  %s\nwant %s", tool.args, original)
	}
	if strings.Contains(string(tool.args), "[REDACTED]") {
		t.Fatal("execution must not receive the redacted copy")
	}
}

// TestRedactArgumentsJSONAlwaysValidAndBounded proves the audit copy stays
// JSON-safe even when the original exceeds the transcript limit — the old raw
// truncation produced invalid JSON and broke the JSONB insert.
func TestRedactArgumentsJSONAlwaysValidAndBounded(t *testing.T) {
	redacted := redactArgumentsJSON(`{"authorization":"Bearer super-secret","customerId":"c1"}`)
	if !json.Valid([]byte(redacted)) {
		t.Fatalf("redacted arguments are not valid JSON: %s", redacted)
	}
	if strings.Contains(redacted, "super-secret") || strings.Contains(strings.ToLower(redacted), "bearer") {
		t.Fatalf("secret was not redacted: %s", redacted)
	}

	bounded := redactArgumentsJSON(hugeApprovalArguments())
	if !json.Valid([]byte(bounded)) {
		t.Fatalf("bounded redaction is not valid JSON: %s", bounded)
	}
	if len(bounded) > 16*1024 {
		t.Fatalf("bounded redaction grew to %d bytes", len(bounded))
	}
	var marker map[string]any
	if err := json.Unmarshal([]byte(bounded), &marker); err != nil || marker["truncated"] != true {
		t.Fatalf("expected truncation marker, got %s", bounded)
	}
}

// TestApprovalProposalPersistsOriginalAndPermissionVersion checks the
// FE-initiated proposal path stores both the original payload and the auth
// snapshot while keeping the redacted copy display-safe.
func TestApprovalProposalPersistsOriginalAndPermissionVersion(t *testing.T) {
	original := hugeApprovalArguments()
	store := &fakeToolRunStore{}
	options := RouterOptions{
		EnableHITLProposals: true,
		ProposalTools: []ProposalToolSpec{{
			Name: "crm.exportCustomer", Version: 1, Risk: "medium", RequiredPermission: "crm.customer.manage",
		}},
	}
	body := fmt.Sprintf(
		`{"threadId":"t-approval","runId":"r-approval","idempotencyKey":"idem-1","tool":{"name":"crm.exportCustomer","version":1,"arguments":%s}}`,
		original,
	)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/approvals", strings.NewReader(body))
	for key, value := range map[string]string{
		"X-Auth-Checked": "true", "X-User-Id": "user-1", "X-Tenant-Id": "tenant-1",
		"X-Auth-Version": "7",
		"X-Permissions":  "ai.assistant.use,ai.approval.propose,crm.customer.manage",
	} {
		req.Header.Set(key, value)
	}
	res := httptest.NewRecorder()
	NewRouterWithOptions(store, nil, options).ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("proposal status = %d: %s", res.Code, res.Body.String())
	}
	if store.approvalProposal.Arguments != original {
		t.Fatalf("original arguments not persisted verbatim:\ngot  %s\nwant %s", store.approvalProposal.Arguments, original)
	}
	if store.approvalProposal.PermissionVersion != "7" {
		t.Fatalf("permission version = %q, want 7", store.approvalProposal.PermissionVersion)
	}
	if !json.Valid([]byte(store.approvalProposal.ArgumentsRedacted)) {
		t.Fatalf("redacted copy is not valid JSON: %s", store.approvalProposal.ArgumentsRedacted)
	}
	if strings.Contains(store.approvalProposal.ArgumentsRedacted, "live-secret-token") {
		t.Fatalf("redacted copy leaked the secret: %s", store.approvalProposal.ArgumentsRedacted)
	}
}

// TestExecuteApprovedToolRejectsStalePermissionVersion locks task #2: when the
// caller's X-Auth-Version moved past the recorded permission_version the tool
// must not run and the execution must be finished as FAILED/ai.approval_stale.
func TestExecuteApprovedToolRejectsStalePermissionVersion(t *testing.T) {
	store := &executionTestStore{permissionVersion: "7"}
	tool := &argCapturingConfirmTool{}
	router := NewRouterWithOptions(store, tools.NewRegistry(tool), RouterOptions{EnableHITLProposals: true})

	req := httptest.NewRequest(http.MethodPost, "/api/ai/approvals/a1/execution", nil)
	gatewayHeaders(req)
	req.Header.Set("X-Auth-Version", "8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "ai.approval_stale") {
		t.Fatalf("stale approval response = %d/%s", res.Code, res.Body.String())
	}
	if tool.executed {
		t.Fatal("stale approval must not execute the tool")
	}
	if store.lastToolStatus != "FAILED" || store.lastToolErrorCode != "ai.approval_stale" {
		t.Fatalf("execution terminal state = %s/%s, want FAILED/ai.approval_stale", store.lastToolStatus, store.lastToolErrorCode)
	}
}

func TestApprovalPermissionFresh(t *testing.T) {
	fresh := repository.ApprovedExecution{PermissionVersion: "7"}
	cases := []struct {
		name  string
		exec  repository.ApprovedExecution
		scope tools.Context
		want  bool
	}{
		{"matching version", fresh, tools.Context{AuthVersion: "7"}, true},
		{"changed version", fresh, tools.Context{AuthVersion: "8"}, false},
		{"missing current version", fresh, tools.Context{}, false},
		{"legacy without snapshot", repository.ApprovedExecution{}, tools.Context{AuthVersion: "8"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := approvalPermissionFresh(tc.exec, tc.scope); got != tc.want {
				t.Fatalf("approvalPermissionFresh = %v, want %v", got, tc.want)
			}
		})
	}
}
