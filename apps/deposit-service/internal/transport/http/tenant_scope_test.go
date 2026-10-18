package http

// deposit-service không tự kiểm tra auth: auth-gateway gửi X-Tenant-Id / X-User-Id, router phải gắn chúng vào
// metadata gRPC gửi đi (ardametadata.HTTPMiddleware). Thiếu bước này mọi lời gọi workflow.CreateCase từ service
// này đến workflow-service với tenant rỗng và bị từ chối ("verified tenant scope is required", gateway trả 502).

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/deposit-service/internal/handler"
	"github.com/arda-labs/arda/apps/deposit-service/internal/migration"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

// scopeRecordingSettlement ghi lại metadata gửi đi của ctx mà handler truyền xuống service.
type scopeRecordingSettlement struct {
	fakeSettlement
	outgoing ardametadata.Context
}

func (f *scopeRecordingSettlement) SubmitSettle(ctx context.Context, tenantID, actor, code string) (*service.Submission, error) {
	f.outgoing = ardametadata.FromOutgoing(ctx)
	return f.fakeSettlement.SubmitSettle(ctx, tenantID, actor, code)
}

func TestRouterBindsGatewayIdentityToOutgoingMetadata(t *testing.T) {
	settlement := &scopeRecordingSettlement{}
	mux := NewRouter(handler.NewDepositHandler(settlement, &fakeAdditional{}, nil, &fakeIBM{}, &fakeInterest{}, &fakeReports{}), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/deposit/savings/SV-1/settle", nil)
	req.Header.Set("X-Tenant-Id", "system")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-Request-Id", "req-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	got := settlement.outgoing
	if got.TenantID != "system" || got.UserID != "user-1" || got.AuthChecked != "true" || got.RequestID != "req-1" {
		t.Fatalf("outgoing metadata = %+v, want tenant system, user user-1, auth-checked true, request req-1", got)
	}
}

// fakeWorkflowServer đóng vai workflow-service: giữ luật verifiedTenant của repository (đọc metadata
// outgoing do UnaryServerMetadataPropagate dựng từ metadata incoming) và nhớ lại người gọi.
type fakeWorkflowServer struct {
	workflowv1.UnimplementedWorkflowCommandServiceServer
	tenants []string
	actors  []string
}

func (s *fakeWorkflowServer) CreateCase(ctx context.Context, req *workflowv1.CreateCaseRequest) (*workflowv1.BusinessCase, error) {
	scope := ardametadata.FromOutgoing(ctx)
	if scope.TenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "verified tenant scope is required")
	}
	s.tenants = append(s.tenants, scope.TenantID)
	s.actors = append(s.actors, scope.UserID)
	return &workflowv1.BusinessCase{Id: "case-1", CaseCode: "DPM-PROD-1"}, nil
}

func (s *fakeWorkflowServer) SubmitCase(context.Context, *workflowv1.SubmitCaseRequest) (*workflowv1.BusinessCase, error) {
	return &workflowv1.BusinessCase{Id: "case-1"}, nil
}

// bufconnWorkflow gọi fakeWorkflowServer qua gRPC thật với đúng interceptor metadata của client workflow.
type bufconnWorkflow struct{ api workflowv1.WorkflowCommandServiceClient }

func (w *bufconnWorkflow) CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	return w.api.CreateCase(ctx, &workflowv1.CreateCaseRequest{
		TenantId: in.TenantID, CaseType: in.CaseType, Title: in.Title, PrimaryObjectType: in.PrimaryObjectType,
		PrimaryObjectId: in.PrimaryObjectID, DomainService: in.DomainService, Priority: in.Priority,
		CreatedBy: in.CreatedBy, IdempotencyKey: in.IdempotencyKey,
	})
}

func (w *bufconnWorkflow) SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, key string) (*workflowv1.BusinessCase, error) {
	vars, err := structpb.NewStruct(variables)
	if err != nil {
		return nil, err
	}
	return w.api.SubmitCase(ctx, &workflowv1.SubmitCaseRequest{CaseId: caseID, Actor: actor, Variables: vars, IdempotencyKey: key})
}

