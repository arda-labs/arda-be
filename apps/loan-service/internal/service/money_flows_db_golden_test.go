package service

// Bảng case chuẩn (cần DB) — các luồng tiền của loan-service mà phép tính
// nằm trong SQL hoặc gắn với trạng thái bảng:
//
//   1. Thu nợ (LNM.301.02, SettleCollectionTx): dư nợ giảm đúng gốc thu, không
//      âm (GREATEST(…,0)), lũy kế gốc/lãi thu tăng, khế ước CLOSED khi dư nợ
//      về 0; settle lặp không trừ hai lần (BUG-1).
//   2. Giải ngân hai pha (LNM.300.02): REGISTER settle cộng "đang chuyển",
//      COMPLETE settle chuyển "đang chuyển" sang dư nợ; hạn mức còn lại =
//      số tiền vay − (dư nợ + đang chuyển + giữ chỗ) chỉ tính mỗi phần một
//      lần (BUG-2/BUG-4).
//   3. Batch EOD lãi (LNM.301.01) và dự phòng (LNM.307.01): số tiền theo ngày
//      chạy, ngày giải ngân và lần tính trước; chạy lại cùng ngày không tạo
//      bút toán mới.
//
// Chạy khi ARDA_TEST_DSN trỏ tới Postgres 18 (testdb tạo DB tạm). Finance được
// thay bằng fake ghi lại PostingRequest để khóa số tiền gửi đi.
// Số tiền là minor unit VND; mọi ngày là hằng số (không dùng time.Now).

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

const goldenTenant = "00000000-0000-0000-0000-000000000010"

func openGoldenLoanDB(t *testing.T) (*sql.DB, *repository.LoanRepository) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	t.Cleanup(func() { _ = db.Close() })
	return db, repository.NewLoanRepository(db)
}

// seedGoldenAgreement creates a contract + ACTIVE agreement and sets the
// balances directly (the fixture is the business state, not a flow).
func seedGoldenAgreement(t *testing.T, db *sql.DB, repo *repository.LoanRepository, loanAmt, outstanding, pending int64, rate float64, debtGroup, disburseDate string) (contractCode, agreementCode string) {
	t.Helper()
	run := repository.NewID("g")
	contractCode, agreementCode = "GC-"+run, "GA-"+run
	ctx := context.Background()
	if _, err := repo.CreateContract(ctx, &domain.Contract{
		ID: repository.NewID("ctrt"), TenantID: goldenTenant, ContractCode: contractCode,
		CustomerCode: "KH-1", LoanAmt: loanAmt, LoanTerm: 12, TermUnit: "MONTH",
		ContractDate: "2026-09-01", MaturityDate: "2027-09-01", Status: domain.ContractApproved, CreatedBy: "golden",
	}); err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	if _, err := repo.CreateAgreement(ctx, &domain.Agreement{
		ID: repository.NewID("agr"), TenantID: goldenTenant, ContractCode: contractCode,
		AgreementCode: agreementCode, DisburseDate: disburseDate, LoanTerm: 12, TermUnit: "MONTH",
		MaturityDate: "2027-09-01", Status: domain.AgreementActive, CreatedBy: "golden",
	}); err != nil {
		t.Fatalf("seed agreement: %v", err)
	}
	if _, err := db.Exec(`UPDATE lnm_agreements
		SET outstanding_amt_minor = $3, pending_disburse_amt_minor = $4, interest_rate = $5, debt_group_code = $6
		WHERE tenant_id = $1 AND agreement_code = $2`, goldenTenant, agreementCode, outstanding, pending, rate, debtGroup); err != nil {
		t.Fatalf("seed balances: %v", err)
	}
	return contractCode, agreementCode
}

type agreementBalances struct {
	outstanding, pending, colnPrincipal, colnInterest int64
	status                                            string
}

func readGoldenAgreement(t *testing.T, db *sql.DB, agreementCode string) agreementBalances {
	t.Helper()
	var b agreementBalances
	if err := db.QueryRow(`SELECT outstanding_amt_minor, pending_disburse_amt_minor, coln_principal_amt_minor, coln_interest_amt_minor, status
		FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`, goldenTenant, agreementCode).
		Scan(&b.outstanding, &b.pending, &b.colnPrincipal, &b.colnInterest, &b.status); err != nil {
		t.Fatalf("read agreement: %v", err)
	}
	return b
}

