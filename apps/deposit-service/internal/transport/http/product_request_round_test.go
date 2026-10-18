package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/deposit-service/internal/handler"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

// recordingWorkflow counts the cases the product request service would open.
type recordingWorkflow struct{ created int }

func (w *recordingWorkflow) CreateCase(context.Context, workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	w.created++
	return &workflowv1.BusinessCase{Id: "case-1", CaseCode: "DPM-PROD-1"}, nil
}

func (w *recordingWorkflow) SubmitCase(context.Context, string, string, map[string]any, string) (*workflowv1.BusinessCase, error) {
	return &workflowv1.BusinessCase{Id: "case-1"}, nil
}

func submitProductRequest(t *testing.T, body string) (*httptest.ResponseRecorder, *recordingWorkflow) {
	t.Helper()
	workflow := &recordingWorkflow{}
	// The repository is nil on purpose: every body below must be rejected by the
	// validation that runs before the request is staged.
	products := service.NewProductRequestService((*repository.DepositRepository)(nil), workflow)
	mux := NewRouter(handler.NewDepositHandler(&fakeSettlement{}, &fakeAdditional{}, products, &fakeIBM{}, &fakeInterest{}, &fakeReports{}), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/deposit/product-requests", bytes.NewBufferString(body))
	req.Header.Set("X-Tenant-Id", "tenant-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec, workflow
}

// Cặp round_no / round_type chỉ điền một nửa hay sai miền là lỗi 400 ngay ở bước trình, không mở ca duyệt nào.
func TestRouterProductRequestRejectsInvalidRoundConfig(t *testing.T) {
	const base = `"request_type":"REGISTER","product_code":"TK12","name":"Tiết kiệm 12 tháng","term_months":12,"interest_rate":5.5`
	tests := []struct {
		name    string
		extra   string
		message string
	}{
		{"chỉ round_no", `,"round_no":1000`, "together"},
		{"chỉ round_type", `,"round_type":"CM163.001"`, "together"},
		{"round_no âm", `,"round_no":-1,"round_type":"CM163.001"`, "negative"},
		{"kiểu lạ", `,"round_no":1000,"round_type":"CM163.009"`, "CM163.001"},
		{"kiểu viết tên", `,"round_no":1000,"round_type":"HALF_UP"`, "CM163.001"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, workflow := submitProductRequest(t, "{"+base+tc.extra+"}")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.message) {
				t.Fatalf("body %s does not mention %q", rec.Body.String(), tc.message)
			}
			if workflow.created != 0 {
				t.Fatalf("a rejected request opened %d approval case(s)", workflow.created)
			}
		})
	}
}
