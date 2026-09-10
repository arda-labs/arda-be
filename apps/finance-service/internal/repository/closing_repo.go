package repository

import (
	"context"
	"database/sql"
	"fmt"

	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// Closing + posting-policy reads (arda iteration 11 — kết chuyển thu chi
// FAC.203.01 + posting policy/backdate). All queries live on
// PostingRepository: they read the journal (fin_journal_entries), the
// trial-balance precompute (fin_trial_balance_daily, built by the COB step)
// and the config tables (fin_posting_policies, fin_accounting_rules).

// PostingPolicy is one fin_posting_policies row: how far back a posting may
// be dated for one business document type, and whether the closing lock
// applies. No row = no policy constraints (the check passes — EPAS
// semantics).
type PostingPolicy struct {
	AllowBackdate    bool
	MaxBackdateDays  int
	CheckClosingLock bool
}

// LoadPostingPolicy returns the policy row for (tenant, docType); found is
// false when no row exists (caller passes the check).
func (r *PostingRepository) LoadPostingPolicy(ctx context.Context, tenantID, docType string) (*PostingPolicy, bool, error) {
	var p PostingPolicy
	err := r.db.QueryRowContext(ctx, `
		SELECT allow_backdate, max_backdate_days, check_closing_lock
		FROM fin_posting_policies
		WHERE tenant_id = $1 AND business_doc_type = $2`, tenantID, docType).
		Scan(&p.AllowBackdate, &p.MaxBackdateDays, &p.CheckClosingLock)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load posting policy %s: %w", docType, err)
	}
	return &p, true, nil
}

// MaxClosingDate returns the latest accounting_date of a FIN_CLOSING journal
// entry (” when none exists) — the closing lock anchor: a posting dated
// before the last closing would write into an already-closed period.
func (r *PostingRepository) MaxClosingDate(ctx context.Context, tenantID string) (string, error) {
	var maxDate string
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(accounting_date)::text, '')
		FROM fin_journal_entries
		WHERE tenant_id = $1 AND business_doc_type = 'FIN_CLOSING'
		  AND status IN ('POSTED', 'REVERSED')`, tenantID).Scan(&maxDate)
	if err != nil {
		return "", fmt.Errorf("load max closing date: %w", err)
	}
	return maxDate, nil
}

// LoadClosingDest resolves the closing destination account for one purpose
// ("INC" | "EXP") from the fin_accounting_rules rule keys
// FIN_CLOSING_INC_DEST / FIN_CLOSING_EXP_DEST (FIXED_CODE rules seeded by
// 20260909090000). An empty account_ref or a missing rule is a caller error.
func (r *PostingRepository) LoadClosingDest(ctx context.Context, tenantID, purpose string) (string, error) {
	switch purpose {
	case "INC", "EXP":
	default:
		return "", fmt.Errorf("invalid closing purpose %q", purpose)
	}
	docType := "FIN_CLOSING_" + purpose + "_DEST"
	var accountRef sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT account_ref
		FROM fin_accounting_rules
		WHERE tenant_id = $1 AND document_type = $2 AND is_active
		ORDER BY line_no
		LIMIT 1`, tenantID, docType).Scan(&accountRef)
	if err == sql.ErrNoRows || (err == nil && !accountRef.Valid) {
		return "", fmt.Errorf("closing destination rule %s is not configured", docType)
	}
	if err != nil {
		return "", fmt.Errorf("load closing destination %s: %w", docType, err)
	}
	ref := accountRef.String
	if ref == "" {
		return "", fmt.Errorf("closing destination rule %s is not configured", docType)
	}
	return ref, nil
}

// ClosingCandidate is one INC/EXP account with a positive natural balance
// as of a date — a row of the closing candidate picker.
type ClosingCandidate struct {
	CoaVersion   string
	AccountCode  string
	AccountName  string
	AccPurpose   string
	AccNature    string
	CurrencyCode string
	BalanceMinor int64
}

