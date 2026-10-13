package service

// Bảng case chuẩn — tiền trong capital-service (CFM hợp đồng vốn).
//
// Khảo sát: capital không tính lãi/lịch trong Go (interest_rate chỉ lưu trên
// hợp đồng); phép tính tiền duy nhất là:
//   1. Bút toán theo loại phát sinh (RECEIPT / DISBURSEMENT / PAYMENT /
//      SETTLEMENT) — 2 leg, classification theo docs/accounting-rule-cards.md
//      (CFC_* provisional) và seed 20260911020000_cfc_accounts_rules.sql.
//   2. Số dư khả dụng hợp đồng = Σ RECEIPT đã POSTED − Σ dòng ra đã POSTED
//      (PostedMovementTotals, SQL); dòng ra không được vượt số dư; RECEIPT
//      luôn được nhận. Phần này cần DB (ARDA_TEST_DSN).
// Số tiền là minor unit VND; ngày là hằng số.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/arda-labs/arda/apps/capital-service/internal/migration"
	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
)

func TestGoldenMovementLines(t *testing.T) {
	contract := &repository.CapitalContract{ContractCode: "HDV-001", FundTypeCode: "NV-TW", OrgCode: "HO"}
	cases := []struct {
		movementType string
		amount       int64
		wantDoc      string
		wantDR       string
		wantCR       string
	}{
		{"RECEIPT", 5_000_000_000, "CFC_RECEIPT", "CASH_SETTLEMENT_ACCOUNT", "FUND_CAPITAL_LIABILITY"},
		{"DISBURSEMENT", 2_000_000_000, "CFC_DISBURSEMENT", "FUND_CAPITAL_LIABILITY", "CASH_SETTLEMENT_ACCOUNT"},
		{"PAYMENT", 37_500_000, "CFC_PAYMENT", "CFC_INTEREST_EXPENSE", "CASH_SETTLEMENT_ACCOUNT"},
		{"SETTLEMENT", 3_000_000_000, "CFC_SETTLEMENT", "FUND_CAPITAL_LIABILITY", "CASH_SETTLEMENT_ACCOUNT"},
	}
	for _, tc := range cases {
		t.Run("phát sinh "+tc.movementType, func(t *testing.T) {
			docType, lines := movementLines(contract, &repository.CapitalMovement{MovementType: tc.movementType, AmountMinor: tc.amount, CurrencyCode: "VND"})
			if docType != tc.wantDoc || len(lines) != 2 {
				t.Fatalf("doc %s, %d dòng; muốn %s, 2 dòng", docType, len(lines), tc.wantDoc)
			}
			dr, cr := lines[0], lines[1]
			if dr.GetLineNo() != 1 || dr.GetDirection() != "DEBIT" || dr.GetAmountMinor() != tc.amount || dr.GetAnalytics().GetAccClassification() != tc.wantDR {
				t.Fatalf("dòng Nợ = %+v, muốn %s %d", dr, tc.wantDR, tc.amount)
			}
			if cr.GetLineNo() != 2 || cr.GetDirection() != "CREDIT" || cr.GetAmountMinor() != tc.amount || cr.GetAnalytics().GetAccClassification() != tc.wantCR {
				t.Fatalf("dòng Có = %+v, muốn %s %d", cr, tc.wantCR, tc.amount)
			}
			for _, l := range lines {
				a := l.GetAnalytics()
				if a.GetFundSourceCode() != "NV-TW" || a.GetOrgUnitCode() != "HO" || a.GetDimensions()["contract_code"] != "HDV-001" || l.GetCurrencyCode() != "VND" {
					t.Fatalf("thiếu nguồn vốn/đơn vị/hợp đồng/tiền tệ: %+v", l)
				}
			}
		})
	}
	if doc, _, _ := movementCard("UNKNOWN"); doc != "CFC_RECEIPT" {
		t.Fatalf("loại lạ rơi về CFC_RECEIPT, nhận %s", doc)
	}
}

