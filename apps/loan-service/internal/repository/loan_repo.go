package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
	"github.com/shopspring/decimal"
	"strings"
)

// Sentinel errors mapped to HTTP statuses by the service layer.
var (
	ErrNotFound = errors.New("lnm: record not found")
	ErrConflict = errors.New("lnm: code conflict")
	// ErrContractNotEditable marks a maker-revise attempt against a contract
	// whose status left the DRAFT/PENDING_APPROVAL/REJECTED window between the service guard
	// read and the guarded UPDATE (race backstop).
	ErrContractNotEditable = errors.New("lnm: contract not editable")
	// ErrAdjustmentNotPending marks a workflow decision against an adjustment
	// that is not in PENDING anymore: either already resolved (target status
	// reached — the repository reports that as an idempotent no-op instead)
	// or still in a pre-submit state.
	ErrAdjustmentNotPending = errors.New("lnm: adjustment is not pending")
	// ErrCollectionNotApproved marks an attempt to post a receipt that is not
	// approved and has no journal entry proving an earlier successful settle.
	ErrCollectionNotApproved = errors.New("lnm: collection is not approved")
	ErrHeadroomExceeded      = errors.New("lnm: contract headroom exceeded")
	// ErrStaleVersion marks a guarded decision transition whose row version no
	// longer matches the one the checker saw: the dossier changed while it was
	// in review, so the decision must not be applied.
	ErrStaleVersion = errors.New("lnm: dossier changed while in review")
)

// NewID generates a prefixed random-hex identifier.
func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure loan id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

type LoanRepository struct {
	db *sql.DB
}

func NewLoanRepository(db *sql.DB) *LoanRepository {
	return &LoanRepository{db: db}
}

// staleVersion reports whether a zero-row guarded transition was caused by a
// version mismatch (the dossier changed since the checker saw it) rather than
// by the row being absent or already terminal. The caller runs it only after
// its UPDATE (which carries the atomic `version = expected` predicate) missed.
// table is an internal constant, never user input.
func staleVersion(ctx context.Context, q repoTX, table, tenantID, id string, expected int64) bool {
	if expected <= 0 {
		return false
	}
	var current int64
	if err := q.QueryRowContext(ctx, `SELECT version FROM `+table+` WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&current); err != nil {
		return false
	}
	return current != expected
}

// repoTX is the query surface shared by *sql.DB and *sql.Tx. Repository
// helpers take it so a caller can keep a state transition and its side
// effects in a single transaction (adjustment resolve) instead of the
// default "each helper opens its own tx" shape.
type repoTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return err
}

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

const productColumns = `id, tenant_id, code, name, product_type, currency_code, interest_rate_code,
	interest_rate, loan_term_from, loan_term_to, term_unit, min_amount_minor, max_amount_minor,
	acc_classification, is_active, description, created_by, created_at, updated_at`

func scanProduct(s interface{ Scan(...any) error }) (domain.LoanProduct, error) {
	var p domain.LoanProduct
	err := s.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.ProductType, &p.CurrencyCode,
		&p.InterestRateCode, &p.InterestRate, &p.LoanTermFrom, &p.LoanTermTo, &p.TermUnit,
		&p.MinAmount, &p.MaxAmount, &p.AccClassification, &p.IsActive, &p.Description,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// productOrderBy maps the whitelisted sort field for the product catalog;
// the historical default is code ASC.
func productOrderBy(sort, order string) string {
	col := "code"
	switch sort {
	case "name":
		col = "name"
	case "created_at":
		col = "created_at"
	}
	if sort != "" && order == "desc" {
		return col + " DESC"
	}
	return col + " ASC"
}

func (r *LoanRepository) ListProducts(ctx context.Context, tenantID string, includeInactive bool, isActive string, q, sort, order string) ([]domain.LoanProduct, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+productColumns+`
		FROM lnm_products
		WHERE tenant_id = $1
		  AND ($2 OR is_active)
		  AND ($3::text = '' OR is_active::text = ANY(string_to_array($3::text, ',')))
		  AND ($4::text = '' OR code ILIKE '%' || $4 || '%' OR name ILIKE '%' || $4 || '%')
		ORDER BY `+productOrderBy(sort, order), tenantID, includeInactive, isActive, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.LoanProduct{}
	for rows.Next() {
		item, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *LoanRepository) GetProductByCode(ctx context.Context, tenantID, code string) (domain.LoanProduct, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+productColumns+` FROM lnm_products WHERE tenant_id = $1 AND code = $2 AND is_active`, tenantID, code)
	item, err := scanProduct(row)
	return item, mapNoRows(err)
}

func (r *LoanRepository) UpsertProduct(ctx context.Context, p *domain.LoanProduct) (*domain.LoanProduct, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_products (id, tenant_id, code, name, product_type, currency_code, interest_rate_code,
			interest_rate, loan_term_from, loan_term_to, term_unit, min_amount_minor, max_amount_minor,
			acc_classification, is_active, description, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (tenant_id, code) DO UPDATE SET
			name = EXCLUDED.name, product_type = EXCLUDED.product_type, currency_code = EXCLUDED.currency_code,
			interest_rate_code = EXCLUDED.interest_rate_code, interest_rate = EXCLUDED.interest_rate,
			loan_term_from = EXCLUDED.loan_term_from, loan_term_to = EXCLUDED.loan_term_to,
			term_unit = EXCLUDED.term_unit, min_amount_minor = EXCLUDED.min_amount_minor, max_amount_minor = EXCLUDED.max_amount_minor,
			acc_classification = EXCLUDED.acc_classification, is_active = EXCLUDED.is_active,
			description = EXCLUDED.description, updated_at = now()
		RETURNING `+productColumns,
		p.ID, p.TenantID, p.Code, p.Name, p.ProductType, p.CurrencyCode, p.InterestRateCode,
		p.InterestRate, p.LoanTermFrom, p.LoanTermTo, p.TermUnit, p.MinAmount, p.MaxAmount,
		p.AccClassification, p.IsActive, p.Description, p.CreatedBy)
	out, err := scanProduct(row)
	return &out, err
}

