package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// TrialBalanceDailyService maintains fin_trial_balance_daily — the COB
// precompute anchor for all statements/reports (EPAS fac_inf_trial_balance
// pattern, fac-statistical-reporting-survey.md §1.3). One row per
// tenant × date × coa_version × account × currency: opening (previous
// day's close), incremental POSTED movement on the date itself, closing.
// RebuildDaily is idempotent per (tenant, date): it recomputes the whole
// date from the journal and replaces the rows, so re-running a COB step or
// backfilling a past date is always safe.
type TrialBalanceDailyService struct {
	db *sql.DB
}

func NewTrialBalanceDailyService(db *sql.DB) *TrialBalanceDailyService {
	return &TrialBalanceDailyService{db: db}
}

// RebuildDailyResult summarizes one date rebuild.
type RebuildDailyResult struct {
	TenantID     string `json:"tenant_id"`
	BusinessDate string `json:"business_date"`
	AccountRows  int    `json:"account_rows"`
}

// RebuildDaily recomputes fin_trial_balance_daily for (tenantID, toDate).
// Incremental movement comes from POSTED journal lines with
// accounting_date = toDate; opening carries each account's latest prior
// closing (DISTINCT ON, so a backfill range with gaps stays correct) plus
// opening-balance rows booked after that prior date. Rows for the date are
// replaced in one transaction; the caller's COB checkpoint row
// (plt_job_runs) provides the run-level idempotency. Opening-balance rows
// backdated before an already-rebuilt range require re-running the backfill
// from the first affected date.
func (s *TrialBalanceDailyService) RebuildDaily(ctx context.Context, tenantID, toDate, actor string) (*RebuildDailyResult, error) {
	if toDate == "" {
		toDate = ardatime.Today()
	}
	if _, err := time.Parse("2006-01-02", toDate); err != nil {
		return nil, fmt.Errorf("to_date must be YYYY-MM-DD: %w", err)
	}
	if actor == "" {
		actor = "cob"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Delete-then-insert replaces the date wholly (safe on re-run).
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM fin_trial_balance_daily
		WHERE tenant_id = $1 AND business_date = $2::date`, tenantID, toDate); err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO fin_trial_balance_daily
		    (tenant_id, business_date, coa_version, account_code, currency_code,
		     open_debit_minor, open_credit_minor, incr_debit_minor, incr_credit_minor,
		     close_debit_minor, close_credit_minor, created_by)
		WITH prev_max AS (
			SELECT max(business_date) AS d FROM fin_trial_balance_daily
			WHERE tenant_id = $1 AND business_date < $2::date
		),
		move AS (
			SELECT l.coa_version, l.account_code, l.currency_code,
			       COALESCE(SUM(l.amount_minor) FILTER (WHERE l.direction = 'DEBIT'), 0)  AS d,
			       COALESCE(SUM(l.amount_minor) FILTER (WHERE l.direction = 'CREDIT'), 0) AS c
			FROM fin_journal_lines l
			JOIN fin_journal_entries e ON e.tenant_id = l.tenant_id AND e.id = l.entry_id
			WHERE l.tenant_id = $1 AND e.accounting_date = $2::date AND e.status = 'POSTED'
			GROUP BY 1, 2, 3
		),
		openings AS (
			SELECT tenant_id, coa_version, account_code, currency_code,
			       SUM(CASE WHEN direction = 'DEBIT' THEN amount_minor ELSE -amount_minor END) AS signed_minor
			FROM fin_opening_balances
			WHERE tenant_id = $1
			  AND accounting_date <= $2::date
			  AND accounting_date > COALESCE((SELECT d FROM prev_max), '-infinity'::date)
			GROUP BY 1, 2, 3, 4
		),
		prev AS (
			SELECT DISTINCT ON (coa_version, account_code, currency_code)
			       coa_version, account_code, currency_code,
			       close_debit_minor, close_credit_minor
			FROM fin_trial_balance_daily
			WHERE tenant_id = $1 AND business_date <= (SELECT d FROM prev_max)
			ORDER BY coa_version, account_code, currency_code, business_date DESC
		),
		keys AS (
			SELECT coa_version, account_code, currency_code FROM move
			UNION SELECT coa_version, account_code, currency_code FROM openings
			UNION SELECT coa_version, account_code, currency_code FROM prev
		)
		SELECT $1, $2::date, k.coa_version, k.account_code, k.currency_code,
		       -- opening: latest prior close + opening-balance rows not yet
		       -- covered by the rebuilt chain; debit-positive split into the
		       -- two signed sides so close = open ± incr stays additive.
		       COALESCE(p.close_debit_minor, 0)
		         + GREATEST(COALESCE(o.signed_minor, 0), 0),
		       COALESCE(p.close_credit_minor, 0)
		         + GREATEST(-COALESCE(o.signed_minor, 0), 0),
		       -- move/openings/prev are LEFT JOINed: carry-accounts with no
		       -- movement on the date must insert 0, not NULL.
		       COALESCE(m.d, 0), COALESCE(m.c, 0),
		       COALESCE(p.close_debit_minor, 0) + GREATEST(COALESCE(o.signed_minor, 0), 0) + COALESCE(m.d, 0),
		       COALESCE(p.close_credit_minor, 0) + GREATEST(-COALESCE(o.signed_minor, 0), 0) + COALESCE(m.c, 0),
		       $3
		FROM keys k
		LEFT JOIN move m ON m.coa_version = k.coa_version
		                AND m.account_code = k.account_code
		                AND m.currency_code = k.currency_code
		LEFT JOIN openings o ON o.coa_version = k.coa_version
		                    AND o.account_code = k.account_code
		                    AND o.currency_code = k.currency_code
		LEFT JOIN prev p ON p.coa_version = k.coa_version
		                AND p.account_code = k.account_code
		                AND p.currency_code = k.currency_code
		WHERE m.d IS NOT NULL OR m.c IS NOT NULL
		   OR COALESCE(o.signed_minor, 0) <> 0
		   OR COALESCE(p.close_debit_minor, 0) <> 0 OR COALESCE(p.close_credit_minor, 0) <> 0`,
		tenantID, toDate, actor)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RebuildDailyResult{TenantID: tenantID, BusinessDate: toDate, AccountRows: int(n)}, nil
}

