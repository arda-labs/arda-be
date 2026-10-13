package casecommand

import (
	"context"
	"net"
	"testing"

	commonv1 "github.com/arda-labs/arda/libs/go/arda-proto/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type recordingServer struct {
	commonv1.UnimplementedCaseCommandServiceServer
	check   *commonv1.CheckCaseRequest
	resolve *commonv1.ResolveCaseRequest
}

func (s *recordingServer) CheckCase(_ context.Context, req *commonv1.CheckCaseRequest) (*commonv1.CheckCaseResponse, error) {
	s.check = req
	if req.GetObjectId() == "bad" {
		return &commonv1.CheckCaseResponse{Ok: false, Message: "not actionable"}, nil
	}
	return &commonv1.CheckCaseResponse{Ok: true}, nil
}

func (s *recordingServer) ResolveCase(_ context.Context, req *commonv1.ResolveCaseRequest) (*commonv1.ResolveCaseResponse, error) {
	s.resolve = req
	if req.GetObjectId() == "locked" {
		return nil, status.Error(codes.FailedPrecondition, "locked")
	}
	return &commonv1.ResolveCaseResponse{Outputs: map[string]string{"employeeId": "emp-1"}}, nil
}

func newTestClient(t *testing.T) (*Client, *recordingServer) {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	recorder := &recordingServer{}
	commonv1.RegisterCaseCommandServiceServer(server, recorder)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return newWithConn(conn, "hrm-service"), recorder
}

func TestCheckCaseMapsRequestAndBusinessFailure(t *testing.T) {
	client, server := newTestClient(t)
	ok, message, err := client.CheckCase(context.Background(), "hrm.employee-register", "reg-1", "case-1")
	if err != nil || !ok || message != "" {
		t.Fatalf("CheckCase = %t %q %v", ok, message, err)
	}
	if server.check.GetKind() != "hrm.employee-register" || server.check.GetObjectId() != "reg-1" || server.check.GetCaseId() != "case-1" {
		t.Fatalf("request = %+v", server.check)
	}
	ok, message, err = client.CheckCase(context.Background(), "hrm.employee-register", "bad", "")
	if err != nil || ok || message != "not actionable" {
		t.Fatalf("failed check = %t %q %v", ok, message, err)
	}
}

func TestResolveCaseMapsDecisionAndOutputs(t *testing.T) {
	client, server := newTestClient(t)
	outputs, err := client.ResolveCase(context.Background(), "hrm.employee-register", "reg-1", "case-1", DecisionApprove, "user-1", "ok", " 7 ")
	if err != nil || outputs["employeeId"] != "emp-1" {
		t.Fatalf("ResolveCase = %v %v", outputs, err)
	}
	if server.resolve.GetDecision() != commonv1.CaseDecision_CASE_DECISION_APPROVE || server.resolve.GetDecidedBy() != "user-1" || server.resolve.GetNote() != "ok" || server.resolve.GetDataVersion() != "7" {
		t.Fatalf("request = %+v", server.resolve)
	}
	if _, err := client.ResolveCase(context.Background(), "k", "reg-1", "", DecisionReject, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if server.resolve.GetDecision() != commonv1.CaseDecision_CASE_DECISION_REJECT {
		t.Fatalf("reject decision = %v", server.resolve.GetDecision())
	}
	if _, err := client.ResolveCase(context.Background(), "k", "locked", "", DecisionApprove, "", "", ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("server error = %v, want FailedPrecondition passed through", err)
	}
}

func TestClientRejectsInvalidInput(t *testing.T) {
	client, _ := newTestClient(t)
	if _, _, err := client.CheckCase(context.Background(), "", "id", ""); err == nil {
		t.Fatal("empty kind accepted")
	}
	if _, err := client.ResolveCase(context.Background(), "k", "id", "", Decision("CANCEL"), "", "", ""); err == nil {
		t.Fatal("unknown decision accepted")
	}
	var nilClient *Client
	if _, _, err := nilClient.CheckCase(context.Background(), "k", "id", ""); err == nil {
		t.Fatal("nil client accepted")
	}
	if _, err := Dial("", "hrm-service", "workflow-service"); err == nil {
		t.Fatal("empty address accepted")
	}
}