// ClosingCandidateAccounts returns the INC/EXP accounts whose natural balance
// is positive as of onDate. The per-date balance source is
// fin_trial_balance_daily (the COB RebuildDaily precompute): the latest
// rebuilt business_date ≤ onDate wins. When COB has never run for the
// tenant (no rows at all ≤ onDate) the live fin_account_balances counters
// are used instead — the balances table has no date dimension, so it can
// only ever provide "current" values.
func (r *PostingRepository) ClosingCandidateAccounts(ctx context.Context, tenantID, onDate string) ([]ClosingCandidate, error) {
	// Natural balance re-signs the debit-positive sides by account nature:
	// credit-nature accounts (INC) hold value on the credit side.
	natural := `CASE WHEN a.acc_nature = 'C'
	                  THEN d.close_credit_minor - d.close_debit_minor
	                  ELSE d.close_debit_minor - d.close_credit_minor END`
	rows, err := r.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT MAX(business_date) AS d
			FROM fin_trial_balance_daily
			WHERE tenant_id = $1 AND business_date <= $2::date
		)
		SELECT d.coa_version, d.account_code, COALESCE(a.name, ''), a.acc_purpose, a.acc_nature,
		       d.currency_code, `+natural+` AS balance_minor
		FROM fin_trial_balance_daily d
		JOIN fin_coa_accounts a
		  ON a.tenant_id = d.tenant_id AND a.version_code = d.coa_version AND a.acc_code = d.account_code
		WHERE d.tenant_id = $1
		  AND d.business_date = (SELECT l.d FROM latest l)
		  AND a.acc_purpose IN ('INC', 'EXP')
		  AND `+natural+` > 0
		ORDER BY d.account_code, d.currency_code`, tenantID, onDate)
	if err != nil {
		return nil, fmt.Errorf("list closing candidates (daily): %w", err)
	}
	candidates, err := scanClosingCandidates(rows)
	if err != nil {
		return nil, err
	}
	if len(candidates) > 0 {
		return candidates, nil
	}
	// Fallback: no rebuilt trial balance yet — live posted counters.
	live := `CASE WHEN a.acc_nature = 'C'
	               THEN b.posted_credit_minor - b.posted_debit_minor
	               ELSE b.posted_debit_minor - b.posted_credit_minor END`
	rows, err = r.db.QueryContext(ctx, `
		SELECT b.coa_version, b.account_code, COALESCE(a.name, ''), a.acc_purpose, a.acc_nature,
		       b.currency_code, `+live+` AS balance_minor
		FROM fin_account_balances b
		JOIN fin_coa_accounts a
		  ON a.tenant_id = b.tenant_id AND a.version_code = b.coa_version AND a.acc_code = b.account_code
		WHERE b.tenant_id = $1
		  AND a.acc_purpose IN ('INC', 'EXP')
		  AND `+live+` > 0
		ORDER BY b.account_code, b.currency_code`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list closing candidates (live): %w", err)
	}
	return scanClosingCandidates(rows)
}

func scanClosingCandidates(rows *sql.Rows) ([]ClosingCandidate, error) {
	defer rows.Close()
	out := []ClosingCandidate{}
	for rows.Next() {
		var c ClosingCandidate
		if err := rows.Scan(&c.CoaVersion, &c.AccountCode, &c.AccountName, &c.AccPurpose,
			&c.AccNature, &c.CurrencyCode, &c.BalanceMinor); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClosingAccountRef is the COA snapshot CreateClosingCase validates closing
// rows against: the account must be postable on the date and carry the
// purpose the FE picked.
type ClosingAccountRef struct {
	CoaVersion  string
	AccountCode string
	AccountName string
	AccPurpose  string
	AccNature   string
}

// ResolveClosingAccount resolves one explicit account code (same default
// version + is_postable + effective-window rules as ResolveAccountDirect)
// plus its acc_purpose/acc_nature for the closing validation.
func (r *PostingRepository) ResolveClosingAccount(ctx context.Context, tenantID, accountCode, onDate string) (*ClosingAccountRef, error) {
	if onDate == "" {
		onDate = ardatime.Today()
	}
	coaVersion := ""
	err := r.db.QueryRowContext(ctx, `
		SELECT code FROM fin_coa_versions
		WHERE tenant_id = $1 AND is_active AND effective_date <= $2::date
		ORDER BY is_default DESC, effective_date DESC
		LIMIT 1`, tenantID, onDate).Scan(&coaVersion)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active COA version effective on %s", onDate)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve COA version: %w", err)
	}
	var out ClosingAccountRef
	var purpose sql.NullString
	err = r.db.QueryRowContext(ctx, `
		SELECT version_code, acc_code, name, acc_purpose, acc_nature
		FROM fin_coa_accounts
		WHERE tenant_id = $1 AND version_code = $2 AND acc_code = $3
		  AND is_postable
		  AND effective_date <= $4::date
		  AND (expiry_date IS NULL OR expiry_date > $4::date)`,
		tenantID, coaVersion, accountCode, onDate).
		Scan(&out.CoaVersion, &out.AccountCode, &out.AccountName, &purpose, &out.AccNature)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ACCOUNT_NOT_FOUND:%s@%s", accountCode, coaVersion)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve closing account %s: %w", accountCode, err)
	}
	out.AccPurpose = purpose.String
	return &out, nil
}