// ── 1. Thu nợ ──

func TestGoldenCollectionSettleBalances(t *testing.T) {
	cases := []struct {
		name                string
		outstanding         int64
		principal, interest int64
		want                agreementBalances
	}{
		{"thu một phần gốc 300 + lãi 50 trên dư nợ 1.000: dư nợ còn 700, ACTIVE", 1_000, 300, 50,
			agreementBalances{outstanding: 700, colnPrincipal: 300, colnInterest: 50, status: "ACTIVE"}},
		{"thu đúng toàn bộ gốc 1.000: dư nợ 0, khế ước CLOSED", 1_000, 1_000, 120,
			agreementBalances{outstanding: 0, colnPrincipal: 1_000, colnInterest: 120, status: "CLOSED"}},
		{"chỉ thu lãi 50: dư nợ giữ nguyên, lũy kế lãi tăng", 1_000, 0, 50,
			agreementBalances{outstanding: 1_000, colnPrincipal: 0, colnInterest: 50, status: "ACTIVE"}},
		{"chỉ thu gốc, không lãi", 1_000, 400, 0,
			agreementBalances{outstanding: 600, colnPrincipal: 400, colnInterest: 0, status: "ACTIVE"}},
		// Service đơn không chặn gốc thu > dư nợ (batch thì chặn ở Create); SQL
		// kẹp dư nợ về 0 nhưng lũy kế gốc thu vẫn ghi đủ 1.200 → lệch 200 so
		// với dư nợ thực. Cần nghiệp vụ chốt: từ chối hay ghi trả thừa riêng.
		{"hiện trạng, cần xác nhận nghiệp vụ: thu gốc 1.200 > dư nợ 1.000 → dư nợ kẹp 0, CLOSED, lũy kế gốc ghi 1.200", 1_000, 1_200, 0,
			agreementBalances{outstanding: 0, colnPrincipal: 1_200, colnInterest: 0, status: "CLOSED"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, repo := openGoldenLoanDB(t)
			contractCode, agreementCode := seedGoldenAgreement(t, db, repo, 1_000, tc.outstanding, 0, 12, "GROUP_1", "2026-09-01")
			var collectionID string
			if err := db.QueryRow(`INSERT INTO lnm_collections
				(tenant_id, contract_code, agreement_code, collection_date, principal_minor, interest_minor, currency_code, status, created_by)
				VALUES ($1,$2,$3,'2026-09-07',$4,$5,'VND','APPROVED','golden') RETURNING id::text`,
				goldenTenant, contractCode, agreementCode, tc.principal, tc.interest).Scan(&collectionID); err != nil {
				t.Fatalf("seed collection: %v", err)
			}
			svc := NewCollectionService(repo, nil)
			ctx := context.Background()
			if err := svc.Settle(ctx, goldenTenant, collectionID, "11111111-1111-4111-8111-111111111111", "checker"); err != nil {
				t.Fatalf("settle: %v", err)
			}
			if got := readGoldenAgreement(t, db, agreementCode); got != tc.want {
				t.Fatalf("sau settle = %+v, muốn %+v", got, tc.want)
			}
			// BUG-1: settle lặp (worker retry) không được trừ nợ lần hai.
			if err := svc.Settle(ctx, goldenTenant, collectionID, "11111111-1111-4111-8111-111111111111", "checker"); err != nil {
				t.Fatalf("settle lặp: %v", err)
			}
			if got := readGoldenAgreement(t, db, agreementCode); got != tc.want {
				t.Fatalf("settle lặp đổi số dư: %+v, muốn giữ %+v", got, tc.want)
			}
		})
	}
}

// ── 2. Giải ngân hai pha + hạn mức ──