// DailyBalanceEntry is one row of the daily trial balance read API.
type DailyBalanceEntry struct {
	CoaVersion       string `json:"coa_version"`
	AccountCode      string `json:"account_code"`
	AccountName      string `json:"account_name"`
	CurrencyCode     string `json:"currency_code"`
	OpenDebitMinor   int64  `json:"open_debit_minor"`
	OpenCreditMinor  int64  `json:"open_credit_minor"`
	IncrDebitMinor   int64  `json:"incr_debit_minor"`
	IncrCreditMinor  int64  `json:"incr_credit_minor"`
	CloseDebitMinor  int64  `json:"close_debit_minor"`
	CloseCreditMinor int64  `json:"close_credit_minor"`
}

// ListDaily returns the precomputed trial balance for one date (read API).
func (s *TrialBalanceDailyService) ListDaily(ctx context.Context, tenantID, asOf string) ([]DailyBalanceEntry, error) {
	if asOf == "" {
		asOf = ardatime.Today()
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tbd.coa_version, tbd.account_code, COALESCE(a.name, ''), tbd.currency_code,
		       tbd.open_debit_minor, tbd.open_credit_minor,
		       tbd.incr_debit_minor, tbd.incr_credit_minor,
		       tbd.close_debit_minor, tbd.close_credit_minor
		FROM fin_trial_balance_daily tbd
		LEFT JOIN fin_accounts a ON a.tenant_id = tbd.tenant_id AND a.code = tbd.account_code
		WHERE tbd.tenant_id = $1 AND tbd.business_date = $2::date
		ORDER BY tbd.account_code, tbd.currency_code`, tenantID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []DailyBalanceEntry{}
	for rows.Next() {
		var e DailyBalanceEntry
		if err := rows.Scan(&e.CoaVersion, &e.AccountCode, &e.AccountName, &e.CurrencyCode,
			&e.OpenDebitMinor, &e.OpenCreditMinor,
			&e.IncrDebitMinor, &e.IncrCreditMinor,
			&e.CloseDebitMinor, &e.CloseCreditMinor); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
