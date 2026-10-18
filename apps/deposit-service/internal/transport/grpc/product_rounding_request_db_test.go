package grpc

// Đường đi trọn vẹn của cấu hình làm tròn sản phẩm trên PostgreSQL thật: người lập trình yêu cầu (đăng ký / sửa)
// với round_no + round_type, người duyệt đồng ý, dpm_products nhận đúng giá trị. Tên bắt đầu bằng TestCaseCommand
// để bước CI "Verify shared case commands on disposable PostgreSQL 18" chạy được.

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

type stubWorkflow struct{ cases int }

func (w *stubWorkflow) CreateCase(context.Context, workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	w.cases++
	return &workflowv1.BusinessCase{Id: "00000000-0000-0000-0000-00000000000" + string(rune('0'+w.cases)), CaseCode: "DPM-PROD"}, nil
}

func (w *stubWorkflow) SubmitCase(context.Context, string, string, map[string]any, string) (*workflowv1.BusinessCase, error) {
	return &workflowv1.BusinessCase{}, nil
}

func ip(v int) *int       { return &v }
func sp(v string) *string { return &v }

type productRound struct {
	no   sql.NullInt32
	kind sql.NullString
}

func readProductRound(t *testing.T, db *sql.DB, table, codeColumn, code string) productRound {
	t.Helper()
	var r productRound
	// A request code can repeat (register then edit); the newest row is the one under test.
	if err := db.QueryRow(`SELECT round_no, round_type FROM `+table+` WHERE tenant_id = $1 AND `+codeColumn+` = $2 ORDER BY created_at DESC LIMIT 1`,
		caseTestTenant, code).Scan(&r.no, &r.kind); err != nil {
		t.Fatal(err)
	}
	return r
}

func wantRound(t *testing.T, got productRound, no int, kind string) {
	t.Helper()
	if !got.no.Valid || int(got.no.Int32) != no || !got.kind.Valid || got.kind.String != kind {
		t.Fatalf("round = (%v, %v), want (%d, %s)", got.no, got.kind, no, kind)
	}
}

