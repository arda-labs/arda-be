package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
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
	// RebuiltFrom là ngày nhỏ nhất đã dựng lại vì bút toán/số dư đầu kỳ ghi
	// sau khi ngày đó được dựng (rỗng khi không có ngày bẩn).
	RebuiltFrom string `json:"rebuilt_from,omitempty"`
	// RebuiltDates liệt kê các ngày trước BusinessDate được dựng lại.
	RebuiltDates []string `json:"rebuilt_dates,omitempty"`
}

// RiskException is one deterministic accounting risk flag (FAC #18): an
// account whose closing balance carries both debit and credit on the same
// date (bilateral balance anomaly) or an abnormal negative closing on an
// asset account. AI anomaly scoring is a later enrichment.
type RiskException struct {
	AccountCode      string `json:"account_code"`
	CurrencyCode     string `json:"currency_code"`
	CoaVersion       string `json:"coa_version"`
	CloseDebitMinor  int64  `json:"close_debit_minor"`
	CloseCreditMinor int64  `json:"close_credit_minor"`
	Reason           string `json:"reason"`
}

// RiskExceptions lists deterministic exceptions as of asOf (latest if empty).
func (s *TrialBalanceDailyService) RiskExceptions(ctx context.Context, tenantID, asOf string) ([]RiskException, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT account_code, currency_code, coa_version, close_debit_minor, close_credit_minor,
		       'BILATERAL_BALANCE' AS reason
		FROM fin_trial_balance_daily tbd
		WHERE tenant_id = $1 AND bal_type_code = $3
		  AND business_date = (
		      SELECT max(business_date) FROM fin_trial_balance_daily
		      WHERE tenant_id = $1 AND bal_type_code = $3 AND ($2 = '' OR business_date <= $2::date))
		  AND close_debit_minor > 0 AND close_credit_minor > 0
		ORDER BY account_code LIMIT 500`, tenantID, asOf, repository.BalanceTypeActual)
	if err != nil {
		return nil, fmt.Errorf("risk exceptions: %w", err)
	}
	defer rows.Close()

	out := []RiskException{}
	for rows.Next() {
		var x RiskException
		if err := rows.Scan(&x.AccountCode, &x.CurrencyCode, &x.CoaVersion,
			&x.CloseDebitMinor, &x.CloseCreditMinor, &x.Reason); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// RebuildDaily recomputes fin_trial_balance_daily for (tenantID, toDate).
//
// Số dư theo ngày là hàm xác định của sổ nhật ký (AP-PER-02):
//   - Phát sinh của một ngày dựng là mọi bút toán POSTED/REVERSED có
//     accounting_date trong (ngày dựng liền trước, ngày dựng]. COB chỉ chạy
//     ngày làm việc, nên bút toán vào thứ Bảy/Chủ nhật/lễ rơi vào ngày dựng kế
//     tiếp thay vì bị bỏ khỏi mọi dòng số dư.
//   - Trước khi dựng toDate, ngày bẩn nhỏ nhất được tìm ra: ngày đã dựng nhỏ
//     nhất phủ một bút toán (posted_at) hoặc số dư đầu kỳ (created_at) ghi sau
//     rebuilt_at của ngày đó — tức hạch toán lùi ngày, kể cả bút toán đảo lùi
//     ngày. Mọi ngày đã dựng từ ngày bẩn đến trước toDate được dựng lại theo
//     thứ tự trong cùng giao dịch, rồi mới tới toDate.
//   - Lần dựng đầu tiên của tenant đưa lịch sử trước toDate vào số đầu kỳ.
//
// Rows for each date are replaced in one transaction under a per-tenant
// advisory lock; the caller's COB checkpoint row (plt_job_runs) provides the
// run-level idempotency.
func (s *TrialBalanceDailyService) RebuildDaily(ctx context.Context, tenantID, toDate, actor string) (*RebuildDailyResult, error) {
	if toDate == "" {
		toDate = ardatime.TodayCtx(ctx)
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

	// Một tenant chỉ có một lượt dựng tại một thời điểm: chuỗi ngày dựng lại
	// đọc close của ngày trước, hai lượt song song sẽ đọc nhau dở dang.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('fin_trial_balance_daily:' || $1))`, tenantID); err != nil {
		return nil, fmt.Errorf("lock daily rebuild: %w", err)
	}

	var dirtyFrom sql.NullString
	if err := tx.QueryRowContext(ctx, `
		WITH stored AS (
			SELECT business_date AS d, min(rebuilt_at) AS rebuilt_at
			FROM fin_trial_balance_daily
			WHERE tenant_id = $1 AND business_date < $2::date
			GROUP BY business_date
		),
		changes AS (
			SELECT e.accounting_date AS a, COALESCE(e.posted_at, e.created_at) AS changed_at
			FROM fin_journal_entries e
			WHERE e.tenant_id = $1 AND e.status IN ('POSTED', 'REVERSED')
			  AND e.accounting_date < $2::date
			  AND COALESCE(e.posted_at, e.created_at) > (SELECT min(rebuilt_at) FROM stored)
			UNION ALL
			SELECT o.accounting_date, o.created_at
			FROM fin_opening_balances o
			WHERE o.tenant_id = $1 AND o.accounting_date < $2::date
			  AND o.created_at > (SELECT min(rebuilt_at) FROM stored)
		)
		SELECT min(cover.d)::text
		FROM changes c
		JOIN LATERAL (
			SELECT d, rebuilt_at FROM stored WHERE d >= c.a ORDER BY d LIMIT 1
		) cover ON true
		WHERE c.changed_at > cover.rebuilt_at`, tenantID, toDate).Scan(&dirtyFrom); err != nil {
		return nil, fmt.Errorf("find dirty daily balance dates: %w", err)
	}

	result := &RebuildDailyResult{TenantID: tenantID, BusinessDate: toDate}
	if dirtyFrom.Valid {
		rows, err := tx.QueryContext(ctx, `
			SELECT DISTINCT business_date::text FROM fin_trial_balance_daily
			WHERE tenant_id = $1 AND business_date >= $2::date AND business_date < $3::date
			ORDER BY 1`, tenantID, dirtyFrom.String, toDate)
		if err != nil {
			return nil, fmt.Errorf("list dirty daily balance dates: %w", err)
		}
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				rows.Close()
				return nil, err
			}
			result.RebuiltDates = append(result.RebuiltDates, d)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		result.RebuiltFrom = dirtyFrom.String
		for _, d := range result.RebuiltDates {
			if _, err := rebuildDailyDate(ctx, tx, tenantID, d, actor); err != nil {
				return nil, fmt.Errorf("rebuild dirty date %s: %w", d, err)
			}
		}
	}

	n, err := rebuildDailyDate(ctx, tx, tenantID, toDate, actor)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	result.AccountRows = n
	return result, nil
}

