package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

// captureTX records every statement a repository helper issues so the SQL
// shape can be asserted without a database.
type captureTX struct {
	execs []string
}

func (c *captureTX) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	c.execs = append(c.execs, query)
	return captureResult(0), nil
}

func (c *captureTX) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("captureTX: unexpected query")
}

func (c *captureTX) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

type captureResult int64

func (captureResult) LastInsertId() (int64, error) { return 0, nil }
func (r captureResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

// The restructure bug: ReplaceRepayPlans used to DELETE the whole schedule
// and re-insert it with coln_* = 0, so collected history vanished and the
// system believed the customer had never paid. The fixed contract retires
// the previous version (is_active = FALSE) and inserts the new one as active.
func TestReplaceRepayPlansTxRetiresInsteadOfDeleting(t *testing.T) {
	q := &captureTX{}
	plans := []domain.RepayPlan{
		{ContractCode: "C1", AgreementCode: "A1", PlanNo: 1, TermNo: 1, FromDate: "2026-10-01", ToDate: "2026-11-01", PlanPrincipalAmt: 60, PlanInterestAmt: 6},
		{ContractCode: "C1", AgreementCode: "A1", PlanNo: 1, TermNo: 2, FromDate: "2026-11-01", ToDate: "2026-12-01", PlanPrincipalAmt: 40, PlanInterestAmt: 4},
	}
	if err := replaceRepayPlansTx(context.Background(), q, "t1", "A1", plans); err != nil {
		t.Fatalf("replaceRepayPlansTx: %v", err)
	}
	if len(q.execs) != 1+len(plans) {
		t.Fatalf("statement count = %d, want %d (retire + inserts)", len(q.execs), 1+len(plans))
	}
	retire := strings.ToUpper(q.execs[0])
	if !strings.Contains(retire, "UPDATE LNM_REPAY_PLANS") || !strings.Contains(retire, "IS_ACTIVE = FALSE") {
		t.Fatalf("first statement must retire the previous version, got %q", q.execs[0])
	}
	for i, stmt := range q.execs {
		if strings.Contains(strings.ToUpper(stmt), "DELETE") {
			t.Fatalf("statement %d deletes collected history: %q", i, stmt)
		}
	}
	for i := range plans {
		insert := strings.ToUpper(q.execs[i+1])
		if !strings.Contains(insert, "INSERT INTO LNM_REPAY_PLANS") || !strings.Contains(insert, "IS_ACTIVE") {
			t.Fatalf("statement %d is not an active-version insert: %q", i+1, q.execs[i+1])
		}
	}
}

// The new schedule version must keep the reconciliation invariant: the sum
// of plan principal equals the agreement's outstanding balance exactly, even
// when the balance does not divide evenly across the term count.
func TestBuildEvenPrincipalPlansSumsToOutstanding(t *testing.T) {
	agreement := domain.Agreement{
		ContractCode:   "C1",
		AgreementCode:  "A1",
		CurrencyCode:   "VND",
		OutstandingAmt: 1_000_000_000,
		InterestRate:   12,
	}
	plans, err := buildEvenPrincipalPlans(agreement, 7, "2026-10-01")
	if err != nil {
		t.Fatalf("buildEvenPrincipalPlans: %v", err)
	}
	if len(plans) != 7 {
		t.Fatalf("term rows = %d, want 7", len(plans))
	}
	var sum int64
	for i, p := range plans {
		sum += p.PlanPrincipalAmt
		if p.TermNo != i+1 {
			t.Fatalf("row %d term_no = %d, want %d", i, p.TermNo, i+1)
		}
		wantFrom := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0).Format("2006-01-02")
		if p.FromDate != wantFrom {
			t.Fatalf("row %d from_date = %q, want %q", i, p.FromDate, wantFrom)
		}
		if p.PlanPrincipalAmt <= 0 {
			t.Fatalf("row %d principal = %d, must be positive", i, p.PlanPrincipalAmt)
		}
		if p.PlanInterestAmt <= 0 {
			t.Fatalf("row %d interest = %d, must be positive", i, p.PlanInterestAmt)
		}
		if i > 0 && p.PlanInterestAmt > plans[i-1].PlanInterestAmt {
			t.Fatalf("row %d interest %d must not grow over declining balance (previous %d)",
				i, p.PlanInterestAmt, plans[i-1].PlanInterestAmt)
		}
	}
	if sum != agreement.OutstandingAmt {
		t.Fatalf("sum(plan_principal) = %d, want outstanding %d", sum, agreement.OutstandingAmt)
	}
}

func TestBuildEvenPrincipalPlansRejectsBadInput(t *testing.T) {
	agreement := domain.Agreement{CurrencyCode: "VND", OutstandingAmt: 100}
	if _, err := buildEvenPrincipalPlans(agreement, 0, "2026-10-01"); err == nil {
		t.Fatal("term count 0 must be rejected")
	}
	if _, err := buildEvenPrincipalPlans(agreement, 3, "not-a-date"); err == nil {
		t.Fatal("invalid start date must be rejected")
	}
}

func TestResolveDecisionStatus(t *testing.T) {
	cases := []struct {
		decision string
		want     string
		wantErr  bool
	}{
		{"APPROVE", domain.AdjustmentActive, false},
		{"approve", domain.AdjustmentActive, false},
		{"REJECT", domain.AdjustmentRejected, false},
		{"CANCEL", domain.AdjustmentCancelled, false},
		{"MAYBE", "", true},
		{"", "", true},
	}
	for _, tt := range cases {
		got, err := resolveDecisionStatus(tt.decision)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("resolveDecisionStatus(%q) must fail", tt.decision)
			}
			continue
		}
		if err != nil {
			t.Fatalf("resolveDecisionStatus(%q): %v", tt.decision, err)
		}
		if got != tt.want {
			t.Fatalf("resolveDecisionStatus(%q) = %q, want %q", tt.decision, got, tt.want)
		}
	}
}

// A guarded-resolve miss is an idempotent no-op only when the row already
// reached the requested status; any other status is a conflict.
func TestReplayOutcome(t *testing.T) {
	if err := replayOutcome(domain.AdjustmentActive, domain.AdjustmentActive); err != nil {
		t.Fatalf("same-status replay must be a no-op, got %v", err)
	}
	for _, current := range []string{domain.AdjustmentRejected, domain.AdjustmentCancelled, domain.AdjustmentDraft} {
		err := replayOutcome(current, domain.AdjustmentActive)
		if !errors.Is(err, ErrAdjustmentNotPending) {
			t.Fatalf("replay from %s must be ErrAdjustmentNotPending, got %v", current, err)
		}
		if !strings.Contains(err.Error(), "status="+current) {
			t.Fatalf("conflict error must carry the current status, got %v", err)
		}
	}
}