func (r *LoanRepository) ListVfuParties(ctx context.Context, tenantID, q string) ([]domain.VfuParty, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, party_code, party_name, party_type, COALESCE(gender_code,''), COALESCE(date_of_birth::text,''),
		       COALESCE(identification_id,''), COALESCE(issue_date::text,''), COALESCE(issue_place,''),
		       COALESCE(mobile_number,''), COALESCE(permanent_address,''), COALESCE(customer_reln_code,''),
		       status, created_by, created_at, updated_at
		FROM lnm_vfu_parties
		WHERE tenant_id = $1 AND ($2 = '' OR party_code ILIKE '%' || $2 || '%' OR party_name ILIKE '%' || $2 || '%')
		ORDER BY party_code LIMIT 500`, tenantID, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.VfuParty{}
	for rows.Next() {
		var p domain.VfuParty
		if err := rows.Scan(&p.ID, &p.TenantID, &p.PartyCode, &p.PartyName, &p.PartyType, &p.GenderCode,
			&p.DateOfBirth, &p.IdentificationID, &p.IssueDate, &p.IssuePlace, &p.MobileNumber,
			&p.PermanentAddress, &p.CustomerRelnCode, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (r *LoanRepository) CreateVfuParty(ctx context.Context, p *domain.VfuParty) (*domain.VfuParty, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_vfu_parties (id, tenant_id, party_code, party_name, party_type, gender_code,
			date_of_birth, identification_id, issue_date, issue_place, mobile_number,
			permanent_address, customer_reln_code, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7::date,$8,$9::date,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (tenant_id, party_code) DO NOTHING
		RETURNING id, tenant_id, party_code, party_name, party_type, COALESCE(gender_code,''), COALESCE(date_of_birth::text,''),
		          COALESCE(identification_id,''), COALESCE(issue_date::text,''), COALESCE(issue_place,''),
		          COALESCE(mobile_number,''), COALESCE(permanent_address,''), COALESCE(customer_reln_code,''),
		          status, created_by, created_at, updated_at`,
		p.ID, p.TenantID, p.PartyCode, p.PartyName, p.PartyType, p.GenderCode,
		p.DateOfBirth, p.IdentificationID, p.IssueDate, p.IssuePlace, p.MobileNumber,
		p.PermanentAddress, p.CustomerRelnCode, p.Status, p.CreatedBy)
	out, err := scanVfuParty(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w", ErrConflict)
	}
	return &out, err
}

