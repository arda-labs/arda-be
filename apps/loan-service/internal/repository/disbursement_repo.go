package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

// disbursementSortCol maps the public sort key (whitelisted in the handler
// ListSpec) to a SQL column; unknown keys fall back to created_at.
func disbursementSortCol(field string) string {
	switch field {
	case "agreement_code":
		return "agreement_code"
	case "contract_code":
		return "contract_code"
	case "disburse_date":
		return "disburse_date"
	case "created_at":
		return "created_at"
	default:
		return "created_at"
	}
}

func sortDirection(order string) string {
	if order == "desc" {
		return "DESC"
	}
	return "ASC"
}

// cashFlowDirection defaults to DESC (historical most-recent-first) until an
// explicit sort field arrives with its own direction.
func cashFlowDirection(sort, order string) string {
	if sort == "" {
		return "DESC"
	}
	return sortDirection(order)
}

func (r *LoanRepository) ListDisbursements(ctx context.Context, tenantID string, orgCodes []string, status, contractCode, flowType, q, sort, order string, limit, offset int) ([]domain.Disbursement, int, error) {
	orgAny := orgCodesToAny(orgCodes)
	sortCol := disbursementSortCol(sort)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, contract_code, agreement_code, disburse_date::text, disburse_amt_minor,
		       currency_code, COALESCE(fund_source_code,''), flow_type, source_register_id::text, COALESCE(batch_id::text,''), status, payload,
		       workflow_case_id::text, COALESCE(workflow_case_code,''), journal_entry_id::text, created_by, created_at, updated_at,
		       count(*) OVER() AS total_count
		FROM lnm_disbursements
		WHERE tenant_id = $1::text
		  AND batch_id IS NULL
		  AND ($2::text = '' OR status = $2::text)
		  AND ($3::text = '' OR contract_code = $3::text)
		  AND ($4::text = '' OR flow_type = $4::text)
		  AND ($5::text = '' OR agreement_code ILIKE '%' || $5::text || '%' OR contract_code ILIKE '%' || $5::text || '%')
		  AND ($6::text[] IS NULL OR org_code = ANY($6::text[]))
		ORDER BY `+sortCol+` `+cashFlowDirection(sort, order)+`, id
		LIMIT $7::int OFFSET $8::int`,
		tenantID, status, contractCode, flowType, q, orgAny, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.Disbursement{}
	total := 0
	for rows.Next() {
		var d domain.Disbursement
		var caseID, entryID, sourceID sql.NullString
		var payload []byte
		if err := rows.Scan(&d.ID, &d.TenantID, &d.ContractCode, &d.AgreementCode, &d.DisburseDate,
			&d.DisburseAmtMinor, &d.CurrencyCode, &d.FundSourceCode, &d.FlowType, &sourceID, &d.BatchID, &d.Status, &payload,
			&caseID, &d.WorkflowCaseCode, &entryID, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		if len(payload) > 0 && string(payload) != "null" {
			d.Payload = payload
		}
		if caseID.Valid {
			d.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			d.JournalEntryID = &entryID.String
		}
		if sourceID.Valid {
			d.SourceRegisterID = sourceID.String
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *LoanRepository) CreateDisbursement(ctx context.Context, d *domain.Disbursement) (*domain.Disbursement, error) {
	flowType := d.FlowType
	if flowType == "" {
		flowType = domain.FlowRegister
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_disbursements
			(tenant_id, contract_code, agreement_code, disburse_date, disburse_amt_minor,
			 currency_code, fund_source_code, flow_type, source_register_id, status, org_code, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'DRAFT',$10,$11)
		RETURNING id, created_at, updated_at`,
		d.TenantID, d.ContractCode, d.AgreementCode, d.DisburseDate, d.DisburseAmtMinor,
		d.CurrencyCode, nullText(d.FundSourceCode), flowType, nullText(d.SourceRegisterID), nullText(d.OrgCode), d.CreatedBy)
	if err := row.Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return d, nil
}