const goldenCfcTenant = "00000000-0000-0000-0000-000000000010"

func seedGoldenCapitalContract(t *testing.T) (*CapitalService, *repository.CapitalRepository, string) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	t.Cleanup(func() { _ = db.Close() })
	repo := repository.NewCapitalRepository(db)
	ctx := context.Background()
	ft, err := repo.CreateFundType(ctx, &repository.FundType{TenantID: goldenCfcTenant, Code: "NV-TW", Name: "Vốn TW", IsActive: true, CreatedBy: "golden"})
	if err != nil {
		t.Fatalf("seed fund type: %v", err)
	}
	// ID đặt tường minh dạng UUID: xem TestGoldenCapitalDefaultIDsAreNotUUID.
	contract, err := repo.CreateContract(ctx, &repository.CapitalContract{
		ID: "11111111-1111-4111-8111-111111111111", TenantID: goldenCfcTenant, ContractCode: "HDV-G", FundTypeCode: ft.Code, CounterpartyCode: "NHNN",
		ContractDate: "2026-09-01", AmountMinor: 10_000_000_000, InterestRate: 4.5, CurrencyCode: "VND", Status: "ACTIVE", CreatedBy: "golden",
	})
	if err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	return NewCapitalService(repo, db, nil, nil), repo, contract.ID
}

var goldenMovementSeq int

func seedMovement(t *testing.T, repo *repository.CapitalRepository, contractID, movementType string, amount int64, status string) {
	t.Helper()
	goldenMovementSeq++
	if _, err := repo.CreateMovement(context.Background(), &repository.CapitalMovement{
		ID: fmt.Sprintf("22222222-2222-4222-8222-%012d", goldenMovementSeq), TenantID: goldenCfcTenant, ContractID: contractID, MovementType: movementType, AmountMinor: amount,
		CurrencyCode: "VND", MovementDate: "2026-09-05", Status: status, CreatedBy: "golden",
	}); err != nil {
		t.Fatalf("seed movement %s %d %s: %v", movementType, amount, status, err)
	}
}

