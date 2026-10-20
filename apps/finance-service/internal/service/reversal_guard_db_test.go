package service

// Chặn đảo bút toán qua finance (AP-REV-01, AP-REV-02) — cần DB
// (ARDA_TEST_DSN trỏ Postgres 18, xem posting_golden_db_test.go):
//
//   - AP-REV-01: chứng từ sinh từ nghiệp vụ domain (lnm/dep/cap/accrual...)
//     không đảo thẳng qua finance được, vì đảo sổ cái không quay lại sổ chi
//     tiết nghiệp vụ; chỉ chứng từ thủ công FIN_* trong danh mục được hủy
//     (tương đương com_cfg_process_type.is_cancel của EPAS).
//   - AP-REV-02: dòng đảo không được làm số dư thực tế/khả dụng âm (trừ tài
//     khoản lưỡng tính nature B) — EPAS ActualBalanceValidator.

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	workflowv1 "github.com/arda-labs/arda/libs/go/arda-proto/workflow/v1"
)

func goldenManualRequest(key, date string, lines ...*financev1.PostingLine) *financev1.PostingRequest {
	return &financev1.PostingRequest{IdempotencyKey: key, AccountingDate: date, CurrencyCode: "VND", Description: key,
		BusinessReference: &financev1.BusinessReference{Domain: "fin", DocumentType: "FIN_DOUBLE_ENTRY", DocumentCode: key}, Lines: lines}
}

func reverseForTest(svc *PostingService, entryID, key string) error {
	_, err := svc.ReverseTransaction(context.Background(), &financev1.ReverseRequest{TenantId: goldenFinTenant,
		JournalEntryId: entryID, Reason: "test", IdempotencyKey: key, Actor: "test", AccountingDate: "2026-09-08"})
	return err
}

func entryStatus(t *testing.T, svc *PostingService, entryID string) string {
	t.Helper()
	var status string
	if err := svc.db.QueryRow(`SELECT status FROM fin_journal_entries WHERE id = $1`, entryID).Scan(&status); err != nil {
		t.Fatalf("reload entry: %v", err)
	}
	return status
}