func newBufconnWorkflow(t *testing.T) (*bufconnWorkflow, *fakeWorkflowServer) {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(interceptors.UnaryServerMetadataPropagate()))
	fake := &fakeWorkflowServer{}
	workflowv1.RegisterWorkflowCommandServiceServer(server, fake)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(interceptors.UnaryClientMetadata("deposit-service", ardametadata.Context{})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &bufconnWorkflow{api: workflowv1.NewWorkflowCommandServiceClient(conn)}, fake
}

func countProductRequests(t *testing.T, db *sql.DB, status string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM dpm_product_requests WHERE status = $1`, status).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func postProductRequest(t *testing.T, db *sql.DB, workflow service.WorkflowSubmitter, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	repo := repository.NewDepositRepository(db)
	products := service.NewProductRequestService(repo, workflow)
	mux := NewRouter(handler.NewDepositHandler(&fakeSettlement{}, &fakeAdditional{}, products, &fakeIBM{}, &fakeInterest{}, &fakeReports{}), nil, nil)
	body := `{"request_type":"REGISTER","product_code":"SP_001","name":"Tiết kiệm 12 tháng","term_months":12,"interest_rate":5.5}`
	req := httptest.NewRequest(http.MethodPost, "/api/deposit/product-requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// Đúng ca đã gặp trên DEV: POST /api/deposit/product-requests qua router thật, service thật trên PostgreSQL,
// gRPC thật tới workflow giả. Cần ARDA_TEST_DSN (PostgreSQL 18); thiếu thì bị bỏ qua như các test DB khác.
func TestTenantScopeProductRequestReachesWorkflow(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	workflow, fake := newBufconnWorkflow(t)

	rec := postProductRequest(t, db, workflow, map[string]string{
		"X-Tenant-Id": "system", "X-User-Id": "00000000-0000-0000-0000-000000000002", "X-Auth-Checked": "true",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if len(fake.tenants) != 1 || fake.tenants[0] != "system" || fake.actors[0] != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("workflow saw tenants %v actors %v, want one call from system / the maker", fake.tenants, fake.actors)
	}
	if n := countProductRequests(t, db, "SUBMITTED"); n != 1 {
		t.Fatalf("SUBMITTED requests = %d, want 1", n)
	}
}

// failingWorkflow đóng vai workflow-service từ chối ca (CreateCase lỗi hoặc SubmitCase lỗi).
type failingWorkflow struct {
	createErr, submitErr error
}

func (w failingWorkflow) CreateCase(context.Context, workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	if w.createErr != nil {
		return nil, w.createErr
	}
	return &workflowv1.BusinessCase{Id: "case-1", CaseCode: "DPM-PROD-1"}, nil
}

func (w failingWorkflow) SubmitCase(context.Context, string, string, map[string]any, string) (*workflowv1.BusinessCase, error) {
	return nil, w.submitErr
}

// Lỗi workflow không được để lại dòng yêu cầu SUBMITTED mồ côi (không có ca duyệt nào sẽ xử lý nó, nhưng nó vẫn hiện
// trong danh sách chờ duyệt): dòng phải chuyển sang FAILED. Cần ARDA_TEST_DSN.
func TestTenantScopeWorkflowFailureDoesNotLeaveOrphanRequest(t *testing.T) {
	headers := map[string]string{"X-Tenant-Id": "system", "X-User-Id": "user-1", "X-Auth-Checked": "true"}
	tests := []struct {
		name string
		wf   failingWorkflow
	}{
		{"CreateCase lỗi", failingWorkflow{createErr: errors.New("rpc error: code = PermissionDenied desc = verified tenant scope is required")}},
		{"SubmitCase lỗi", failingWorkflow{submitErr: errors.New("rpc error: code = FailedPrecondition desc = case is not submittable")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
			rec := postProductRequest(t, db, tc.wf, headers)
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502 (body %s)", rec.Code, rec.Body.String())
			}
			if n := countProductRequests(t, db, "SUBMITTED"); n != 0 {
				t.Fatalf("SUBMITTED requests = %d, want 0 (orphan left behind)", n)
			}
			if n := countProductRequests(t, db, "FAILED"); n != 1 {
				t.Fatalf("FAILED requests = %d, want 1", n)
			}
		})
	}
}