func TestGoldenCapitalAvailability(t *testing.T) {
	type mv struct {
		kind   string
		amount int64
		status string
	}
	cases := []struct {
		name          string
		history       []mv
		wantReceipts  int64
		wantOutflows  int64
		allowed       []mv // (kind, amount) phải qua
		rejected      []mv // (kind, amount) phải bị từ chối
	}{
		{"chưa có tiếp nhận: mọi dòng ra bị từ chối, RECEIPT vẫn được", nil, 0, 0,
			[]mv{{"RECEIPT", 1, ""}}, []mv{{"DISBURSEMENT", 1, ""}, {"PAYMENT", 1, ""}, {"SETTLEMENT", 1, ""}}},
		{"tiếp nhận 5 tỷ POSTED: giải ngân đúng 5 tỷ qua, 5 tỷ + 1 đồng bị từ chối",
			[]mv{{"RECEIPT", 5_000_000_000, "POSTED"}}, 5_000_000_000, 0,
			[]mv{{"DISBURSEMENT", 5_000_000_000, ""}}, []mv{{"DISBURSEMENT", 5_000_000_001, ""}}},
		{"tiếp nhận DRAFT/SUBMITTED chưa POSTED không tính vào số dư",
			[]mv{{"RECEIPT", 5_000_000_000, "DRAFT"}, {"RECEIPT", 1_000_000_000, "SUBMITTED"}}, 0, 0,
			nil, []mv{{"DISBURSEMENT", 1, ""}}},
		{"5 tỷ vào, giải ngân 2 tỷ + trả lãi 37,5 triệu POSTED: còn 2.962.500.000",
			[]mv{{"RECEIPT", 5_000_000_000, "POSTED"}, {"DISBURSEMENT", 2_000_000_000, "POSTED"}, {"PAYMENT", 37_500_000, "POSTED"}},
			5_000_000_000, 2_037_500_000,
			[]mv{{"SETTLEMENT", 2_962_500_000, ""}}, []mv{{"SETTLEMENT", 2_962_500_001, ""}}},
		{"dòng ra REJECTED không trừ số dư",
			[]mv{{"RECEIPT", 1_000_000, "POSTED"}, {"DISBURSEMENT", 900_000, "REJECTED"}}, 1_000_000, 0,
			[]mv{{"DISBURSEMENT", 1_000_000, ""}}, nil},
		{"hai lần tiếp nhận cộng dồn: 3 + 2 = 5 tỷ",
			[]mv{{"RECEIPT", 3_000_000_000, "POSTED"}, {"RECEIPT", 2_000_000_000, "POSTED"}}, 5_000_000_000, 0,
			[]mv{{"PAYMENT", 5_000_000_000, ""}}, []mv{{"PAYMENT", 5_000_000_001, ""}}},
		{"tất toán hết: số dư 0, không ra thêm được",
			[]mv{{"RECEIPT", 1_000_000, "POSTED"}, {"SETTLEMENT", 1_000_000, "POSTED"}}, 1_000_000, 1_000_000,
			[]mv{{"RECEIPT", 1, ""}}, []mv{{"PAYMENT", 1, ""}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, contractID := seedGoldenCapitalContract(t)
			for _, m := range tc.history {
				seedMovement(t, repo, contractID, m.kind, m.amount, m.status)
			}
			ctx := context.Background()
			receipts, outflows, err := repo.PostedMovementTotals(ctx, goldenCfcTenant, contractID)
			if err != nil {
				t.Fatal(err)
			}
			if receipts != tc.wantReceipts || outflows != tc.wantOutflows {
				t.Fatalf("totals = %d vào / %d ra, muốn %d / %d", receipts, outflows, tc.wantReceipts, tc.wantOutflows)
			}
			for _, m := range tc.allowed {
				if err := svc.checkAvailability(ctx, goldenCfcTenant, contractID, m.kind, m.amount); err != nil {
					t.Fatalf("%s %d phải qua: %v", m.kind, m.amount, err)
				}
			}
			for _, m := range tc.rejected {
				err := svc.checkAvailability(ctx, goldenCfcTenant, contractID, m.kind, m.amount)
				var appErr *ardaerrors.Error
				if err == nil || !errors.As(err, &appErr) || appErr.Code != ardaerrors.CodeInvalidInput {
					t.Fatalf("%s %d phải bị từ chối CodeInvalidInput, nhận %v", m.kind, m.amount, err)
				}
			}
		})
	}
}

// Nghi bug: cfc_contracts.id và cfc_movements.id là UUID (uuidv7()) nhưng
// repository.CreateContract (ID rỗng) và CapitalService.RecordMovement sinh id
// dạng "cfc_<hex>" / "cfcmv_<hex>" qua NewCapitalID → INSERT lỗi
// "invalid input syntax for type uuid" (SQLSTATE 22P02). Luồng tạo hợp đồng
// vốn và ghi phát sinh qua service hiện không thể thành công trên schema này.
func TestGoldenCapitalDefaultIDsAreNotUUID(t *testing.T) {
	t.Skip("BUG-13 (nghi): NewCapitalID sinh id không phải UUID cho cfc_contracts/cfc_movements (cột UUID) — CreateContract/RecordMovement fail SQLSTATE 22P02")
	_, repo, _ := seedGoldenCapitalContract(t)
	if _, err := repo.CreateContract(context.Background(), &repository.CapitalContract{
		TenantID: goldenCfcTenant, ContractCode: "HDV-DEFAULT-ID", FundTypeCode: "NV-TW", CounterpartyCode: "NHNN",
		ContractDate: "2026-09-01", AmountMinor: 1, CurrencyCode: "VND", Status: "ACTIVE", CreatedBy: "golden",
	}); err != nil {
		t.Fatalf("CreateContract với ID mặc định phải thành công: %v", err)
	}
}
