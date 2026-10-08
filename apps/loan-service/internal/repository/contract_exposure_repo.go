package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

// GetContractExposure is the only repository query for contract REGISTER
// exposure. The domain model adds outstanding, pending, and HELD amounts.
func (r *LoanRepository) GetContractExposure(ctx context.Context, tenantID, contractCode string) (domain.ContractExposure, error) {
	return loadContractExposure(ctx, r.db, tenantID, contractCode)
}

func loadContractExposure(ctx context.Context, q repoTX, tenantID, contractCode string) (domain.ContractExposure, error) {
	var exposure domain.ContractExposure
	err := q.QueryRowContext(ctx, `
		SELECT c.loan_amt_minor,
		       COALESCE((SELECT SUM(a.outstanding_amt_minor) FROM lnm_agreements a
		                 WHERE a.tenant_id = c.tenant_id AND a.contract_code = c.contract_code), 0),
	       COALESCE((SELECT SUM(a.pending_disburse_amt_minor) FROM lnm_agreements a
		                 WHERE a.tenant_id = c.tenant_id AND a.contract_code = c.contract_code), 0),
		       COALESCE((SELECT SUM(r.amount_minor) FROM lnm_contract_reservations r
		                 WHERE r.tenant_id = c.tenant_id AND r.contract_code = c.contract_code
		                   AND r.status = 'HELD'), 0)
		FROM lnm_contracts c
		WHERE c.tenant_id = $1 AND c.contract_code = $2`, tenantID, contractCode).
		Scan(&exposure.LoanAmountMinor, &exposure.OutstandingMinor, &exposure.PendingMinor, &exposure.ReservedMinor)
	if err != nil {
		return domain.ContractExposure{}, mapNoRows(err)
	}
	return exposure, nil
}

// reserveContractAmount serializes reservations by locking the contract row,
// recomputes the shared exposure query, and inserts a HELD reservation before
// releasing the lock. Callers include the source record in their transaction.
func reserveContractAmount(ctx context.Context, tx *sql.Tx, tenantID, contractCode, sourceType, sourceID string, amountMinor int64) error {
	var lockedContractCode string
	if err := tx.QueryRowContext(ctx, `
		SELECT contract_code FROM lnm_contracts
		WHERE tenant_id = $1 AND contract_code = $2
		FOR UPDATE`, tenantID, contractCode).Scan(&lockedContractCode); err != nil {
		return mapNoRows(err)
	}
	exposure, err := loadContractExposure(ctx, tx, tenantID, contractCode)
	if err != nil {
		return err
	}
	if !exposure.Allows(amountMinor) {
		return fmt.Errorf("%w: amount_minor %d exceeds contract %s headroom %d", ErrHeadroomExceeded, amountMinor, contractCode, exposure.HeadroomMinor())
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO lnm_contract_reservations
			(tenant_id, contract_code, source_type, source_id, amount_minor, status)
		VALUES ($1,$2,$3,$4,$5,'HELD')`, tenantID, contractCode, sourceType, sourceID, amountMinor)
	return err
}