func seedApprovedDisbursement(t *testing.T, db *sql.DB, contractCode, agreementCode, flow string, amount int64, sourceRegisterID string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO lnm_disbursements
		(tenant_id, contract_code, agreement_code, disburse_date, disburse_amt_minor, currency_code, flow_type, source_register_id, status, created_by)
		VALUES ($1,$2,$3,'2026-09-07',$4,'VND',$5,NULLIF($6,'')::uuid,'APPROVED','golden') RETURNING id::text`,
		goldenTenant, contractCode, agreementCode, amount, flow, sourceRegisterID).Scan(&id); err != nil {
		t.Fatalf("seed %s disbursement: %v", flow, err)
	}
	return id
}

func holdReservation(t *testing.T, db *sql.DB, contractCode, sourceID string, amount int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO lnm_contract_reservations (tenant_id, contract_code, source_type, source_id, amount_minor, status)
		VALUES ($1,$2,'DISBURSEMENT',$3::uuid,$4,'HELD')`, goldenTenant, contractCode, sourceID, amount); err != nil {
		t.Fatalf("hold reservation: %v", err)
	}
}

func TestGoldenTwoPhaseDisbursementExposure(t *testing.T) {
	db, repo := openGoldenLoanDB(t)
	ctx := context.Background()
	const loanAmt = 1_000_000_000
	contractCode, agreementCode := seedGoldenAgreement(t, db, repo, loanAmt, 0, 0, 12, "GROUP_1", "2026-09-01")

	type step struct {
		name         string
		run          func(t *testing.T)
		wantAgree    agreementBalances
		wantExposure domain.ContractExposure
	}
	registerID := seedApprovedDisbursement(t, db, contractCode, agreementCode, domain.FlowRegister, 600_000_000, "")
	holdReservation(t, db, contractCode, registerID, 600_000_000)
	var completeID string

	steps := []step{
		{
			name: "REGISTER 600 triệu đang chờ duyệt: giữ chỗ 600, dư nợ/đang chuyển 0, còn 400 triệu",
			run:  func(t *testing.T) {},
			wantAgree:    agreementBalances{status: "ACTIVE"},
			wantExposure: domain.ContractExposure{LoanAmountMinor: loanAmt, ReservedMinor: 600_000_000},
		},
		{
			name: "REGISTER settle: giữ chỗ → đang chuyển 600 triệu, dư nợ vẫn 0, còn 400 triệu (không tính đôi)",
			run: func(t *testing.T) {
				applied, err := repo.SettleDisbursementRegister(ctx, goldenTenant, registerID, agreementCode, 600_000_000, "", "checker")
				if err != nil || !applied {
					t.Fatalf("settle register = %v, %v", applied, err)
				}
			},
			wantAgree:    agreementBalances{pending: 600_000_000, status: "ACTIVE"},
			wantExposure: domain.ContractExposure{LoanAmountMinor: loanAmt, PendingMinor: 600_000_000},
		},
		{
			name: "REGISTER settle lặp: không cộng đang chuyển lần hai",
			run: func(t *testing.T) {
				applied, err := repo.SettleDisbursementRegister(ctx, goldenTenant, registerID, agreementCode, 600_000_000, "", "checker")
				if err != nil || applied {
					t.Fatalf("settle register lặp = %v, %v; muốn (false, nil)", applied, err)
				}
			},
			wantAgree:    agreementBalances{pending: 600_000_000, status: "ACTIVE"},
			wantExposure: domain.ContractExposure{LoanAmountMinor: loanAmt, PendingMinor: 600_000_000},
		},
		{
			name: "COMPLETE 250 triệu settle: đang chuyển còn 350, dư nợ 250, còn hạn mức vẫn 400 (COMPLETE không tiêu hạn mức)",
			run: func(t *testing.T) {
				completeID = seedApprovedDisbursement(t, db, contractCode, agreementCode, domain.FlowComplete, 250_000_000, registerID)
				applied, err := repo.SettleDisbursementComplete(ctx, goldenTenant, completeID, contractCode, agreementCode, 250_000_000, "", "checker")
				if err != nil || !applied {
					t.Fatalf("settle complete = %v, %v", applied, err)
				}
			},
			wantAgree:    agreementBalances{outstanding: 250_000_000, pending: 350_000_000, status: "ACTIVE"},
			wantExposure: domain.ContractExposure{LoanAmountMinor: loanAmt, OutstandingMinor: 250_000_000, PendingMinor: 350_000_000},
		},
		{
			name: "COMPLETE vượt phần đang chuyển (400 > 350) bị từ chối, số dư không đổi",
			run: func(t *testing.T) {
				overID := seedApprovedDisbursement(t, db, contractCode, agreementCode, domain.FlowComplete, 400_000_000, registerID)
				if _, err := repo.SettleDisbursementComplete(ctx, goldenTenant, overID, contractCode, agreementCode, 400_000_000, "", "checker"); err == nil {
					t.Fatal("COMPLETE vượt phần đang chuyển phải lỗi")
				}
			},
			wantAgree:    agreementBalances{outstanding: 250_000_000, pending: 350_000_000, status: "ACTIVE"},
			wantExposure: domain.ContractExposure{LoanAmountMinor: loanAmt, OutstandingMinor: 250_000_000, PendingMinor: 350_000_000},
		},
		{
			name: "REGISTER thứ hai 400 triệu giữ chỗ: dùng hết hạn mức, còn 0; REGISTER thứ ba 1 đồng bị từ chối",
			run: func(t *testing.T) {
				secondID := seedApprovedDisbursement(t, db, contractCode, agreementCode, domain.FlowRegister, 400_000_000, "")
				holdReservation(t, db, contractCode, secondID, 400_000_000)
				exposure, err := repo.GetContractExposure(ctx, goldenTenant, contractCode)
				if err != nil {
					t.Fatal(err)
				}
				if exposure.Allows(1) {
					t.Fatalf("hạn mức đã hết nhưng vẫn cho REGISTER 1 đồng: %+v", exposure)
				}
			},
			wantAgree:    agreementBalances{outstanding: 250_000_000, pending: 350_000_000, status: "ACTIVE"},
			wantExposure: domain.ContractExposure{LoanAmountMinor: loanAmt, OutstandingMinor: 250_000_000, PendingMinor: 350_000_000, ReservedMinor: 400_000_000},
		},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			st.run(t)
			got := readGoldenAgreement(t, db, agreementCode)
			if got != st.wantAgree {
				t.Fatalf("số dư khế ước = %+v, muốn %+v", got, st.wantAgree)
			}
			exposure, err := repo.GetContractExposure(ctx, goldenTenant, contractCode)
			if err != nil {
				t.Fatalf("exposure: %v", err)
			}
			if exposure != st.wantExposure {
				t.Fatalf("exposure = %+v, muốn %+v", exposure, st.wantExposure)
			}
			if exposure.HeadroomMinor() != loanAmt-exposure.UsedMinor() {
				t.Fatalf("headroom %d ≠ loan − used %d", exposure.HeadroomMinor(), loanAmt-exposure.UsedMinor())
			}
		})
	}
}

