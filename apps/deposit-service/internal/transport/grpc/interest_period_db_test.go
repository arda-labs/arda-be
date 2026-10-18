package grpc

// Kiểm trên PostgreSQL thật (ARDA_TEST_DSN) các câu SQL của dự chi lũy kế: mốc đầu kỳ suy ra từ op trả/nhập gốc,
// tổng đã dự chi của kỳ, gửi thêm, mốc lãi suất, và ràng buộc round_no/round_type của sản phẩm. Tên bắt đầu bằng
// TestCaseCommand để bước CI "Verify shared case commands on disposable PostgreSQL 18" chạy được.

import (
	"context"
	"testing"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
)

func TestCaseCommandInterestOpStartsPeriodOnlyWhileInFlight(t *testing.T) {
	_, db := newCaseCommandFixture(t)
	repo := repository.NewDepositRepository(db)
	ctx := context.Background()
	savingsID := insertSavings(t, db, "STK-P", "ACTIVE")
	op := insertInterestOp(t, db, savingsID, "STK-P", "SUBMITTED", nil)

	if got, err := repo.InterestPeriodStart(ctx, caseTestTenant, savingsID); err != nil || got != "" {
		t.Fatalf("chưa có op POSTING/POSTED: mốc kỳ = %q, %v; muốn rỗng (kỳ bắt đầu ở ngày mở sổ)", got, err)
	}
	if err := repo.BeginInterestOpPosting(ctx, caseTestTenant, op, savingsID, 10000, false, "", "2026-10-18"); err != nil {
		t.Fatalf("BeginInterestOpPosting: %v", err)
	}
	var status, effective string
	if err := db.QueryRow(`SELECT status, effective_date::text FROM dpm_interest_ops WHERE id = $1::uuid`, op).Scan(&status, &effective); err != nil {
		t.Fatal(err)
	}
	if status != "POSTING" || effective != "2026-10-18" {
		t.Fatalf("op = (%s, %s), muốn POSTING với effective_date 2026-10-18 đặt trước khi chuyển POSTING", status, effective)
	}
	if got, _ := repo.InterestPeriodStart(ctx, caseTestTenant, savingsID); got != "2026-10-18" {
		t.Fatalf("mốc kỳ khi POSTING = %q, muốn 2026-10-18", got)
	}
	// Hoàn tác (GL lỗi): op về SUBMITTED, mốc kỳ tự trở về ngày mở sổ.
	if err := repo.CompensateInterestOp(ctx, caseTestTenant, op, savingsID, 10000, false); err != nil {
		t.Fatalf("CompensateInterestOp: %v", err)
	}
	if got, _ := repo.InterestPeriodStart(ctx, caseTestTenant, savingsID); got != "" {
		t.Fatalf("mốc kỳ sau hoàn tác = %q, muốn rỗng", got)
	}
	// Làm lại ở ngày khác và hoàn tất: mốc là ngày của lần sau.
	if err := repo.BeginInterestOpPosting(ctx, caseTestTenant, op, savingsID, 10000, false, "", "2026-10-20"); err != nil {
		t.Fatalf("BeginInterestOpPosting lần 2: %v", err)
	}
	if err := repo.MarkInterestOpPosted(ctx, caseTestTenant, op, ""); err != nil {
		t.Fatalf("MarkInterestOpPosted: %v", err)
	}
	if got, _ := repo.InterestPeriodStart(ctx, caseTestTenant, savingsID); got != "2026-10-20" {
		t.Fatalf("mốc kỳ sau khi POSTED = %q, muốn 2026-10-20", got)
	}
}

