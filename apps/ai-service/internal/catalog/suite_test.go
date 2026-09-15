package catalog

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type fakeApprovalSuiteStore struct {
	created *repository.ApprovalProposal
}

func (f *fakeApprovalSuiteStore) Start(context.Context, repository.RunContext, string) error {
	return nil
}

func (f *fakeApprovalSuiteStore) Finish(context.Context, repository.RunContext, string, string) error {
	return nil
}

func (f *fakeApprovalSuiteStore) CreateApprovalProposal(_ context.Context, proposal repository.ApprovalProposal) (repository.ApprovalRecord, error) {
	f.created = &proposal
	return repository.ApprovalRecord{ID: "approval-1", Status: "PENDING", ExpiresAt: proposal.ExpiresAt}, nil
}

func (f *fakeApprovalSuiteStore) DecideApproval(context.Context, string, string, string, string) (repository.ApprovalRecord, error) {
	return repository.ApprovalRecord{}, nil
}

func (f *fakeApprovalSuiteStore) ListApprovals(context.Context, string, string, int, int) ([]repository.ApprovalDetail, error) {
	return []repository.ApprovalDetail{}, nil
}

func codeModeScope() tools.Context {
	return tools.Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
		RequestID:   "req-1",
		Permissions: map[string]struct{}{
			"ai.assistant.use":    {},
			"crm.customer.manage": {},
		},
	}
}

// TestCodeModeConfirmToolCreatesApprovalProposal locks the full Code Mode
// chain: the sandbox refuses the dispatch, the suite persists a proposal, and
// the model receives a typed Result.Approval instead of a completed call.
func TestCodeModeConfirmToolCreatesApprovalProposal(t *testing.T) {
	store := &fakeApprovalSuiteStore{}
	suite := NewCodeModeSuite(ClientSet{}, store, true, nil, nil)

	result, err := suite.ExecuteTool.Execute(
		context.Background(),
		codeModeScope(),
		json.RawMessage(`{"code":"return await arda.crm.exportCustomer({ customerId: 'c1' })"}`),
	)
	if err != nil {
		t.Fatalf("execute meta-tool error: %v", err)
	}
	if store.created == nil {
		t.Fatal("approval proposal was not persisted")
	}
	if store.created.ToolName != "crm.exportCustomer" || store.created.Risk != "medium" {
		t.Fatalf("proposal = %+v, want crm.exportCustomer/medium", store.created)
	}
	if result.Approval == nil || result.Approval.ProposalID != "approval-1" || result.Approval.Tool != "crm.exportCustomer" {
		t.Fatalf("typed approval missing: %+v", result.Approval)
	}
	if strings.Contains(string(result.Data), "PREPARED") {
		t.Fatal("confirm-kind dispatcher must not execute inside Code Mode")
	}
}

// TestCodeModeConfirmToolFailsClosedWithoutHITL locks the fail-closed rule: no
// HITL means no proposal, no execution, and a clear model-facing refusal.
func TestCodeModeConfirmToolFailsClosedWithoutHITL(t *testing.T) {
	store := &fakeApprovalSuiteStore{}
	suite := NewCodeModeSuite(ClientSet{}, store, false, nil, nil)

	result, err := suite.ExecuteTool.Execute(
		context.Background(),
		codeModeScope(),
		json.RawMessage(`{"code":"return await arda.crm.exportCustomer({ customerId: 'c1' })"}`),
	)
	if err != nil {
		t.Fatalf("execute meta-tool error: %v", err)
	}
	if store.created != nil {
		t.Fatal("no proposal may be created when HITL is disabled")
	}
	if result.Approval != nil {
		t.Fatal("no typed approval may be returned when HITL is disabled")
	}
	if !strings.Contains(string(result.Data), "approval_unavailable") {
		t.Fatalf("expected approval_unavailable refusal, got %s", result.Data)
	}
	if strings.Contains(string(result.Data), "PREPARED") {
		t.Fatal("confirm-kind dispatcher must not execute when HITL is disabled")
	}
}

func TestExecutionResolverResolvesOnlyConfirmEntries(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterBuiltinCatalog(reg, nil)
	resolver := NewExecutionResolver(reg)
	scope := codeModeScope()

	tool, definition, err := resolver.ResolveForExecution(tools.Call{Name: "crm.exportCustomer", Version: 1}, scope)
	if err != nil {
		t.Fatalf("confirm entry must resolve: %v", err)
	}
	if definition.Kind != "confirm" || definition.Timeout <= 0 {
		t.Fatalf("definition = %+v, want confirm kind with timeout", definition)
	}

	// Read-kind entries are never approval-executable.
	if _, _, err := resolver.ResolveForExecution(tools.Call{Name: "iam.me", Version: 1}, scope); err == nil {
		t.Fatal("read entry must not resolve for execution")
	}

	// Missing tool permission fails closed.
	denied := tools.Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
		Permissions: map[string]struct{}{"ai.assistant.use": {}},
	}
	if _, _, err := resolver.ResolveForExecution(tools.Call{Name: "crm.exportCustomer", Version: 1}, denied); err == nil {
		t.Fatal("missing permission must not resolve for execution")
	}

	if _, _, err := resolver.ResolveForExecution(tools.Call{Name: "nope.missing", Version: 1}, scope); err == nil {
		t.Fatal("unknown tool must not resolve for execution")
	}

	result, err := tool.Execute(context.Background(), scope, json.RawMessage(`{"customerId":"c1","format":"csv"}`))
	if err != nil {
		t.Fatalf("approved execution failed: %v", err)
	}
	if !strings.Contains(string(result.Data), "PREPARED") {
		t.Fatalf("executed stub result = %s", result.Data)
	}
}