// ── 3. Batch EOD ──

// recordingFinance accepts every posting and records the amount per
// idempotency key so the table can assert what was sent to finance.
type recordingFinance struct {
	posts map[string]*financev1.PostingRequest
	next  int
}

func newRecordingFinance() *recordingFinance {
	return &recordingFinance{posts: map[string]*financev1.PostingRequest{}}
}

func (f *recordingFinance) Post(_ context.Context, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	if _, seen := f.posts[req.GetIdempotencyKey()]; !seen {
		f.next++
		f.posts[req.GetIdempotencyKey()] = req
	}
	return &financev1.PostingResponse{JournalEntryId: fmt.Sprintf("00000000-0000-7000-8000-%012d", f.next)}, nil
}

func (f *recordingFinance) ListPostingRules(_ context.Context, documentType string) ([]*financev1.PostingRule, error) {
	return goldenLoanCards[documentType], nil
}

func (f *recordingFinance) sumFor(agreementCode string) (total int64, count int) {
	for _, req := range f.posts {
		if req.GetBusinessReference().GetDocumentCode() != agreementCode {
			continue
		}
		count++
		for _, line := range req.GetLines() {
			if line.GetDirection() == "DEBIT" {
				total += line.GetAmountMinor()
			}
		}
	}
	return total, count
}