func TestCaseCommandInterestOpEffectiveDateConstraint(t *testing.T) {
	_, db := newCaseCommandFixture(t)
	savingsID := insertSavings(t, db, "STK-C", "ACTIVE")
	for _, status := range []string{"POSTING", "POSTED"} {
		if _, err := db.Exec(`
			INSERT INTO dpm_interest_ops (tenant_id, savings_id, savings_code, op_type, amount_minor, status, created_by)
			VALUES ($1, $2, 'STK-C', 'PAY', 10000, $3, 'tester')`, caseTestTenant, savingsID, status); err == nil {
			t.Fatalf("op %s không có effective_date phải bị chk_dpm_interest_op_effective_date từ chối", status)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO dpm_interest_ops (tenant_id, savings_id, savings_code, op_type, amount_minor, status, created_by)
		VALUES ($1, $2, 'STK-C', 'PAY', 10000, 'SUBMITTED', 'tester')`, caseTestTenant, savingsID); err != nil {
		t.Fatalf("op SUBMITTED không cần effective_date: %v", err)
	}
}

func TestCaseCommandAccrualPeriodReads(t *testing.T) {
	_, db := newCaseCommandFixture(t)
	repo := repository.NewDepositRepository(db)
	ctx := context.Background()
	savingsID := insertSavings(t, db, "STK-A", "ACTIVE")

	for _, a := range []repository.Accrual{
		{PeriodFrom: "2026-01-01", PeriodTo: "2026-01-01", Days: 1, AmountMinor: 1_000},
		{PeriodFrom: "2026-01-02", PeriodTo: "2026-01-04", Days: 3, AmountMinor: 2_000},
		{PeriodFrom: "2026-01-05", PeriodTo: "2026-01-05", Days: 1, AmountMinor: 3_000},
	} {
		a.TenantID, a.SavingsID, a.SavingsCode, a.BaseMinor, a.Rate = caseTestTenant, savingsID, "STK-A", 5_000_000, 5
		if inserted, err := repo.CreateAccrual(ctx, &a); err != nil || !inserted {
			t.Fatalf("CreateAccrual: %v %v", inserted, err)
		}
	}
	if got, err := repo.SumPeriodAccruals(ctx, caseTestTenant, savingsID, "2026-01-01"); err != nil || got != 6_000 {
		t.Fatalf("tổng cả kỳ = %d, %v; muốn 6000 (mọi trạng thái, kể cả PENDING)", got, err)
	}
	if got, _ := repo.SumPeriodAccruals(ctx, caseTestTenant, savingsID, "2026-01-02"); got != 5_000 {
		t.Fatalf("tổng từ 02/01 = %d, muốn 5000 (dòng 01/01 thuộc kỳ trước)", got)
	}
	if got, _ := repo.SumPeriodAccruals(ctx, caseTestTenant, savingsID, "2026-02-01"); got != 0 {
		t.Fatalf("tổng kỳ chưa có dòng = %d, muốn 0", got)
	}

	for i, tx := range []struct{ typ, date, status string }{
		{"TOP_UP_POSTED", "2026-01-03", "POSTED"},
		{"TOP_UP_POSTED", "2026-01-09", "POSTED"},
		{"TOP_UP_POSTED", "2026-01-04", "DRAFT"},
		{"WITHDRAW", "2026-01-05", "POSTED"},
	} {
		if _, err := db.Exec(`
			INSERT INTO dpm_transactions (id, tenant_id, savings_id, txn_type, amount_minor, txn_date, status, created_by)
			VALUES ($1, $2, $3, $4, $5, $6::date, $7, 'tester')`,
			repository.NewDepositID("dpmtx"), caseTestTenant, savingsID, tx.typ, int64(100+i), tx.date, tx.status); err != nil {
			t.Fatal(err)
		}
	}
	topUps, err := repo.ListPostedTopUps(ctx, caseTestTenant, savingsID, "2026-01-03")
	if err != nil || len(topUps) != 2 || topUps[0].Date != "2026-01-03" || topUps[0].AmountMinor != 100 || topUps[1].Date != "2026-01-09" {
		t.Fatalf("gửi thêm = %+v, %v; muốn 2 khoản POSTED loại TOP_UP_POSTED từ 03/01", topUps, err)
	}
	if topUps, _ = repo.ListPostedTopUps(ctx, caseTestTenant, savingsID, "2026-01-04"); len(topUps) != 1 {
		t.Fatalf("gửi thêm từ 04/01 = %+v, muốn 1 khoản", topUps)
	}

	for _, r := range []struct {
		product, from string
		rate          float64
		active        bool
	}{
		{"TK6", "2026-01-01", 6, true},
		{"TK6", "2026-01-10", 5, true},
		{"TK6", "2026-01-20", 4, false},
		{"", "2026-01-15", 3, true},
		{"OTHER", "2026-01-12", 2, true},
	} {
		var product any
		if r.product != "" {
			product = r.product
		}
		if _, err := db.Exec(`
			INSERT INTO dpm_interest_rates (id, tenant_id, product_code, term_months, method, denominator, rate, effective_from, is_active)
			VALUES ($1, $2, $3, 0, 'SIMPLE', 365, $4, $5::date, $6)`,
			repository.NewDepositID("dpmrate"), caseTestTenant, product, r.rate, r.from, r.active); err != nil {
			t.Fatal(err)
		}
	}
	dates, err := repo.ListRateChangeDates(ctx, caseTestTenant, "TK6", 0, "2026-01-01", "2026-01-31")
	if err != nil || len(dates) != 2 || dates[0] != "2026-01-10" || dates[1] != "2026-01-15" {
		t.Fatalf("ngày đổi lãi suất = %v, %v; muốn [2026-01-10 2026-01-15] (bucket TK6 và mặc định, bỏ dòng không hoạt động và sản phẩm khác)", dates, err)
	}
	// Trước khi dòng mặc định (NULL) có hiệu lực, dòng riêng của sản phẩm là dòng đang áp dụng.
	if row, err := repo.FindEffectiveRate(ctx, caseTestTenant, "TK6", 0, "2026-01-12"); err != nil || row == nil || row.Rate != 5 {
		t.Fatalf("lãi suất ngày 12/01 = %+v, %v; muốn 5", row, err)
	}
}

func TestCaseCommandProductRoundingColumns(t *testing.T) {
	_, db := newCaseCommandFixture(t)
	repo := repository.NewDepositRepository(db)
	ctx := context.Background()
	no, kind := 1000, "CM163.001"

	if _, err := repo.UpsertProduct(ctx, &repository.SavingsProduct{TenantID: caseTestTenant, Code: "RND", Name: "Co lam tron", TermMonths: 6,
		InterestRate: 5, CurrencyCode: "VND", RoundNo: &no, RoundType: &kind, CreatedBy: "tester"}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetProductByCode(ctx, caseTestTenant, "RND")
	if err != nil || got == nil || got.RoundNo == nil || *got.RoundNo != 1000 || got.RoundType == nil || *got.RoundType != "CM163.001" {
		t.Fatalf("sản phẩm = %+v, %v; muốn round_no 1000 và CM163.001", got, err)
	}
	// Sửa không nêu làm tròn: giữ nguyên giá trị đã lưu.
	if _, err := repo.UpsertProduct(ctx, &repository.SavingsProduct{TenantID: caseTestTenant, Code: "RND", Name: "Doi ten", TermMonths: 6,
		InterestRate: 5.5, CurrencyCode: "VND", CreatedBy: "tester"}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetProductByCode(ctx, caseTestTenant, "RND")
	if got.Name != "Doi ten" || got.RoundNo == nil || *got.RoundNo != 1000 || got.RoundType == nil {
		t.Fatalf("sửa không nêu làm tròn phải giữ round_no/round_type: %+v", got)
	}
	// Sản phẩm không cấu hình: NULL, không DEFAULT.
	if _, err := repo.UpsertProduct(ctx, &repository.SavingsProduct{TenantID: caseTestTenant, Code: "NOR", Name: "Khong lam tron", TermMonths: 6,
		InterestRate: 5, CurrencyCode: "VND", CreatedBy: "tester"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = repo.GetProductByCode(ctx, caseTestTenant, "NOR"); got.RoundNo != nil || got.RoundType != nil {
		t.Fatalf("migration không được tự điền: %+v", got)
	}
	// CHECK ở DB: chỉ một trong hai, số âm, mã lạ.
	for name, stmt := range map[string]string{
		"chỉ round_no":   `UPDATE dpm_products SET round_no = 1000, round_type = NULL WHERE code = 'NOR'`,
		"chỉ round_type": `UPDATE dpm_products SET round_no = NULL, round_type = 'CM163.001' WHERE code = 'NOR'`,
		"round_no âm":    `UPDATE dpm_products SET round_no = -5, round_type = 'CM163.001' WHERE code = 'NOR'`,
		"mã lạ":          `UPDATE dpm_products SET round_no = 1000, round_type = 'CM163.999' WHERE code = 'NOR'`,
	} {
		if _, err := db.Exec(stmt); err == nil {
			t.Fatalf("%s phải bị CHECK từ chối", name)
		}
	}
	// Yêu cầu sản phẩm mang round_no/round_type và đọc lại đúng.
	req, err := repo.InsertProductRequest(ctx, &repository.ProductRequest{TenantID: caseTestTenant, RequestType: "REGISTER", ProductCode: "RQ",
		Name: "Yeu cau", TermMonths: 12, InterestRate: 6, CurrencyCode: "VND", RoundNo: &no, RoundType: &kind, CreatedBy: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	back, err := repo.GetProductRequest(ctx, caseTestTenant, req.ID)
	if err != nil || back.RoundNo == nil || *back.RoundNo != 1000 || back.RoundType == nil || *back.RoundType != "CM163.001" {
		t.Fatalf("yêu cầu đọc lại = %+v, %v", back, err)
	}
}
