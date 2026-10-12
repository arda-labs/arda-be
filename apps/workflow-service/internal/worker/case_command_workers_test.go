package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/arda-labs/arda/libs/go/arda-grpc/client/casecommand"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"github.com/camunda/zeebe/clients/go/v8/pkg/commands"
	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type caseCommandGateway struct {
	pb.GatewayClient
	completed *pb.CompleteJobRequest
	failed    *pb.FailJobRequest
	thrown    *pb.ThrowErrorRequest
}

func (g *caseCommandGateway) CompleteJob(_ context.Context, req *pb.CompleteJobRequest, _ ...grpc.CallOption) (*pb.CompleteJobResponse, error) {
	g.completed = req
	return &pb.CompleteJobResponse{}, nil
}

func (g *caseCommandGateway) FailJob(_ context.Context, req *pb.FailJobRequest, _ ...grpc.CallOption) (*pb.FailJobResponse, error) {
	g.failed = req
	return &pb.FailJobResponse{}, nil
}

func (g *caseCommandGateway) ThrowError(_ context.Context, req *pb.ThrowErrorRequest, _ ...grpc.CallOption) (*pb.ThrowErrorResponse, error) {
	g.thrown = req
	return &pb.ThrowErrorResponse{}, nil
}

func (g *caseCommandGateway) NewCompleteJobCommand() commands.CompleteJobCommandStep1 {
	return commands.NewCompleteJobCommand(g, func(context.Context, error) bool { return false })
}
func (g *caseCommandGateway) NewFailJobCommand() commands.FailJobCommandStep1 {
	return commands.NewFailJobCommand(g, func(context.Context, error) bool { return false })
}
func (g *caseCommandGateway) NewThrowErrorCommand() commands.ThrowErrorCommandStep1 {
	return commands.NewThrowErrorCommand(g, func(context.Context, error) bool { return false })
}

type fakeCaseCommand struct {
	checkOK      bool
	checkMessage string
	err          error
	outputs      map[string]string

	kind, objectID, caseID, decidedBy, note, dataVersion string
	decision                                             casecommand.Decision
	tenantID, actorID                                    string
}

func (f *fakeCaseCommand) record(ctx context.Context, kind, objectID, caseID string) {
	f.kind, f.objectID, f.caseID = kind, objectID, caseID
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		incoming := ardametadata.FromIncoming(metadata.NewIncomingContext(ctx, md))
		f.tenantID, f.actorID = incoming.TenantID, incoming.ActorUserID
	}
}

func (f *fakeCaseCommand) CheckCase(ctx context.Context, kind, objectID, caseID string) (bool, string, error) {
	f.record(ctx, kind, objectID, caseID)
	return f.checkOK, f.checkMessage, f.err
}

func (f *fakeCaseCommand) ResolveCase(ctx context.Context, kind, objectID, caseID string, decision casecommand.Decision, decidedBy, note, dataVersion string) (map[string]string, error) {
	f.record(ctx, kind, objectID, caseID)
	f.decision, f.decidedBy, f.note, f.dataVersion = decision, decidedBy, note, dataVersion
	return f.outputs, f.err
}

func caseJob(t *testing.T, vars map[string]any) entities.Job {
	t.Helper()
	data, err := json.Marshal(vars)
	if err != nil {
		t.Fatal(err)
	}
	return entities.Job{ActivatedJob: &pb.ActivatedJob{Key: 77, Retries: 3, Type: "mc.validate", Variables: string(data)}}
}

var baseCaseVars = map[string]any{
	"primaryObjectId": "reg-1",
	"tenantId":        "tenant-1",
	"actorUserId":     "user-1",
	"mcKind":          "hrm.employee-register",
}

func completedVariables(t *testing.T, gateway *caseCommandGateway) map[string]any {
	t.Helper()
	if gateway.completed == nil {
		t.Fatal("job was not completed")
	}
	vars := map[string]any{}
	if gateway.completed.GetVariables() != "" {
		if err := json.Unmarshal([]byte(gateway.completed.GetVariables()), &vars); err != nil {
			t.Fatal(err)
		}
	}
	return vars
}

func TestCaseCommandValidateCompletesPassedCheckWithJobContext(t *testing.T) {
	caller := &fakeCaseCommand{checkOK: true}
	gateway := &caseCommandGateway{}
	handlers := NewCaseCommandWorkers(nil).Handlers("hrm.employee-register", caller)
	handlers.Validate(gateway, caseJob(t, baseCaseVars))
	completedVariables(t, gateway)
	if gateway.thrown != nil || gateway.failed != nil {
		t.Fatalf("thrown=%v failed=%v", gateway.thrown, gateway.failed)
	}
	if caller.kind != "hrm.employee-register" || caller.objectID != "reg-1" || caller.tenantID != "tenant-1" || caller.actorID != "user-1" {
		t.Fatalf("call = %+v", caller)
	}
}