func TestGoldenAccrualRunDailyAmounts(t *testing.T) {
	cases := []struct {
		name         string
		outstanding  int64
		rate         float64
		disburseDate string
		runs         []string // ngày chạy EOD liên tiếp
		wantPerRun   []int64  // lãi ghi nhận mỗi lần chạy (0 = bỏ qua)
		wantPosted   int      // số bút toán gửi finance
	}{
		{
			// 500 triệu × 8,5% × 6/30 /1200 = 708.333,33 → 708.333
			name: "giải ngân 01/09, chạy 07/09: 6 ngày lãi = 708.333", outstanding: 500_000_000, rate: 8.5, disburseDate: "2026-09-01",
			runs: []string{"2026-09-07"}, wantPerRun: []int64{708_333}, wantPosted: 1,
		},
		{
			name: "chạy lại đúng ngày đã tính: 0 ngày, bỏ qua, không bút toán mới", outstanding: 500_000_000, rate: 8.5, disburseDate: "2026-09-01",
			runs: []string{"2026-09-07", "2026-09-07"}, wantPerRun: []int64{708_333, 0}, wantPosted: 1,
		},
		{
			// lần 2: từ 07/09 đến 30/09 = 23 ngày: 500tr × 8,5 × 23/30 /1200 = 2.715.277,78 → 2.715.278
			name: "hai lần chạy nối tiếp: 6 ngày rồi 23 ngày, tổng 29 ngày", outstanding: 500_000_000, rate: 8.5, disburseDate: "2026-09-01",
			runs: []string{"2026-09-07", "2026-09-30"}, wantPerRun: []int64{708_333, 2_715_278}, wantPosted: 2,
		},
		{
			// 1 tỷ × 12% × 30/30 /1200 = 10.000.000
			name: "tròn 30 ngày 1 tỷ 12% = 10 triệu", outstanding: 1_000_000_000, rate: 12, disburseDate: "2026-09-01",
			runs: []string{"2026-10-01"}, wantPerRun: []int64{10_000_000}, wantPosted: 1,
		},
		{
			name: "chạy trước ngày giải ngân: số ngày âm, bỏ qua", outstanding: 1_000_000_000, rate: 12, disburseDate: "2026-09-10",
			runs: []string{"2026-09-07"}, wantPerRun: []int64{0}, wantPosted: 0,
		},
		{
			name: "lãi suất 0: không bút toán", outstanding: 1_000_000_000, rate: 0, disburseDate: "2026-09-01",
			runs: []string{"2026-10-01"}, wantPerRun: []int64{0}, wantPosted: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, repo := openGoldenLoanDB(t)
			_, agreementCode := seedGoldenAgreement(t, db, repo, 2_000_000_000, tc.outstanding, 0, tc.rate, "GROUP_1", tc.disburseDate)
			fake := newRecordingFinance()
			svc := NewAccrualService(repo, db, nil)
			svc.finance = fake
			var total int64
			for i, day := range tc.runs {
				before := total
				result, err := svc.RunDaily(context.Background(), goldenTenant, day, "eod")
				if err != nil {
					t.Fatalf("run %s: %v", day, err)
				}
				if len(result.FailedDetail) != 0 {
					t.Fatalf("run %s failed: %v", day, result.FailedDetail)
				}
				total += result.TotalMinor
				if got := total - before; got != tc.wantPerRun[i] {
					t.Fatalf("lần chạy %s ghi lãi %d, muốn %d", day, got, tc.wantPerRun[i])
				}
			}
			sent, count := fake.sumFor(agreementCode)
			if count != tc.wantPosted || sent != total {
				t.Fatalf("finance nhận %d bút toán tổng %d, muốn %d bút toán tổng %d", count, sent, tc.wantPosted, total)
			}
			var stored int64
			if err := db.QueryRow(`SELECT COALESCE(SUM(interest_minor),0) FROM lnm_accruals WHERE tenant_id=$1 AND agreement_code=$2 AND status='POSTED'`, goldenTenant, agreementCode).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored != total {
				t.Fatalf("lnm_accruals POSTED tổng %d, muốn %d", stored, total)
			}
		})
	}
}