func scanVfuParty(s interface{ Scan(...any) error }) (domain.VfuParty, error) {
	var p domain.VfuParty
	err := s.Scan(&p.ID, &p.TenantID, &p.PartyCode, &p.PartyName, &p.PartyType, &p.GenderCode,
		&p.DateOfBirth, &p.IdentificationID, &p.IssueDate, &p.IssuePlace, &p.MobileNumber,
		&p.PermanentAddress, &p.CustomerRelnCode, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (r *LoanRepository) ListVfuMandates(ctx context.Context, tenantID, q string) ([]domain.VfuMandate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, mandate_code, COALESCE(mandate_no,''), COALESCE(mandate_date::text,''), party_code,
		       COALESCE(org_code,''), COALESCE(rep_name,''), COALESCE(rep_phone,''), COALESCE(rep_address,''),
		       COALESCE(bank_name,''), COALESCE(bank_account,''), COALESCE(fee_payment_freq,''), rate_value,
		       status, created_by, created_at, updated_at
		FROM lnm_vfu_mandates
		WHERE tenant_id = $1 AND ($2 = '' OR mandate_code ILIKE '%' || $2 || '%' OR party_code ILIKE '%' || $2 || '%')
		ORDER BY mandate_code LIMIT 500`, tenantID, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.VfuMandate{}
	for rows.Next() {
		var m domain.VfuMandate
		if err := rows.Scan(&m.ID, &m.TenantID, &m.MandateCode, &m.MandateNo, &m.MandateDate, &m.PartyCode,
			&m.OrgCode, &m.RepName, &m.RepPhone, &m.RepAddress, &m.BankName, &m.BankAccount,
			&m.FeePaymentFreq, &m.RateValue, &m.Status, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (r *LoanRepository) CreateVfuMandate(ctx context.Context, m *domain.VfuMandate) (*domain.VfuMandate, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_vfu_mandates (id, tenant_id, mandate_code, mandate_no, mandate_date, party_code,
			org_code, rep_name, rep_phone, rep_address, bank_name, bank_account, fee_payment_freq,
			rate_value, status, created_by)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (tenant_id, mandate_code) DO NOTHING
		RETURNING id, tenant_id, mandate_code, COALESCE(mandate_no,''), COALESCE(mandate_date::text,''), party_code,
		          COALESCE(org_code,''), COALESCE(rep_name,''), COALESCE(rep_phone,''), COALESCE(rep_address,''),
		          COALESCE(bank_name,''), COALESCE(bank_account,''), COALESCE(fee_payment_freq,''), rate_value,
		          status, created_by, created_at, updated_at`,
		m.ID, m.TenantID, m.MandateCode, m.MandateNo, m.MandateDate, m.PartyCode,
		m.OrgCode, m.RepName, m.RepPhone, m.RepAddress, m.BankName, m.BankAccount,
		m.FeePaymentFreq, m.RateValue, m.Status, m.CreatedBy)
	out := &domain.VfuMandate{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.MandateCode, &out.MandateNo, &out.MandateDate, &out.PartyCode,
		&out.OrgCode, &out.RepName, &out.RepPhone, &out.RepAddress, &out.BankName, &out.BankAccount,
		&out.FeePaymentFreq, &out.RateValue, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrConflict)
		}
		return nil, err
	}
	return out, nil
}