func TestCaseCommandValidateThrowsBusinessFailureToMaker(t *testing.T) {
	caller := &fakeCaseCommand{checkOK: false, checkMessage: "status APPROVED is not actionable"}
	gateway := &caseCommandGateway{}
	NewCaseCommandWorkers(nil).Handlers("hrm.employee-register", caller).Validate(gateway, caseJob(t, baseCaseVars))
	if gateway.thrown == nil || gateway.thrown.GetErrorCode() != ErrorValidationFailed || gateway.thrown.GetErrorMessage() != caller.checkMessage {
		t.Fatalf("thrown = %v, want VALIDATION_FAILED with the owner's message", gateway.thrown)
	}
	if gateway.completed != nil || gateway.failed != nil {
		t.Fatal("failed check must not complete or fail the job")
	}
}

func TestCaseCommandTransportErrorsRetryAndMissingObjectStops(t *testing.T) {
	caller := &fakeCaseCommand{err: errors.New("unavailable")}
	gateway := &caseCommandGateway{}
	handlers := NewCaseCommandWorkers(nil).Handlers("hrm.employee-register", caller)
	handlers.Validate(gateway, caseJob(t, baseCaseVars))
	if gateway.failed == nil || gateway.failed.GetRetries() != 2 {
		t.Fatalf("transport error: failed = %v, want retries decremented to 2", gateway.failed)
	}
	gateway = &caseCommandGateway{}
	handlers.Execute(gateway, caseJob(t, baseCaseVars))
	if gateway.failed == nil || gateway.failed.GetRetries() != 2 || gateway.completed != nil {
		t.Fatalf("resolve error: failed = %v completed = %v", gateway.failed, gateway.completed)
	}
	gateway = &caseCommandGateway{}
	handlers.Validate(gateway, caseJob(t, map[string]any{"tenantId": "tenant-1"}))
	if gateway.failed == nil || gateway.failed.GetRetries() != 0 {
		t.Fatalf("missing object: failed = %v, want an immediate incident", gateway.failed)
	}
}

func TestCaseCommandExecuteApprovesAndFiltersReservedOutputs(t *testing.T) {
	caller := &fakeCaseCommand{outputs: map[string]string{"employeeId": "emp-9", "primaryObjectId": "hijack", "approvalStatus": "REJECTED"}}
	gateway := &caseCommandGateway{}
	vars := map[string]any{"decisionNote": "looks good", "dataVersion": float64(4)}
	for k, v := range baseCaseVars {
		vars[k] = v
	}
	NewCaseCommandWorkers(nil).Handlers("hrm.employee-register", caller).Execute(gateway, caseJob(t, vars))
	if caller.decision != casecommand.DecisionApprove || caller.decidedBy != "user-1" || caller.note != "looks good" || caller.dataVersion != "4" {
		t.Fatalf("resolve call = %+v", caller)
	}
	got := completedVariables(t, gateway)
	if got["approvalStatus"] != "APPROVED" || got["employeeId"] != "emp-9" {
		t.Fatalf("completed variables = %v", got)
	}
	if _, leaked := got["primaryObjectId"]; leaked {
		t.Fatalf("reserved output leaked into process variables: %v", got)
	}
}

func TestCaseCommandCancelRejectsWithDefaultNote(t *testing.T) {
	caller := &fakeCaseCommand{}
	gateway := &caseCommandGateway{}
	NewCaseCommandWorkers(nil).Handlers("hrm.employee-register", caller).Cancel(gateway, caseJob(t, baseCaseVars))
	if caller.decision != casecommand.DecisionReject || caller.note != "Rejected by checker" || caller.dataVersion != "" {
		t.Fatalf("resolve call = %+v", caller)
	}
	if got := completedVariables(t, gateway); got["approvalStatus"] != "REJECTED" {
		t.Fatalf("completed variables = %v", got)
	}
}

func TestMakerCheckerDispatchReachesCaseCommandKindWithoutLoan(t *testing.T) {
	caller := &fakeCaseCommand{checkOK: true}
	handlers := map[string]MakerCheckerHandlers{
		"hrm.employee-register": NewCaseCommandWorkers(nil).Handlers("hrm.employee-register", caller),
	}
	dispatcher, err := NewMakerCheckerWorkersWithAdditional(nil, []string{"hrm.employee-register"}, nil, handlers)
	if err != nil {
		t.Fatal(err)
	}
	validate, _, _ := dispatcher.Handlers()
	gateway := &caseCommandGateway{}
	validate(gateway, caseJob(t, baseCaseVars))
	completedVariables(t, gateway)
	if caller.objectID != "reg-1" {
		t.Fatalf("dispatcher did not reach the case command caller: %+v", caller)
	}
}