func TestGoldenProvisionRunAmounts(t *testing.T) {
	type run struct {
		date        string
		outstanding int64  // dư nợ trước khi chạy (cập nhật trực tiếp)
		debtGroup   string // nhóm nợ trước khi chạy
		wantNet     int64  // delta ghi nhận lần này
	}
	cases := []struct {
		name string
		runs []run
	}{
		{"nhóm 3 dư nợ 500 triệu: kỳ đầu trích 100 triệu", []run{
			{"2026-09-30", 500_000_000, "GROUP_3", 100_000_000},
		}},
		{"chạy lại cùng ngày: đã có kỳ POSTED, bỏ qua (0)", []run{
			{"2026-09-30", 500_000_000, "GROUP_3", 100_000_000},
			{"2026-09-30", 500_000_000, "GROUP_3", 0},
		}},
		{"dư nợ giảm 500 → 400 triệu: hoàn nhập 20 triệu", []run{
			{"2026-09-30", 500_000_000, "GROUP_3", 100_000_000},
			{"2026-10-31", 400_000_000, "GROUP_3", -20_000_000},
		}},
		{"chuyển nhóm 3 → nhóm 4: trích thêm 150 triệu (250 − 100)", []run{
			{"2026-09-30", 500_000_000, "GROUP_3", 100_000_000},
			{"2026-10-31", 500_000_000, "GROUP_4", 150_000_000},
		}},
		{"chuyển nhóm 3 → nhóm 1: hoàn toàn bộ 100 triệu", []run{
			{"2026-09-30", 500_000_000, "GROUP_3", 100_000_000},
			{"2026-10-31", 500_000_000, "GROUP_1", -100_000_000},
		}},
		{"nhóm 1 ngay từ đầu: không trích, không bút toán", []run{
			{"2026-09-30", 500_000_000, "GROUP_1", 0},
		}},
		{"nhóm nợ không có tỷ lệ cấu hình: bỏ qua", []run{
			{"2026-09-30", 500_000_000, "GROUP_9", 0},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, repo := openGoldenLoanDB(t)
			_, agreementCode := seedGoldenAgreement(t, db, repo, 2_000_000_000, tc.runs[0].outstanding, 0, 10, tc.runs[0].debtGroup, "2026-09-01")
			fake := newRecordingFinance()
			svc := NewProvisionService(repo, db, nil)
			svc.finance = fake
			var accumulated int64
			for _, r := range tc.runs {
				if _, err := db.Exec(`UPDATE lnm_agreements SET outstanding_amt_minor=$3, debt_group_code=$4 WHERE tenant_id=$1 AND agreement_code=$2`,
					goldenTenant, agreementCode, r.outstanding, r.debtGroup); err != nil {
					t.Fatal(err)
				}
				result, err := svc.Run(context.Background(), goldenTenant, r.date, "eod")
				if err != nil {
					t.Fatalf("run %s: %v", r.date, err)
				}
				if len(result.FailedAgmt) != 0 {
					t.Fatalf("run %s failed: %v", r.date, result.FailedAgmt)
				}
				if result.NetMinor != r.wantNet {
					t.Fatalf("lần chạy %s delta %d, muốn %d", r.date, result.NetMinor, r.wantNet)
				}
				accumulated += r.wantNet
			}
			var stored sql.NullInt64
			if err := db.QueryRow(`SELECT SUM(delta_minor) FROM lnm_provisions WHERE tenant_id=$1 AND agreement_code=$2 AND status='POSTED'`, goldenTenant, agreementCode).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored.Int64 != accumulated {
				t.Fatalf("lũy kế dự phòng trong DB %d, muốn %d", stored.Int64, accumulated)
			}
			// Mỗi bút toán gửi finance mang đúng |delta| ở dòng Nợ.
			var wantSent int64
			for _, r := range tc.runs {
				if r.wantNet < 0 {
					wantSent -= r.wantNet
				} else {
					wantSent += r.wantNet
				}
			}
			if sent, _ := fake.sumFor(agreementCode); sent != wantSent {
				t.Fatalf("finance nhận tổng Nợ %d, muốn %d", sent, wantSent)
			}
		})
	}
}