// rebuildDailyDate replaces the rows of one date. Incremental movement is the
// journal in (previous rebuilt date, toDate]; opening carries each account's
// latest prior closing (DISTINCT ON, so a range with gaps stays correct) plus
// opening-balance rows and — on the tenant's first rebuilt date — journal
// history booked before toDate.
func rebuildDailyDate(ctx context.Context, tx *sql.Tx, tenantID, toDate, actor string) (int, error) {
	// Delete-then-insert replaces the date wholly (safe on re-run).
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM fin_trial_balance_daily
		WHERE tenant_id = $1 AND business_date = $2::date`, tenantID, toDate); err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO fin_trial_balance_daily
		    (tenant_id, business_date, org_code, bal_type_code, coa_version, account_code, currency_code,
		     open_debit_minor, open_credit_minor, incr_debit_minor, incr_credit_minor,
		     close_debit_minor, close_credit_minor, created_by)
		WITH prev_max AS (
			SELECT max(business_date) AS d FROM fin_trial_balance_daily
			WHERE tenant_id = $1 AND business_date < $2::date
		),
		journal AS (
			SELECT e.accounting_date, l.bal_type_code, l.coa_version, l.account_code, l.currency_code,
			       COALESCE(NULLIF(e.metadata->>'org_code', ''), '') AS org_code,
			       l.direction, l.amount_minor
			FROM fin_journal_lines l
			JOIN fin_journal_entries e ON e.tenant_id = l.tenant_id AND e.id = l.entry_id
			WHERE l.tenant_id = $1 AND l.bal_type_code = $4
			  AND e.accounting_date <= $2::date AND e.status IN ('POSTED', 'REVERSED')
		),
		-- Phát sinh: (ngày dựng trước, toDate] — gồm cả ngày nghỉ ở giữa.
		-- Chưa có ngày dựng trước thì chỉ đúng toDate; lịch sử vào history.
		move AS (
			SELECT bal_type_code, coa_version, account_code, currency_code, org_code,
			       COALESCE(SUM(amount_minor) FILTER (WHERE direction = 'DEBIT'), 0)  AS d,
			       COALESCE(SUM(amount_minor) FILTER (WHERE direction = 'CREDIT'), 0) AS c
			FROM journal
			WHERE accounting_date > COALESCE((SELECT d FROM prev_max), $2::date - 1)
			GROUP BY 1, 2, 3, 4, 5
		),
		history AS (
			SELECT bal_type_code, coa_version, account_code, currency_code, org_code,
			       COALESCE(SUM(amount_minor) FILTER (WHERE direction = 'DEBIT'), 0)  AS d,
			       COALESCE(SUM(amount_minor) FILTER (WHERE direction = 'CREDIT'), 0) AS c
			FROM journal
			WHERE (SELECT d FROM prev_max) IS NULL AND accounting_date < $2::date
			GROUP BY 1, 2, 3, 4, 5
		),
		openings AS (
			SELECT tenant_id, ''::varchar AS org_code, coa_version, account_code, currency_code,
			       SUM(CASE WHEN direction = 'DEBIT' THEN amount_minor ELSE -amount_minor END) AS signed_minor
			FROM fin_opening_balances
			WHERE tenant_id = $1
			  AND accounting_date <= $2::date
			  AND accounting_date > COALESCE((SELECT d FROM prev_max), '-infinity'::date)
			GROUP BY 1, 2, 3, 4, 5
		),
		prev AS (
			SELECT DISTINCT ON (org_code, bal_type_code, coa_version, account_code, currency_code)
			       org_code, bal_type_code, coa_version, account_code, currency_code,
			       close_debit_minor, close_credit_minor
			FROM fin_trial_balance_daily
			WHERE tenant_id = $1 AND bal_type_code = $4 AND business_date <= (SELECT d FROM prev_max)
			ORDER BY org_code, bal_type_code, coa_version, account_code, currency_code, business_date DESC
		),
		keys AS (
			SELECT org_code, bal_type_code, coa_version, account_code, currency_code FROM move
			UNION SELECT org_code, bal_type_code, coa_version, account_code, currency_code FROM history
			UNION SELECT org_code, $4::text, coa_version, account_code, currency_code FROM openings
			UNION SELECT org_code, bal_type_code, coa_version, account_code, currency_code FROM prev
		)
		SELECT $1, $2::date, k.org_code, k.bal_type_code, k.coa_version, k.account_code, k.currency_code,
		       -- opening: latest prior close + opening-balance rows not yet
		       -- covered by the rebuilt chain (+ history on the first date);
		       -- debit-positive split into the two signed sides so
		       -- close = open ± incr stays additive.
		       COALESCE(p.close_debit_minor, 0)
		         + GREATEST(COALESCE(o.signed_minor, 0), 0) + COALESCE(h.d, 0),
		       COALESCE(p.close_credit_minor, 0)
		         + GREATEST(-COALESCE(o.signed_minor, 0), 0) + COALESCE(h.c, 0),
		       -- move/openings/prev are LEFT JOINed: carry-accounts with no
		       -- movement on the date must insert 0, not NULL.
		       COALESCE(m.d, 0), COALESCE(m.c, 0),
		       COALESCE(p.close_debit_minor, 0) + GREATEST(COALESCE(o.signed_minor, 0), 0) + COALESCE(h.d, 0) + COALESCE(m.d, 0),
		       COALESCE(p.close_credit_minor, 0) + GREATEST(-COALESCE(o.signed_minor, 0), 0) + COALESCE(h.c, 0) + COALESCE(m.c, 0),
		       $3
		FROM keys k
		LEFT JOIN move m ON m.org_code = k.org_code AND m.bal_type_code = k.bal_type_code
		                AND m.coa_version = k.coa_version
		                AND m.account_code = k.account_code
		                AND m.currency_code = k.currency_code
		LEFT JOIN history h ON h.org_code = k.org_code AND h.bal_type_code = k.bal_type_code
		                   AND h.coa_version = k.coa_version
		                   AND h.account_code = k.account_code
		                   AND h.currency_code = k.currency_code
		LEFT JOIN openings o ON o.org_code = k.org_code
		                    AND o.coa_version = k.coa_version
		                    AND o.account_code = k.account_code
		                    AND o.currency_code = k.currency_code
		LEFT JOIN prev p ON p.org_code = k.org_code AND p.bal_type_code = k.bal_type_code
		                AND p.coa_version = k.coa_version
		                AND p.account_code = k.account_code
		                AND p.currency_code = k.currency_code
		WHERE m.d IS NOT NULL OR m.c IS NOT NULL
		   OR COALESCE(h.d, 0) <> 0 OR COALESCE(h.c, 0) <> 0
		   OR COALESCE(o.signed_minor, 0) <> 0
		   OR COALESCE(p.close_debit_minor, 0) <> 0 OR COALESCE(p.close_credit_minor, 0) <> 0`,
		tenantID, toDate, actor, repository.BalanceTypeActual)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DailyBalanceEntry is one row of the daily trial balance read API.
