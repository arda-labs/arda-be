package grpc

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newScopeTestServer() *WorkflowServer {
	// The repository rejects a missing tenant scope before it touches the database, so no connection is needed.
	return NewWorkflowServer(service.NewWorkflowCommandService(repository.NewCaseRepository(nil), nil))
}

func createCaseRequest() *workflowv1.CreateCaseRequest {
	return &workflowv1.CreateCaseRequest{
		TenantId: "system", CaseType: "DPM_PRODUCT_REGISTER_V1", Title: "Đăng ký sản phẩm SP_001",
		PrimaryObjectType: "dpm.product_request", PrimaryObjectId: "dpmpr-1", DomainService: "deposit-service",
		CreatedBy: "user-1",
	}
}

// Thiếu tenant scope là lỗi của người gọi/hạ tầng chứ không phải dữ liệu sai: trả PermissionDenied (cùng quy ước với
// finance-service và với 403 của HTTP), và phải để lại dòng log có lý do để không phải đoán từ "tenant_id=\"\"".
func TestCreateCaseWithoutTenantScopeIsPermissionDeniedAndLogged(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, err := newScopeTestServer().CreateCase(context.Background(), createCaseRequest())
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("code = %v (err %v), want PermissionDenied", got, err)
	}
	line := logs.String()
	for _, want := range []string{"verified tenant scope is required", "DPM_PRODUCT_REGISTER_V1", "deposit-service"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log %q does not mention %q", line, want)
		}
	}
}

func TestCreateCaseKeepsInvalidArgumentForBadInput(t *testing.T) {
	ctx := ardametadata.AppendToOutgoing(context.Background(), ardametadata.Context{TenantID: "system"})
	req := createCaseRequest()
	req.Title = ""
	_, err := newScopeTestServer().CreateCase(ctx, req)
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v (err %v), want InvalidArgument", got, err)
	}
}
