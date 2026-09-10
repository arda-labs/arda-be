package service

import (
	"context"
	"database/sql"

	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// TrialBalanceEntry is one trial-balance row: per account, per currency.
type TrialBalanceEntry struct {
	AccountCode  string `json:"account_code"`
	AccountName  string `json:"account_name"`
	CoaVersion   string `json:"coa_version"`
	CurrencyCode string `json:"currency_code"`
	DebitMinor   int64  `json:"debit_minor"`
	CreditMinor  int64  `json:"credit_minor"`
	BalanceMinor int64  `json:"balance_minor"` // debit-positive signed aggregate
}

// TrialBalanceResult is the P1a journal-aggregated trial balance.
type TrialBalanceResult struct {
	TenantID         string              `json:"tenant_id"`
	AsOf             string              `json:"as_of"`
	Entries          []TrialBalanceEntry `json:"entries"`
	TotalDebitMinor  int64               `json:"total_debit_minor"`
	TotalCreditMinor int64               `json:"total_credit_minor"`
}

// TrialBalanceService aggregates fin_journal_lines (+ fin_opening_balances)
// per account. Source of truth is the journal — no materialized balance.
type TrialBalanceService struct {
	db *sql.DB
}

func NewTrialBalanceService(db *sql.DB) *TrialBalanceService {
	return &TrialBalanceService{db: db}
}

func (s *TrialBalanceService) TrialBalance(ctx context.Context, tenantID, asOf string) (*TrialBalanceResult, error) {
	if asOf == "" {
		asOf = ardatime.TodayCtx(ctx)
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH lines AS (
			SELECT l.tenant_id, l.coa_version, l.account_code, l.currency_code,
			       SUM(CASE WHEN l.direction = 'DEBIT' THEN l.amount_minor ELSE 0 END) AS debit_minor,
			       SUM(CASE WHEN l.direction = 'CREDIT' THEN l.amount_minor ELSE 0 END) AS credit_minor
			FROM fin_journal_lines l
			JOIN fin_journal_entries e ON e.id = l.entry_id AND e.tenant_id = l.tenant_id
			WHERE l.tenant_id = $1 AND e.accounting_date <= $2::date AND e.status = 'POSTED'
			GROUP BY 1, 2, 3, 4
		), openings AS (
			SELECT tenant_id, coa_version, account_code, currency_code,
			       SUM(CASE WHEN direction = 'DEBIT' THEN amount_minor ELSE -amount_minor END) AS open_minor
			FROM fin_opening_balances
			WHERE tenant_id = $1 AND accounting_date <= $2::date
			GROUP BY 1, 2, 3, 4
		)
		SELECT COALESCE(l.coa_version, o.coa_version)          AS coa_version,
		       COALESCE(l.account_code, o.account_code)        AS account_code,
		       COALESCE(a.name, '')                            AS account_name,
		       COALESCE(l.currency_code, o.currency_code)      AS currency_code,
		       COALESCE(l.debit_minor, 0)                      AS debit_minor,
		       COALESCE(l.credit_minor, 0)                     AS credit_minor,
		       COALESCE(o.open_minor, 0)
		         + COALESCE(l.debit_minor, 0) - COALESCE(l.credit_minor, 0) AS balance_minor
		FROM lines l
		FULL OUTER JOIN openings o
		  ON o.tenant_id = l.tenant_id AND o.coa_version = l.coa_version
		 AND o.account_code = l.account_code AND o.currency_code = l.currency_code
		LEFT JOIN fin_accounts a
		  ON a.tenant_id = $1 AND a.code = COALESCE(l.account_code, o.account_code)
		ORDER BY 2, 4`,
		tenantID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &TrialBalanceResult{TenantID: tenantID, AsOf: asOf, Entries: []TrialBalanceEntry{}}
	for rows.Next() {
		var e TrialBalanceEntry
		if err := rows.Scan(&e.CoaVersion, &e.AccountCode, &e.AccountName, &e.CurrencyCode,
			&e.DebitMinor, &e.CreditMinor, &e.BalanceMinor); err != nil {
			return nil, err
		}
		result.TotalDebitMinor += e.DebitMinor
		result.TotalCreditMinor += e.CreditMinor
		result.Entries = append(result.Entries, e)
	}
	return result, rows.Err()
}
