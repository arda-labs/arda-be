package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// BalanceKey identifies one fin_account_balances counter row (tenant_id is
// uniform per call and part of the table primary key).
type BalanceKey struct {
	CoaVersion   string
	AccountCode  string
	CurrencyCode string
}

// BalanceRow is one locked materialized counter set. All four counters are
// unsigned: reserved amounts are held by PENDING entries and move to posted
// on Post (or back to zero on Release), so no counter ever goes negative.
type BalanceRow struct {
	Key                 BalanceKey
	PostedDebitMinor    int64
	PostedCreditMinor   int64
	ReservedDebitMinor  int64
	ReservedCreditMinor int64
}

// OpeningSide is one raw opening-balance side (signing happens in the
// service layer, which knows the account nature).
type OpeningSide struct {
	Direction   string
	AmountMinor int64
}

// EnsureAndLockBalances materializes zero rows for every key, then locks all
// rows FOR UPDATE in primary-key order for the rest of the transaction —
// the deterministic lock order prevents deadlocks between concurrent
// entries sharing accounts.
func (r *PostingRepository) EnsureAndLockBalances(ctx context.Context, tx *sql.Tx, tenantID string, keys []BalanceKey) ([]BalanceRow, error) {
	sorted := append([]BalanceKey(nil), keys...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].CoaVersion != sorted[j].CoaVersion {
			return sorted[i].CoaVersion < sorted[j].CoaVersion
		}
		if sorted[i].AccountCode != sorted[j].AccountCode {
			return sorted[i].AccountCode < sorted[j].AccountCode
		}
		return sorted[i].CurrencyCode < sorted[j].CurrencyCode
	})
	for _, k := range sorted {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fin_account_balances (tenant_id, coa_version, account_code, currency_code)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (tenant_id, coa_version, account_code, currency_code) DO NOTHING`,
			tenantID, k.CoaVersion, k.AccountCode, k.CurrencyCode); err != nil {
			return nil, fmt.Errorf("ensure balance row: %w", err)
		}
	}
	rows := make([]BalanceRow, 0, len(sorted))
	for _, k := range sorted {
		var row BalanceRow
		row.Key = k
		err := tx.QueryRowContext(ctx, `
			SELECT posted_debit_minor, posted_credit_minor, reserved_debit_minor, reserved_credit_minor
			FROM fin_account_balances
			WHERE tenant_id = $1 AND coa_version = $2 AND account_code = $3 AND currency_code = $4
			FOR UPDATE`,
			tenantID, k.CoaVersion, k.AccountCode, k.CurrencyCode,
		).Scan(&row.PostedDebitMinor, &row.PostedCreditMinor, &row.ReservedDebitMinor, &row.ReservedCreditMinor)
		if err != nil {
			return nil, fmt.Errorf("lock balance row %s/%s/%s: %w", k.CoaVersion, k.AccountCode, k.CurrencyCode, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// SaveBalance writes the authoritative counter values back (the caller holds
// the row lock for the whole transaction).
func (r *PostingRepository) SaveBalance(ctx context.Context, tx *sql.Tx, tenantID string, row BalanceRow) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE fin_account_balances
		SET posted_debit_minor = $5, posted_credit_minor = $6,
		    reserved_debit_minor = $7, reserved_credit_minor = $8,
		    updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND coa_version = $2 AND account_code = $3 AND currency_code = $4`,
		tenantID, row.Key.CoaVersion, row.Key.AccountCode, row.Key.CurrencyCode,
		row.PostedDebitMinor, row.PostedCreditMinor, row.ReservedDebitMinor, row.ReservedCreditMinor)
	return err
}

// LoadAccountNatures returns the COA account nature ('D'|'C') per key.
// Reserve and post checks fail closed when a nature is missing.
func (r *PostingRepository) LoadAccountNatures(ctx context.Context, tenantID string, keys []BalanceKey) (map[BalanceKey]string, error) {
	out := make(map[BalanceKey]string, len(keys))
	for _, k := range keys {
		if _, ok := out[k]; ok {
			continue
		}
		var nature string
		err := r.db.QueryRowContext(ctx, `
			SELECT acc_nature FROM fin_coa_accounts
			WHERE tenant_id = $1 AND version_code = $2 AND acc_code = $3`,
			tenantID, k.CoaVersion, k.AccountCode).Scan(&nature)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("ACCOUNT_NATURE_UNKNOWN:%s", k.AccountCode)
		}
		if err != nil {
			return nil, fmt.Errorf("load account nature %s: %w", k.AccountCode, err)
		}
		out[k] = nature
	}
	return out, nil
}

// LoadOpeningSides returns the opening-balance side effective on each key at
// the latest opening date ≤ onDate (zero when none exists). Sign conversion
// to the account's natural direction happens in the service layer.
func (r *PostingRepository) LoadOpeningSides(ctx context.Context, tenantID string, keys []BalanceKey, onDate string) (map[BalanceKey]OpeningSide, error) {
	out := make(map[BalanceKey]OpeningSide, len(keys))
	for _, k := range keys {
		var side OpeningSide
		err := r.db.QueryRowContext(ctx, `
			SELECT direction, amount_minor FROM fin_opening_balances
			WHERE tenant_id = $1 AND coa_version = $2 AND account_code = $3 AND currency_code = $4
			  AND accounting_date = (
			        SELECT MAX(accounting_date) FROM fin_opening_balances
			        WHERE tenant_id = $1 AND coa_version = $2 AND account_code = $3
			              AND currency_code = $4 AND accounting_date <= $5::date)`,
			tenantID, k.CoaVersion, k.AccountCode, k.CurrencyCode, onDate,
		).Scan(&side.Direction, &side.AmountMinor)
		if err == sql.ErrNoRows {
			out[k] = OpeningSide{}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load opening balance %s: %w", k.AccountCode, err)
		}
		out[k] = side
	}
	return out, nil
}

// ListEntryLines loads the stored lines of one entry.
func (r *PostingRepository) ListEntryLines(ctx context.Context, tenantID, entryID string) ([]JournalLineRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT line_no, direction, coa_version, account_code, COALESCE(account_name,''),
		       amount_minor, currency_code, counterparty_code, counterparty_name, description, analytics
		FROM fin_journal_lines WHERE tenant_id = $1 AND entry_id = $2 ORDER BY line_no`,
		tenantID, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalLineRow
	for rows.Next() {
		var l JournalLineRow
		if err := rows.Scan(&l.LineNo, &l.Direction, &l.CoaVersion, &l.AccountCode, &l.AccountName,
			&l.AmountMinor, &l.Currency, &l.CounterpartyCode, &l.CounterpartyName, &l.Description, &l.Analytics); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