// version is the row version the checker saw (the optimistic-lock token of the decision).
func version(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var v int64
	if err := db.QueryRow(`SELECT version FROM dpm_product_requests WHERE id = $1`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return strconv.FormatInt(v, 10)
}

func wantNoRound(t *testing.T, got productRound) {
	t.Helper()
	if got.no.Valid || got.kind.Valid {
		t.Fatalf("round = (%v, %v), want not configured", got.no, got.kind)
	}
}

func TestCaseCommandProductRoundingFlowsThroughMakerChecker(t *testing.T) {
	server, db := newCaseCommandFixture(t)
	repo := repository.NewDepositRepository(db)
	workflow := &stubWorkflow{}
	products := service.NewProductRequestService(repo, workflow)
	ctx := tenantContext(caseTestTenant)

	submit := func(in service.ProductRequestInput) (string, error) {
		t.Helper()
		if _, err := products.Submit(context.Background(), caseTestTenant, "maker-1", in); err != nil {
			return "", err
		}
		var id string
		if err := db.QueryRow(`SELECT id FROM dpm_product_requests WHERE tenant_id = $1 AND product_code = $2 ORDER BY created_at DESC LIMIT 1`,
			caseTestTenant, in.ProductCode).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id, nil
	}
	base := func(kind string) service.ProductRequestInput {
		return service.ProductRequestInput{RequestType: kind, ProductCode: "TK-ROUND", Name: "Tiết kiệm làm tròn", TermMonths: 12, InterestRate: 5.5}
	}

	// Đăng ký không điền làm tròn: hợp lệ, nghĩa là "chưa cấu hình" cả ở yêu cầu lẫn sản phẩm.
	register := base(service.ProductRequestRegister)
	id, err := submit(register)
	if err != nil {
		t.Fatalf("register without rounding: %v", err)
	}
	wantNoRound(t, readProductRound(t, db, "dpm_product_requests", "product_code", "TK-ROUND"))
	if _, err := resolve(ctx, server, KindProductRequest, id, "", approve, version(t, db, id)); err != nil {
		t.Fatalf("approve register: %v", err)
	}
	wantNoRound(t, readProductRound(t, db, "dpm_products", "code", "TK-ROUND"))

	// Sửa để điền 1.000 + CM163.001: yêu cầu giữ giá trị, sản phẩm chỉ nhận sau khi người duyệt đồng ý.
	edit := base(service.ProductRequestEdit)
	edit.RoundNo, edit.RoundType = ip(1000), sp("CM163.001")
	id, err = submit(edit)
	if err != nil {
		t.Fatalf("edit with rounding: %v", err)
	}
	wantRound(t, readProductRound(t, db, "dpm_product_requests", "product_code", "TK-ROUND"), 1000, "CM163.001")
	wantNoRound(t, readProductRound(t, db, "dpm_products", "code", "TK-ROUND"))
	if _, err := resolve(ctx, server, KindProductRequest, id, "", approve, version(t, db, id)); err != nil {
		t.Fatalf("approve edit: %v", err)
	}
	wantRound(t, readProductRound(t, db, "dpm_products", "code", "TK-ROUND"), 1000, "CM163.001")

	// Người duyệt từ chối thì sản phẩm giữ nguyên.
	edit = base(service.ProductRequestEdit)
	edit.RoundNo, edit.RoundType = ip(5000), sp("CM163.003")
	id, err = submit(edit)
	if err != nil {
		t.Fatalf("edit to reject: %v", err)
	}
	if _, err := resolve(ctx, server, KindProductRequest, id, "", reject, ""); err != nil {
		t.Fatalf("reject edit: %v", err)
	}
	wantRound(t, readProductRound(t, db, "dpm_products", "code", "TK-ROUND"), 1000, "CM163.001")

	// round_no = 0 (làm tròn đồng một lần) là giá trị hợp lệ, không bị coi là "để trống".
	edit = base(service.ProductRequestEdit)
	edit.RoundNo, edit.RoundType = ip(0), sp("CM163.002")
	id, err = submit(edit)
	if err != nil {
		t.Fatalf("edit with round_no 0: %v", err)
	}
	if _, err := resolve(ctx, server, KindProductRequest, id, "", approve, version(t, db, id)); err != nil {
		t.Fatalf("approve round_no 0: %v", err)
	}
	wantRound(t, readProductRound(t, db, "dpm_products", "code", "TK-ROUND"), 0, "CM163.002")

	// Sửa không điền làm tròn: giữ nguyên giá trị đã duyệt (không xóa cấu hình ngầm).
	id, err = submit(base(service.ProductRequestEdit))
	if err != nil {
		t.Fatalf("edit without rounding: %v", err)
	}
	if _, err := resolve(ctx, server, KindProductRequest, id, "", approve, version(t, db, id)); err != nil {
		t.Fatalf("approve edit without rounding: %v", err)
	}
	wantRound(t, readProductRound(t, db, "dpm_products", "code", "TK-ROUND"), 0, "CM163.002")

	// Điền một nửa bị chặn trước khi lưu yêu cầu: không có hàng mới, không mở ca.
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM dpm_product_requests WHERE tenant_id = $1`, caseTestTenant).Scan(&before); err != nil {
		t.Fatal(err)
	}
	cases := workflow.cases
	half := base(service.ProductRequestEdit)
	half.RoundNo = ip(1000)
	_, err = products.Submit(context.Background(), caseTestTenant, "maker-1", half)
	var typed *ardaerrors.Error
	if !errors.As(err, &typed) || typed.Code != ardaerrors.CodeInvalidInput {
		t.Fatalf("half-filled pair = %v, want %s", err, ardaerrors.CodeInvalidInput)
	}
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM dpm_product_requests WHERE tenant_id = $1`, caseTestTenant).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before || workflow.cases != cases {
		t.Fatalf("half-filled pair left %d new request(s) and %d new case(s)", after-before, workflow.cases-cases)
	}
}

// Ràng buộc cuối cùng nằm ở bảng: ngay cả đường ghi trực tiếp cũng không lưu được cặp lệch hoặc kiểu lạ.
func TestCaseCommandProductRoundingTableConstraintsHold(t *testing.T) {
	_, db := newCaseCommandFixture(t)
	repo := repository.NewDepositRepository(db)
	ctx := context.Background()
	for name, p := range map[string]repository.SavingsProduct{
		"chỉ round_no":   {TenantID: caseTestTenant, Code: "BAD-1", Name: "x", TermMonths: 6, RoundNo: ip(1000)},
		"chỉ round_type": {TenantID: caseTestTenant, Code: "BAD-2", Name: "x", TermMonths: 6, RoundType: sp("CM163.001")},
		"round_no âm":    {TenantID: caseTestTenant, Code: "BAD-3", Name: "x", TermMonths: 6, RoundNo: ip(-1), RoundType: sp("CM163.001")},
		"kiểu lạ":        {TenantID: caseTestTenant, Code: "BAD-4", Name: "x", TermMonths: 6, RoundNo: ip(1000), RoundType: sp("HALF_UP")},
	} {
		p.CurrencyCode = "VND"
		if _, err := repo.UpsertProduct(ctx, &p); err == nil {
			t.Fatalf("%s: UpsertProduct accepted an invalid round configuration", name)
		}
	}
}
