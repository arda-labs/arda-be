package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
	"github.com/shopspring/decimal"
	"strings"
)

// AdjustmentTables maps each registered flow kind to its table. Kind names
// come from the shared list in libs/go/arda-grpc/client/loan — callers must
// validate with loan.IsValidKind before reaching this map.
var AdjustmentTables = map[string]string{
	"debt-change":        "lnm_debt_changes",
	"rate-change":        "lnm_rate_changes",
	"restructure":        "lnm_restructures",
	"waiver":             "lnm_waivers",
	"writeoff":           "lnm_writeoffs",
	"recovery":           "lnm_recoveries",
	"fund-check":         "lnm_fund_checks",
	"revenue-allocation": "lnm_revenue_allocations",
	"vfu-fee-allocation": "lnm_vfu_fee_allocations",
	"off-balance-export": "lnm_off_balance_exports",
	"mortgage-adjust":    "lnm_mortgage_adjustments",
}

const adjustmentColumns = `id, tenant_id, contract_code, agreement_code, effective_date::text, amount_minor,
	payload, status, workflow_case_id, decision_note, decided_by, created_by, created_at, updated_at`

func scanAdjustment(s interface{ Scan(...any) error }) (domain.Adjustment, error) {
	var a domain.Adjustment
	var agreementCode, effectiveDate sql.NullString
	var amount sql.NullInt64
	var payload []byte
	var caseID, note, decidedBy sql.NullString
	err := s.Scan(&a.ID, &a.TenantID, &a.ContractCode, &agreementCode, &effectiveDate, &amount,
		&payload, &a.Status, &caseID, &note, &decidedBy, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
	if agreementCode.Valid {
		a.AgreementCode = &agreementCode.String
	}
	if effectiveDate.Valid {
		a.EffectiveDate = &effectiveDate.String
	}
	if amount.Valid {
		a.Amount = &amount.Int64
	}
	if len(payload) > 0 && string(payload) != "null" {
		a.Payload = payload
	}
	if caseID.Valid {
		a.WorkflowCaseID = &caseID.String
	}
	if note.Valid {
		a.DecisionNote = &note.String
	}
	if decidedBy.Valid {
		a.DecidedBy = &decidedBy.String
	}
	return a, err
}

func (r *LoanRepository) ListAdjustments(ctx context.Context, table, tenantID, contractCode, status string) ([]domain.Adjustment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+adjustmentColumns+`
		FROM `+table+`
		WHERE tenant_id = $1
		  AND ($2 = '' OR contract_code = $2)
		  AND ($3 = '' OR status = $3)
		ORDER BY created_at DESC LIMIT 500`, tenantID, contractCode, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Adjustment{}
	for rows.Next() {
		item, err := scanAdjustment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *LoanRepository) GetAdjustment(ctx context.Context, table, tenantID, id string) (domain.Adjustment, error) {
	return getAdjustment(ctx, r.db, table, tenantID, id)
}

// getAdjustment reads one adjustment row through any query surface.
func getAdjustment(ctx context.Context, q repoTX, table, tenantID, id string) (domain.Adjustment, error) {
	row := q.QueryRowContext(ctx, `SELECT `+adjustmentColumns+` FROM `+table+` WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	item, err := scanAdjustment(row)
	return item, mapNoRows(err)
}

func (r *LoanRepository) CreateAdjustment(ctx context.Context, table string, a *domain.Adjustment) (*domain.Adjustment, error) {
	if a.Status == "" {
		a.Status = domain.AdjustmentDraft
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO `+table+` (id, tenant_id, contract_code, agreement_code, effective_date, amount_minor, payload, status, created_by)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9)
		RETURNING `+adjustmentColumns,
		a.ID, a.TenantID, a.ContractCode, a.AgreementCode, a.EffectiveDate, a.Amount, nullIfEmpty(a.Payload), a.Status, a.CreatedBy)
	out, err := scanAdjustment(row)
	return &out, err
}

func (r *LoanRepository) SetAdjustmentWorkflowCase(ctx context.Context, table, tenantID, id, caseID string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE `+table+` SET workflow_case_id = $3, status = 'PENDING', updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'DRAFT'`, tenantID, id, caseID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}

// resolveDecisionStatus maps a workflow decision onto the adjustment's
// target status — one source of truth for the guarded transition and the
// idempotent-replay check.
func resolveDecisionStatus(decision string) (string, error) {
	switch strings.ToUpper(decision) {
	case "APPROVE":
		return domain.AdjustmentActive, nil
	case "REJECT":
		return domain.AdjustmentRejected, nil
	case "CANCEL":
		return domain.AdjustmentCancelled, nil
	default:
		return "", fmt.Errorf("unknown decision %q", decision)
	}
}

// replayOutcome interprets a guarded-resolve miss (no PENDING row matched):
// the same decision already committed is an idempotent no-op; any other
// status is a conflict that needs an operator, not a retry.
func replayOutcome(currentStatus, targetStatus string) error {
	if currentStatus == targetStatus {
		return nil
	}
	return fmt.Errorf("%w (status=%s)", ErrAdjustmentNotPending, currentStatus)
}

// ResolveAdjustment applies a workflow decision as a guarded PENDING →
// terminal transition. The state change and every side effect (debt-group /
// rate update, schedule version swap, waiver, writeoff, recovery) run in one
// transaction, so:
//
//   - a retry after a committed decision finds the target status already set
//     and returns the row as an idempotent no-op — nothing is applied twice;
//   - a side-effect failure rolls the transition back, letting the worker
//     retry the whole step safely instead of leaving a decided-but-unapplied
//     adjustment behind.
//
// A decision against any other status (never submitted, or a different
// terminal status) is rejected as a conflict.
//
// NOTE: waiver/writeoff/recovery currently mutate balances and schedules
// only; they intentionally do NOT post GL entries yet. That gap is tracked
// outside this change — do not add postings here without wiring the finance
// posting rules and the worker settle step.
func (r *LoanRepository) ResolveAdjustment(ctx context.Context, table, tenantID, id, decision, decidedBy, note string) (domain.Adjustment, error) {
	status, err := resolveDecisionStatus(decision)
	if err != nil {
		return domain.Adjustment{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Adjustment{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		UPDATE `+table+` SET status = $3, decided_by = $4, decision_note = $5, updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = $6
		RETURNING `+adjustmentColumns, tenantID, id, status, decidedBy, note, domain.AdjustmentPending)
	item, err := scanAdjustment(row)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.Adjustment{}, err
		}
		current, getErr := getAdjustment(ctx, tx, table, tenantID, id)
		if getErr != nil {
			return domain.Adjustment{}, getErr
		}
		if err := replayOutcome(current.Status, status); err != nil {
			return domain.Adjustment{}, err
		}
		// Idempotent replay of the same decision: transition and side effect
		// committed together, so there is nothing left to do.
		return current, nil
	}
	if status == domain.AdjustmentActive {
		if err := applyAdjustmentSideEffect(ctx, tx, tenantID, table, item); err != nil {
			return domain.Adjustment{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Adjustment{}, err
	}
	return item, nil
}

// applyAdjustmentSideEffect applies the approved flow's effect through the
// resolve transaction — it must never open its own connection or tx.
func applyAdjustmentSideEffect(ctx context.Context, q repoTX, tenantID, table string, item domain.Adjustment) error {
	var payload map[string]any
	if len(item.Payload) > 0 {
		_ = json.Unmarshal(item.Payload, &payload)
	}
	payloadString := func(key string) string {
		if v, ok := payload[key].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	payloadInt := func(key string) (int, bool) {
		if v, ok := payload[key].(float64); ok {
			return int(v), true
		}
		return 0, false
	}
	switch table {
	case "lnm_debt_changes":
		toGroup := payloadString("to_debt_group_code")
		if item.AgreementCode != nil && *item.AgreementCode != "" && toGroup != "" {
			if _, err := q.ExecContext(ctx, `
				UPDATE lnm_agreements SET debt_group_code = $3, updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, toGroup); err != nil {
				return err
			}
		}
	case "lnm_rate_changes":
		newRate := payloadString("new_rate")
		if item.AgreementCode != nil && *item.AgreementCode != "" && newRate != "" {
			if _, err := q.ExecContext(ctx, `
				UPDATE lnm_agreements SET interest_rate = $3, updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, newRate); err != nil {
				return err
			}
		}
	case "lnm_restructures":
		// Real restructure: move maturity/term onto the agreement and
		// regenerate an even-principal schedule over the new term count.
		if item.AgreementCode == nil || *item.AgreementCode == "" {
			return nil
		}
		newMaturity := payloadString("new_maturity_date")
		termCount := 0
		if v, ok := payloadInt("new_term"); ok {
			termCount = v
		}
		if newMaturity != "" {
			if _, err := q.ExecContext(ctx, `
				UPDATE lnm_agreements SET maturity_date = $3::date, updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, newMaturity); err != nil {
				return err
			}
		}
		if termCount > 0 {
			start := item.EffectiveDate
			if start == nil || *start == "" {
				today := ardatime.TodayCtx(ctx)
				start = &today
			}
			if err := regeneratePlansTx(ctx, q, tenantID, *item.AgreementCode, termCount, *start); err != nil {
				return err
			}
		}
	case "lnm_waivers":
		// Real waiver: shave unpaid interest across the schedule until the
		// waiver amount (or percent of outstanding interest) is consumed.
		amount := int64(0)
		if item.Amount != nil {
			amount = *item.Amount
		} else if pct, ok := payloadInt("waiver_percent"); ok {
			row := q.QueryRowContext(ctx, `
				SELECT COALESCE(SUM(plan_interest_amt_minor - coln_interest_amt_minor), 0)
				FROM lnm_repay_plans
				WHERE tenant_id = $1 AND agreement_code = $2 AND is_active AND plan_interest_amt_minor > coln_interest_amt_minor`,
				tenantID, derefAgreement(item))
			var total int64
			if err := row.Scan(&total); err != nil {
				return err
			}
			// Percent math in decimal — total*pct overflows int64 past ~9.2e16.
			// Minor units are currency-agnostic here (pure ratio), so "USD" exponent
			// cancels out in Mul/Div; ToMinor just re-quantizes the result.
			amount = ardamoney.MustToMinor(
				ardamoney.FromMinor(total, "USD").Mul(decimal.NewFromInt(int64(pct))).Div(decimal.NewFromInt(100)), "USD")
		}
		if amount > 0 && item.AgreementCode != nil && *item.AgreementCode != "" {
			if _, err := reducePlanInterestTx(ctx, q, tenantID, *item.AgreementCode, amount); err != nil {
				return err
			}
		}
	case "lnm_writeoffs":
		// Real write-off: remove the written-off amount from outstanding and
		// close the agreement once nothing is left.
		if item.AgreementCode != nil && *item.AgreementCode != "" && item.Amount != nil {
			if _, err := q.ExecContext(ctx, `
				UPDATE lnm_agreements
				SET outstanding_amt_minor = GREATEST(outstanding_amt_minor - $3, 0),
				    status = CASE WHEN GREATEST(outstanding_amt_minor - $3, 0) = 0 THEN 'CLOSED' ELSE status END,
				    updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, *item.Amount); err != nil {
				return err
			}
		}
	case "lnm_recoveries":
		if item.AgreementCode != nil && *item.AgreementCode != "" && item.Amount != nil {
			if _, err := q.ExecContext(ctx, `
				UPDATE lnm_agreements SET outstanding_amt_minor = GREATEST(outstanding_amt_minor - $3, 0), coln_principal_amt_minor = coln_principal_amt_minor + $3, updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, *item.Amount); err != nil {
				return err
			}
		}
	}
	return nil
}

func derefAgreement(item domain.Adjustment) string {
	if item.AgreementCode != nil {
		return *item.AgreementCode
	}
	return ""
}

func nullIfEmpty(payload []byte) any {
	if len(payload) == 0 || strings.TrimSpace(string(payload)) == "" || string(payload) == "null" {
		return nil
	}
	return []byte(payload)
}