func (r *LoanRepository) ListVfuPlans(ctx context.Context, tenantID, mandateCode string) ([]domain.VfuPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, plan_code, COALESCE(plan_date::text,''), mandate_code, COALESCE(contract_code,''),
		       allocated_amt_minor, settled_amt_minor, fee_amt_minor, status, created_by, created_at, updated_at
		FROM lnm_vfu_plans
		WHERE tenant_id = $1 AND ($2 = '' OR mandate_code = $2)
		ORDER BY plan_code LIMIT 500`, tenantID, mandateCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.VfuPlan{}
	for rows.Next() {
		var p domain.VfuPlan
		if err := rows.Scan(&p.ID, &p.TenantID, &p.PlanCode, &p.PlanDate, &p.MandateCode, &p.ContractCode,
			&p.AllocatedAmt, &p.SettledAmt, &p.FeeAmt, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (r *LoanRepository) CreateVfuPlan(ctx context.Context, p *domain.VfuPlan) (*domain.VfuPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_vfu_plans (id, tenant_id, plan_code, plan_date, mandate_code, contract_code,
			allocated_amt_minor, settled_amt_minor, fee_amt_minor, status, created_by)
		VALUES ($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, plan_code) DO NOTHING
		RETURNING id, tenant_id, plan_code, COALESCE(plan_date::text,''), mandate_code, COALESCE(contract_code,''),
		          allocated_amt_minor, settled_amt_minor, fee_amt_minor, status, created_by, created_at, updated_at`,
		p.ID, p.TenantID, p.PlanCode, p.PlanDate, p.MandateCode, p.ContractCode,
		p.AllocatedAmt, p.SettledAmt, p.FeeAmt, p.Status, p.CreatedBy)
	out := &domain.VfuPlan{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.PlanCode, &out.PlanDate, &out.MandateCode, &out.ContractCode,
		&out.AllocatedAmt, &out.SettledAmt, &out.FeeAmt, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrConflict)
		}
		return nil, err
	}
	return out, nil
}

// UpdateVfuParty edits a trust party. party_code and id are immutable.
func (r *LoanRepository) UpdateVfuParty(ctx context.Context, p *domain.VfuParty) (*domain.VfuParty, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE lnm_vfu_parties
		SET party_name = $3, party_type = $4, gender_code = $5, date_of_birth = NULLIF($6,'')::date,
		    identification_id = $7, issue_date = NULLIF($8,'')::date, issue_place = $9,
		    mobile_number = $10, permanent_address = $11, customer_reln_code = $12, status = $13,
		    updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, party_code, party_name, party_type, COALESCE(gender_code,''), COALESCE(date_of_birth::text,''),
		          COALESCE(identification_id,''), COALESCE(issue_date::text,''), COALESCE(issue_place,''),
		          COALESCE(mobile_number,''), COALESCE(permanent_address,''), COALESCE(customer_reln_code,''),
		          status, created_by, created_at, updated_at
	`, p.TenantID, p.ID, p.PartyName, p.PartyType, p.GenderCode, p.DateOfBirth, p.IdentificationID,
		p.IssueDate, p.IssuePlace, p.MobileNumber, p.PermanentAddress, p.CustomerRelnCode, p.Status)
	out, err := scanVfuParty(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w", ErrNotFound)
	}
	return &out, err
}

// UpdateVfuMandate edits a trust mandate. mandate_code and id are immutable.
func (r *LoanRepository) UpdateVfuMandate(ctx context.Context, m *domain.VfuMandate) (*domain.VfuMandate, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE lnm_vfu_mandates
		SET mandate_no = $3, mandate_date = NULLIF($4,'')::date, party_code = $5, org_code = $6,
		    rep_name = $7, rep_phone = $8, rep_address = $9, bank_name = $10, bank_account = $11,
		    fee_payment_freq = $12, rate_value = $13, status = $14, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, mandate_code, COALESCE(mandate_no,''), COALESCE(mandate_date::text,''), party_code,
		          COALESCE(org_code,''), COALESCE(rep_name,''), COALESCE(rep_phone,''), COALESCE(rep_address,''),
		          COALESCE(bank_name,''), COALESCE(bank_account,''), COALESCE(fee_payment_freq,''), rate_value,
		          status, created_by, created_at, updated_at
	`, m.TenantID, m.ID, m.MandateNo, m.MandateDate, m.PartyCode, m.OrgCode, m.RepName, m.RepPhone,
		m.RepAddress, m.BankName, m.BankAccount, m.FeePaymentFreq, m.RateValue, m.Status)
	out := &domain.VfuMandate{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.MandateCode, &out.MandateNo, &out.MandateDate, &out.PartyCode,
		&out.OrgCode, &out.RepName, &out.RepPhone, &out.RepAddress, &out.BankName, &out.BankAccount,
		&out.FeePaymentFreq, &out.RateValue, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrNotFound)
		}
		return nil, err
	}
	return out, nil
}