func TestReverseRejectsDomainDocument(t *testing.T) {
	_, svc := openGoldenFinance(t)
	ctx := context.Background()
	disb, err := svc.PostTransaction(ctx, goldenFinTenant, goldenRequest("rg-disb", "2026-09-05",
		goldenLine(1, "DEBIT", 600_000, "LNM_LOAN_PRINCIPAL", "", ""), goldenLine(2, "CREDIT", 600_000, "CASH_SETTLEMENT_ACCOUNT", "", "")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	err = reverseForTest(svc, disb.GetJournalEntryId(), "rg-disb-rev")
	if err == nil || !strings.Contains(err.Error(), "REVERSAL_NOT_ALLOWED") {
		t.Fatalf("đảo chứng từ lnm phải bị từ chối REVERSAL_NOT_ALLOWED, nhận %v", err)
	}
	if got := ClassifyPostingError(err); got != financev1.PostingErrorCode_POSTING_ERROR_CODE_REVERSAL_NOT_ALLOWED {
		t.Fatalf("mã lỗi = %s, muốn REVERSAL_NOT_ALLOWED", got)
	}
	if st := entryStatus(t, svc, disb.GetJournalEntryId()); st != "POSTED" {
		t.Fatalf("chứng từ gốc = %s, muốn vẫn POSTED", st)
	}
}

func TestReverseAllowsManualFinDocument(t *testing.T) {
	_, svc := openGoldenFinance(t)
	ctx := context.Background()
	manual, err := svc.PostTransaction(ctx, goldenFinTenant, goldenManualRequest("rg-manual", "2026-09-05",
		goldenLine(1, "DEBIT", 100_000, "LNM_LOAN_PRINCIPAL", "", ""), goldenLine(2, "CREDIT", 100_000, "CASH_SETTLEMENT_ACCOUNT", "", "")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if err := reverseForTest(svc, manual.GetJournalEntryId(), "rg-manual-rev"); err != nil {
		t.Fatalf("đảo chứng từ thủ công FIN_DOUBLE_ENTRY phải được: %v", err)
	}
	if st := entryStatus(t, svc, manual.GetJournalEntryId()); st != "REVERSED" {
		t.Fatalf("chứng từ gốc = %s, muốn REVERSED", st)
	}
}

func TestReverseHonoursCancelCatalog(t *testing.T) {
	_, svc := openGoldenFinance(t)
	ctx := context.Background()
	// Danh mục tắt hủy cho FIN_DOUBLE_ENTRY → đảo bị từ chối.
	if _, err := svc.db.Exec(`UPDATE fin_posting_policies SET allow_cancel = FALSE
		WHERE tenant_id = $1 AND business_doc_type = 'FIN_DOUBLE_ENTRY'`, goldenFinTenant); err != nil {
		t.Fatalf("policy: %v", err)
	}
	manual, err := svc.PostTransaction(ctx, goldenFinTenant, goldenManualRequest("rg-off", "2026-09-05",
		goldenLine(1, "DEBIT", 100_000, "LNM_LOAN_PRINCIPAL", "", ""), goldenLine(2, "CREDIT", 100_000, "CASH_SETTLEMENT_ACCOUNT", "", "")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if err := reverseForTest(svc, manual.GetJournalEntryId(), "rg-off-rev"); err == nil || !strings.Contains(err.Error(), "REVERSAL_NOT_ALLOWED") {
		t.Fatalf("danh mục tắt hủy phải chặn, nhận %v", err)
	}
	// Ràng buộc DB: không bật hủy được cho loại chứng từ nghiệp vụ domain.
	if _, err := svc.db.Exec(`INSERT INTO fin_posting_policies (tenant_id, business_doc_type, allow_cancel)
		VALUES ($1, 'LNM_COLLECTION', TRUE)`, goldenFinTenant); err == nil {
		t.Fatal("bật allow_cancel cho LNM_COLLECTION phải vi phạm ràng buộc")
	}
}

func TestReverseRejectsNegativeActualBalance(t *testing.T) {
	_, svc := openGoldenFinance(t)
	ctx := context.Background()
	// Nợ 1311 / Có 1131 600.000, rồi thu về Nợ 1131 / Có 1311 300.000:
	// đảo bút toán đầu sẽ làm 1311 (nature D) = 300.000 − 600.000 < 0.
	first, err := svc.PostTransaction(ctx, goldenFinTenant, goldenManualRequest("rg-neg-1", "2026-09-05",
		goldenLine(1, "DEBIT", 600_000, "LNM_LOAN_PRINCIPAL", "", ""), goldenLine(2, "CREDIT", 600_000, "CASH_SETTLEMENT_ACCOUNT", "", "")))
	if err != nil {
		t.Fatalf("post 1: %v", err)
	}
	if _, err := svc.PostTransaction(ctx, goldenFinTenant, goldenManualRequest("rg-neg-2", "2026-09-06",
		goldenLine(1, "DEBIT", 300_000, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 300_000, "LNM_LOAN_PRINCIPAL", "", ""))); err != nil {
		t.Fatalf("post 2: %v", err)
	}
	err = reverseForTest(svc, first.GetJournalEntryId(), "rg-neg-1-rev")
	if err == nil || !strings.Contains(err.Error(), "BAL_ACTUAL_IS_NOT_ENOUGH:1311") {
		t.Fatalf("đảo làm 1311 âm phải bị từ chối BAL_ACTUAL_IS_NOT_ENOUGH:1311, nhận %v", err)
	}
	if st := entryStatus(t, svc, first.GetJournalEntryId()); st != "POSTED" {
		t.Fatalf("chứng từ gốc = %s, muốn vẫn POSTED", st)
	}
	var postedCredit int64
	if err := svc.db.QueryRow(`SELECT posted_credit_minor FROM fin_account_balances
		WHERE tenant_id = $1 AND account_code = '1311' AND bal_type_code = 'ACTUAL'`, goldenFinTenant).Scan(&postedCredit); err != nil {
		t.Fatalf("balance: %v", err)
	}
	if postedCredit != 300_000 {
		t.Fatalf("1311 posted_credit = %d, muốn 300.000 (không ghi dòng đảo)", postedCredit)
	}
}

// failingWorkflow ghi nhận việc tạo case; test dùng để chứng minh case hủy
// không được mở khi chứng từ gốc không thuộc danh mục được hủy.
type failingWorkflow struct{ created int }

func (w *failingWorkflow) CreateCase(context.Context, workflowclient.CaseCreate) (*workflowv1.BusinessCase, error) {
	w.created++
	return &workflowv1.BusinessCase{Id: "case-1"}, nil
}
func (w *failingWorkflow) SubmitCase(context.Context, string, string, map[string]any, string) (*workflowv1.BusinessCase, error) {
	return &workflowv1.BusinessCase{Id: "case-1"}, nil
}

func TestCancellationCaseRejectsDomainDocumentEarly(t *testing.T) {
	_, svc := openGoldenFinance(t)
	ctx := context.Background()
	disb, err := svc.PostTransaction(ctx, goldenFinTenant, goldenRequest("rg-case", "2026-09-05",
		goldenLine(1, "DEBIT", 600_000, "LNM_LOAN_PRINCIPAL", "", ""), goldenLine(2, "CREDIT", 600_000, "CASH_SETTLEMENT_ACCOUNT", "", "")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	wf := &failingWorkflow{}
	cases := NewPostingCaseService(svc, wf)
	_, err = cases.createCancellationCase(ctx, goldenFinTenant, "maker", PostingCaseInput{Flow: FlowCancellation,
		Cancellation: &CancellationCaseInput{ReferenceEntryNo: strconv.FormatInt(disb.GetEntryNo(), 10), Reason: "sai"}})
	if err == nil || !strings.Contains(err.Error(), "REVERSAL_NOT_ALLOWED") {
		t.Fatalf("tạo case hủy cho chứng từ lnm phải bị từ chối REVERSAL_NOT_ALLOWED, nhận %v", err)
	}
	var appErr *ardaerrors.Error
	if !errors.As(err, &appErr) || appErr.Code != ardaerrors.CodeInvalidInput {
		t.Fatalf("mã lỗi = %v, muốn %s", err, ardaerrors.CodeInvalidInput)
	}
	if wf.created != 0 {
		t.Fatalf("không được mở case workflow, đã tạo %d", wf.created)
	}
}