type DailyBalanceEntry struct {
	OrgCode          string `json:"org_code"`
	BalTypeCode      string `json:"bal_type_code"`
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
		asOf = ardatime.TodayCtx(ctx)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tbd.org_code, tbd.bal_type_code, tbd.coa_version, tbd.account_code, COALESCE(a.name, ''), tbd.currency_code,
		       tbd.open_debit_minor, tbd.open_credit_minor,
		       tbd.incr_debit_minor, tbd.incr_credit_minor,
		       tbd.close_debit_minor, tbd.close_credit_minor
		FROM fin_trial_balance_daily tbd
		LEFT JOIN fin_coa_accounts a
		       ON a.tenant_id = tbd.tenant_id
		      AND a.version_code = tbd.coa_version
		      AND a.acc_code = tbd.account_code
		WHERE tbd.tenant_id = $1 AND tbd.business_date = $2::date AND tbd.bal_type_code = $3
		ORDER BY tbd.account_code, tbd.currency_code, tbd.org_code`, tenantID, asOf, repository.BalanceTypeActual)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []DailyBalanceEntry{}
	for rows.Next() {
		var e DailyBalanceEntry
		if err := rows.Scan(&e.OrgCode, &e.BalTypeCode, &e.CoaVersion, &e.AccountCode, &e.AccountName, &e.CurrencyCode,
			&e.OpenDebitMinor, &e.OpenCreditMinor,
			&e.IncrDebitMinor, &e.IncrCreditMinor,
			&e.CloseDebitMinor, &e.CloseCreditMinor); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