// UpdateVfuPlan edits a funding plan. plan_code and id are immutable.
func (r *LoanRepository) UpdateVfuPlan(ctx context.Context, p *domain.VfuPlan) (*domain.VfuPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE lnm_vfu_plans
		SET plan_date = NULLIF($3,'')::date, mandate_code = $4, contract_code = $5,
		    allocated_amt_minor = $6, settled_amt_minor = $7, fee_amt_minor = $8, status = $9,
		    updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, plan_code, COALESCE(plan_date::text,''), mandate_code, COALESCE(contract_code,''),
		          allocated_amt_minor, settled_amt_minor, fee_amt_minor, status, created_by, created_at, updated_at
	`, p.TenantID, p.ID, p.PlanDate, p.MandateCode, p.ContractCode, p.AllocatedAmt, p.SettledAmt, p.FeeAmt, p.Status)
	out := &domain.VfuPlan{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.PlanCode, &out.PlanDate, &out.MandateCode, &out.ContractCode,
		&out.AllocatedAmt, &out.SettledAmt, &out.FeeAmt, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrNotFound)
		}
		return nil, err
	}
	return out, nil
}

// AccruableAgreement is one ACTIVE agreement eligible for accrual.
type AccruableAgreement struct {
	ID                string
	ContractCode      string
	AgreementCode     string
	DisburseDate      string
	OutstandingAmt    int64
	InterestRate      float64
	DebtGroupCode     string
	AccClassification string
	CurrencyCode      string
}

// ListActiveAgreementsForAccrual returns ACTIVE agreements with positive
// outstanding not yet accrued to toDate.
func (r *LoanRepository) ListActiveAgreementsForAccrual(ctx context.Context, tenantID, toDate string) ([]AccruableAgreement, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.contract_code, a.agreement_code, a.disburse_date::text,
		       a.outstanding_amt_minor, a.interest_rate, a.debt_group_code,
		       COALESCE(a.acc_classification,''), COALESCE(a.currency_code,'')
		FROM lnm_agreements a
		WHERE a.tenant_id = $1 AND a.status = 'ACTIVE' AND a.outstanding_amt_minor > 0
		  AND a.disburse_date <= $2::date
		  AND NOT EXISTS (
		        SELECT 1 FROM lnm_accruals x
		        WHERE x.tenant_id = a.tenant_id AND x.agreement_code = a.agreement_code
		          AND x.to_date = $2::date AND x.status = 'POSTED')
		ORDER BY a.agreement_code`, tenantID, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccruableAgreement{}
	for rows.Next() {
		var a AccruableAgreement
		if err := rows.Scan(&a.ID, &a.ContractCode, &a.AgreementCode, &a.DisburseDate,
			&a.OutstandingAmt, &a.InterestRate, &a.DebtGroupCode, &a.AccClassification, &a.CurrencyCode); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// orgCodesToAny: nil slice -> nil (unrestricted); slice -> []string for ANY().
func orgCodesToAny(orgCodes []string) any {
	if len(orgCodes) == 0 {
		return nil
	}
	return orgCodes
}