// CreateDisbursementWithReservation atomically creates a REGISTER request and
// holds its contract amount. The contract row lock serializes all writers for
// the same contract before exposure is read and the HELD reservation inserted.
func (r *LoanRepository) CreateDisbursementWithReservation(ctx context.Context, d *domain.Disbursement) (*domain.Disbursement, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	flowType := d.FlowType
	if flowType == "" {
		flowType = domain.FlowRegister
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO lnm_disbursements
			(tenant_id, contract_code, agreement_code, disburse_date, disburse_amt_minor,
			 currency_code, fund_source_code, flow_type, source_register_id, status, org_code, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'DRAFT',$10,$11)
		RETURNING id::text, created_at, updated_at`,
		d.TenantID, d.ContractCode, d.AgreementCode, d.DisburseDate, d.DisburseAmtMinor,
		d.CurrencyCode, nullText(d.FundSourceCode), flowType, nullText(d.SourceRegisterID), nullText(d.OrgCode), d.CreatedBy).
		Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	if err := reserveContractAmount(ctx, tx, d.TenantID, d.ContractCode, "DISBURSEMENT", d.ID, d.DisburseAmtMinor); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d, nil
}

func (r *LoanRepository) GetDisbursement(ctx context.Context, tenantID, id string) (*domain.Disbursement, error) {
	var d domain.Disbursement
	var caseID, entryID, sourceID sql.NullString
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, contract_code, agreement_code, disburse_date::text, disburse_amt_minor,
		       currency_code, COALESCE(fund_source_code,''), flow_type, source_register_id::text, COALESCE(batch_id::text,''), status, payload,
		       workflow_case_id::text, COALESCE(workflow_case_code,''), journal_entry_id::text, created_by, created_at, updated_at,
		       version
		FROM lnm_disbursements WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	var payload []byte
	err := row.Scan(&d.ID, &d.TenantID, &d.ContractCode, &d.AgreementCode, &d.DisburseDate,
		&d.DisburseAmtMinor, &d.CurrencyCode, &d.FundSourceCode, &d.FlowType, &sourceID, &d.BatchID, &d.Status, &payload,
		&caseID, &d.WorkflowCaseCode, &entryID, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt, &d.DataVersion)
	if err == sql.ErrNoRows {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if len(payload) > 0 && string(payload) != "null" {
		d.Payload = payload
	}
	if caseID.Valid {
		d.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		d.JournalEntryID = &entryID.String
	}
	if sourceID.Valid {
		d.SourceRegisterID = sourceID.String
	}
	return &d, nil
}

func (r *LoanRepository) SetDisbursementStatus(ctx context.Context, tenantID, id, from, to, updatedBy, reason string) error {
	if err := domain.CanTransition(domain.WorkflowMachine, domain.Status(from), domain.Status(to), reason); err != nil {
		return err
	}
	if to == domain.DisbursementRejected || to == domain.DisbursementCancelled {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := setDisbursementStatus(ctx, tx, tenantID, id, from, to, updatedBy); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE lnm_contract_reservations SET status = 'RELEASED'
			WHERE tenant_id = $1 AND source_type = 'DISBURSEMENT' AND source_id = $2 AND status = 'HELD'`, tenantID, id); err != nil {
			return err
		}
		return tx.Commit()
	}
	return setDisbursementStatus(ctx, r.db, tenantID, id, from, to, updatedBy)
}

