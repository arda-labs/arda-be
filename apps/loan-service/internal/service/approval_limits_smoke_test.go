package service

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

// limitCaptureWorkflow ghi lại biến BPMN mà SubmitContract gửi đi.
type limitCaptureWorkflow struct {
	created int
	vars    map[string]any
}

func (f *limitCaptureWorkflow) CreateCase(ctx context.Context, in workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	f.created++
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano()%1_000_000_000_000)
	return &workflowv1.BusinessCase{Id: id}, nil
}

func (f *limitCaptureWorkflow) SubmitCase(ctx context.Context, caseID, actor string, variables map[string]any, idempotencyKey string) (*workflowv1.BusinessCase, error) {
	f.vars = variables
	return &workflowv1.BusinessCase{Id: caseID}, nil
}

// TestApprovalLimitSubmitSmoke (AP-APR-01, cần ARDA_TEST_DSN): seed đã đúng
// đơn vị VND, DB chặn dòng cấu hình sai, hồ sơ thiếu cấu hình lên Hội đồng
// và lỗi tra cứu hạn mức chặn nộp trước khi mở case.
func TestApprovalLimitSubmitSmoke(t *testing.T) {
	const tenantID = "00000000-0000-0000-0000-000000000010"

	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewLoanRepository(db)
	run := time.Now().UTC().Format("20060102T150405.000000000")

	// Seed pilot sau migration sửa đơn vị: 500 triệu / 2 tỷ VND (số mũ 0).
	var pgdSeed, gdSeed int64
	if err := db.QueryRowContext(ctx, `SELECT pgd_limit_minor, gd_limit_minor FROM lnm_approval_limits
		WHERE tenant_id = $1 AND org_code = 'LNM_PGD' AND product_code = ''`, tenantID).Scan(&pgdSeed, &gdSeed); err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if pgdSeed != 500_000_000 || gdSeed != 2_000_000_000 {
		t.Fatalf("seed hạn mức sai đơn vị: pgd=%d gd=%d", pgdSeed, gdSeed)
	}

	// Ràng buộc DB chặn cấu hình sai.
	for _, bad := range [][2]int64{{0, 1}, {2_000_000_000, 500_000_000}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO lnm_approval_limits (tenant_id, org_code, product_code, pgd_limit_minor, gd_limit_minor)
			VALUES ($1, 'LNM_BAD', $2, $3, $4)`, tenantID, fmt.Sprintf("P%d", bad[0]), bad[0], bad[1]); err == nil {
			t.Fatalf("DB phải từ chối hạn mức sai %v", bad)
		}
	}

	newContract := func(suffix, orgCode string) domain.Contract {
		t.Helper()
		created, err := repo.CreateContract(ctx, &domain.Contract{
			ID:           repository.NewID("ctrt"),
			TenantID:     tenantID,
			ContractCode: "APRL-" + suffix + "-" + run,
			CustomerCode: "KH-APRL",
			ContractDate: "2026-09-07",
			MaturityDate: "2027-09-07",
			LoanTerm:     12,
			TermUnit:     "MONTH",
			LoanAmt:      100_000_000,
			InterestRate: 8.5,
			Status:       domain.ContractDraft,
			CreatedBy:    "smoke",
		})
		if err != nil {
			t.Fatalf("seed contract: %v", err)
		}
		if orgCode != "" {
			if _, err := db.ExecContext(ctx, `UPDATE lnm_contracts SET org_code = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, created.ID, orgCode); err != nil {
				t.Fatalf("stamp org: %v", err)
			}
		}
		return *created
	}

	// Có cấu hình: biến BPMN lấy đúng hạn mức của org.
	wf := &limitCaptureWorkflow{}
	svc := NewLoanService(repo, wf)
	configured := newContract("CFG", "LNM_PGD")
	if _, err := svc.SubmitContract(ctx, tenantID, "smoke", configured.ID); err != nil {
		t.Fatalf("submit configured: %v", err)
	}
	if wf.vars["pgdLimit"] != int64(500_000_000) || wf.vars["gdLimit"] != int64(2_000_000_000) {
		t.Fatalf("configured vars: %+v", wf.vars)
	}

	// Thiếu cấu hình: không hạ về cấp thực thi; khoản 100 triệu phải lên Hội đồng.
	wf = &limitCaptureWorkflow{}
	svc = NewLoanService(repo, wf)
	missing := newContract("MISS", "LNM_NO_LIMIT")
	if _, err := svc.SubmitContract(ctx, tenantID, "smoke", missing.ID); err != nil {
		t.Fatalf("submit missing-config: %v", err)
	}
	amount := wf.vars["amount"].(int64)
	if !(amount > wf.vars["gdLimit"].(int64)) || !(amount > wf.vars["pgdLimit"].(int64)) {
		t.Fatalf("thiếu cấu hình phải lên Hội đồng: %+v", wf.vars)
	}

	// Lỗi tra cứu hạn mức: chặn nộp và không mở case.
	if _, err := db.ExecContext(ctx, `ALTER TABLE lnm_approval_limits RENAME TO lnm_approval_limits_gone`); err != nil {
		t.Fatalf("simulate lookup failure: %v", err)
	}
	wf = &limitCaptureWorkflow{}
	svc = NewLoanService(repo, wf)
	blocked := newContract("ERR", "LNM_PGD")
	if _, err := svc.SubmitContract(ctx, tenantID, "smoke", blocked.ID); err == nil {
		t.Fatal("lỗi tra cứu hạn mức phải chặn nộp hồ sơ")
	}
	if wf.created != 0 {
		t.Fatalf("không được mở case khi chưa giải được hạn mức, created=%d", wf.created)
	}
}