func setDisbursementStatus(ctx context.Context, q repoTX, tenantID, id, from, to, updatedBy string) error {
	expected := domain.DataVersionFromContext(ctx)
	res, err := q.ExecContext(ctx, `
		UPDATE lnm_disbursements SET status = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = $5
		  AND ($6 = 0 OR version = $6)`, tenantID, id, to, updatedBy, from, expected)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if staleVersion(ctx, q, "lnm_disbursements", tenantID, id, expected) {
			return ErrStaleVersion
		}
		var current string
		lookupErr := q.QueryRowContext(ctx, `SELECT status FROM lnm_disbursements WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&current)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("%w", ErrNotFound)
		}
		if lookupErr != nil {
			return lookupErr
		}
		return fmt.Errorf("%w: disbursement is %s, expected %s", domain.ErrInvalidTransition, current, from)
	}
	return nil
}

func (r *LoanRepository) SetDisbursementCaseAndJournal(ctx context.Context, tenantID, id, caseID, caseCode, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_disbursements SET workflow_case_id = $3, workflow_case_code = $4, journal_entry_id = $5, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nullText(caseID), nullText(caseCode), nullText(journalEntryID))
	return err
}

// SettleRegisterDisbursement is a legacy fixture helper for tests that seed
// already-disbursed principal directly. Production REGISTER posting uses the
// transactional SettleDisbursementRegister path below.
func (r *LoanRepository) SettleRegisterDisbursement(ctx context.Context, tenantID, agreementCode string, amountMinor int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_agreements
		SET outstanding_amt_minor = outstanding_amt_minor + $3,
		    updated_at = now()
		WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, agreementCode, amountMinor)
	return err
}

// SettleCompleteDisbursement applies the COMPLETE posting side effect: move
// the amount from pending into outstanding, and activate the contract.
func (r *LoanRepository) SettleCompleteDisbursement(ctx context.Context, tenantID, contractCode, agreementCode string, amountMinor int64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_agreements
		SET outstanding_amt_minor = outstanding_amt_minor + $3,
		    pending_disburse_amt_minor = pending_disburse_amt_minor - $3,
		    updated_at = now()
		WHERE tenant_id = $1 AND agreement_code = $2 AND pending_disburse_amt_minor >= $3`, tenantID, agreementCode, amountMinor)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("agreement %s does not have enough pending amount to complete", agreementCode)
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE lnm_contracts c SET status = 'DISBURSED', updated_at = now()
		WHERE c.tenant_id = $1
		  AND c.contract_code = $2
		  AND c.status = 'APPROVED'
		  AND EXISTS (
		        SELECT 1 FROM lnm_disbursements d
		        WHERE d.tenant_id = $1 AND d.contract_code = c.contract_code
		          AND d.flow_type = 'COMPLETE' AND d.status = 'POSTED')`,
		tenantID, contractCode)
	return err
}

// SettleDisbursementRegister atomically marks one REGISTER drawdown POSTED and
// applies its agreement side effect (in-transit pending bump,
// PENDING → ACTIVE). Returns false when the disbursement was already settled,
// keeping worker retries idempotent instead of double-counting the drawdown.
func (r *LoanRepository) SettleDisbursementRegister(ctx context.Context, tenantID, id, agreementCode string, amountMinor int64, journalEntryID, updatedBy string) (bool, error) {
	if err := domain.CanTransition(domain.WorkflowMachine, domain.StatusApproved, domain.StatusPosted, ""); err != nil {
		return false, err
	}
	if err := domain.CanTransition(domain.ContractMachine, domain.StatusApproved, domain.StatusDisbursed, ""); err != nil {
		return false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	expected := domain.DataVersionFromContext(ctx)
	res, err := tx.ExecContext(ctx, `
		UPDATE lnm_disbursements
		SET status = 'POSTED', journal_entry_id = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'APPROVED'
		  AND ($5 = 0 OR version = $5)`,
		tenantID, id, nullText(journalEntryID), updatedBy, expected)
	if err != nil {
		return false, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		if staleVersion(ctx, tx, "lnm_disbursements", tenantID, id, expected) {
			return false, ErrStaleVersion
		}
		return false, nil
	}

	res, err = tx.ExecContext(ctx, `
		UPDATE lnm_agreements
		SET pending_disburse_amt_minor = pending_disburse_amt_minor + $3,
		    updated_at = now()
		WHERE tenant_id = $1 AND agreement_code = $2`,
		tenantID, agreementCode, amountMinor)
	if err != nil {
		return false, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return false, fmt.Errorf("agreement %s not found while settling disbursement", agreementCode)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE lnm_contracts c SET status = 'DISBURSED', updated_at = now()
		WHERE c.tenant_id = $1 AND c.status = 'APPROVED'
		  AND c.contract_code = (SELECT d.contract_code FROM lnm_disbursements d WHERE d.tenant_id = $1 AND d.id = $2)`, tenantID, id); err != nil {
		return false, err
	}
	reservation, err := tx.ExecContext(ctx, `
		UPDATE lnm_contract_reservations SET status = 'CONSUMED'
		WHERE tenant_id = $1 AND source_type = 'DISBURSEMENT' AND source_id = $2 AND status = 'HELD'`, tenantID, id)
	if err != nil {
		return false, err
	}
	if affected, _ := reservation.RowsAffected(); affected == 0 {
		return false, fmt.Errorf("held contract reservation not found while settling disbursement %s", id)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// SettleDisbursementComplete is the COMPLETE counterpart of
// SettleDisbursementRegister: it marks the drawdown POSTED, unwinds the
// in-transit pending and activates the contract once a completed drawdown
// exists. Returns false on idempotent replay.
func (r *LoanRepository) SettleDisbursementComplete(ctx context.Context, tenantID, id, contractCode, agreementCode string, amountMinor int64, journalEntryID, updatedBy string) (bool, error) {
	if err := domain.CanTransition(domain.WorkflowMachine, domain.StatusApproved, domain.StatusPosted, ""); err != nil {
		return false, err
	}
	if err := domain.CanTransition(domain.ContractMachine, domain.StatusApproved, domain.StatusDisbursed, ""); err != nil {
		return false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	expected := domain.DataVersionFromContext(ctx)
	res, err := tx.ExecContext(ctx, `
		UPDATE lnm_disbursements
		SET status = 'POSTED', journal_entry_id = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'APPROVED'
		  AND ($5 = 0 OR version = $5)`,
		tenantID, id, nullText(journalEntryID), updatedBy, expected)
	if err != nil {
		return false, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		if staleVersion(ctx, tx, "lnm_disbursements", tenantID, id, expected) {
			return false, ErrStaleVersion
		}
		return false, nil
	}

	res, err = tx.ExecContext(ctx, `
		UPDATE lnm_agreements
		SET outstanding_amt_minor = outstanding_amt_minor + $3,
		    pending_disburse_amt_minor = pending_disburse_amt_minor - $3,
		    updated_at = now()
		WHERE tenant_id = $1 AND agreement_code = $2 AND pending_disburse_amt_minor >= $3`,
		tenantID, agreementCode, amountMinor)
	if err != nil {
		return false, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return false, fmt.Errorf("agreement %s does not have enough pending amount to complete", agreementCode)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE lnm_contracts c SET status = 'DISBURSED', updated_at = now()
		WHERE c.tenant_id = $1
		  AND c.contract_code = $2
		  AND c.status = 'APPROVED'
		  AND EXISTS (
		        SELECT 1 FROM lnm_disbursements d
		        WHERE d.tenant_id = $1 AND d.contract_code = c.contract_code
		          AND d.flow_type = 'COMPLETE' AND d.status = 'POSTED')`,
		tenantID, contractCode); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// SumCompleteForSource totals the COMPLETE drawdowns already booked against
// one REGISTER source — in-flight cases (PENDING_APPROVAL/APPROVED) count too, so
// concurrent completes cannot overshoot the register (complete remainder guard).
func (r *LoanRepository) SumCompleteForSource(ctx context.Context, tenantID, sourceRegisterID string) (int64, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(disburse_amt_minor), 0)
		FROM lnm_disbursements
		WHERE tenant_id = $1 AND source_register_id = $2 AND flow_type = 'COMPLETE'
		  AND status IN ('PENDING_APPROVAL', 'APPROVED', 'POSTED')`, tenantID, sourceRegisterID)
	var total int64
	return total, row.Scan(&total)
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
