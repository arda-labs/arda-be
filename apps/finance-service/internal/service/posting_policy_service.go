package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
)

// Posting-date policy (arda iteration 11 — posting policy/backdate). Every
// posting-date rule is evaluated by EnsurePostingDateAllowed before any
// write (Reserve / direct Post / Reverse). Errors are sentinel-wrapped: the
// message starts with the stable code the FE and the workflow workers key on
// (the workers throw it as the BPMN VALIDATION_FAILED boundary error, so the
// case goes back to the maker instead of burning job retries).
var (
	// ErrTransactionDateExceedsCurrentDate — the posting date is in the
	// future.
	ErrTransactionDateExceedsCurrentDate = errors.New("TRANSACTION_DATE_EXCEEDS_CURRENT_DATE")
	// ErrBackdateNotAllowed — a backdated posting for a document type whose
	// policy row sets allow_backdate = false.
	ErrBackdateNotAllowed = errors.New("BACKDATE_NOT_ALLOWED")
	// ErrTransactionDateExceedsBackdate — backdating beyond the policy's
	// max_backdate_days window.
	ErrTransactionDateExceedsBackdate = errors.New("TRANSACTION_DATE_EXCEEDS_BACKDATE")
	// ErrPostingDateBeforeClosingLock — the posting date precedes the latest
	// FIN_CLOSING entry: the period is already closed by the closing run.
	ErrPostingDateBeforeClosingLock = errors.New("POSTING_DATE_BEFORE_CLOSING_LOCK")
)

// postingPolicySentinels is the classification list the workflow workers use
// to distinguish validation-class (permanent, maker-fixable) finance errors
// from infrastructure failures.
var postingPolicySentinels = []string{
	ErrTransactionDateExceedsCurrentDate.Error(),
	ErrBackdateNotAllowed.Error(),
	ErrTransactionDateExceedsBackdate.Error(),
	ErrPostingDateBeforeClosingLock.Error(),
}

// IsPostingPolicyError reports whether the error is one of the posting-date
// policy sentinels (errors.Is works through %w wrapping).
func IsPostingPolicyError(err error) bool {
	for _, sentinel := range postingPolicySentinels {
		if errors.Is(err, sentinelErr(sentinel)) {
			return true
		}
	}
	return false
}

func sentinelErr(code string) error {
	switch code {
	case ErrTransactionDateExceedsCurrentDate.Error():
		return ErrTransactionDateExceedsCurrentDate
	case ErrBackdateNotAllowed.Error():
		return ErrBackdateNotAllowed
	case ErrTransactionDateExceedsBackdate.Error():
		return ErrTransactionDateExceedsBackdate
	default:
		return ErrPostingDateBeforeClosingLock
	}
}

// EnsurePostingDateAllowed enforces the posting-date policy for one business
// document type:
//
//	date > today → TRANSACTION_DATE_EXCEEDS_CURRENT_DATE
//	date < today → policy lookup (no row → pass, EPAS semantics):
//	  allow_backdate=false            → BACKDATE_NOT_ALLOWED
//	  beyond max_backdate_days        → TRANSACTION_DATE_EXCEEDS_BACKDATE
//	  check_closing_lock and date < latest FIN_CLOSING date
//	                                  → POSTING_DATE_BEFORE_CLOSING_LOCK
//
// Today passes without a policy lookup — only backdated postings are
// constrained.
func (s *PostingService) EnsurePostingDateAllowed(ctx context.Context, tenantID, docType, date string) error {
	violation, err := s.checkPostingDate(ctx, tenantID, docType, date, time.Now())
	if err != nil {
		return err
	}
	if violation != nil {
		return violation
	}
	return nil
}

// checkPostingDate is the pure decision core (now injected for tests); it
// returns a policy violation error, an infrastructure error, or nil.
func (s *PostingService) checkPostingDate(ctx context.Context, tenantID, docType, date string, now time.Time) (error, error) {
	date = trimDate(date)
	if date == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		// Date-format problems are caught by the caller's own validation /
		// the DATE column cast — the policy layer has nothing to add.
		return nil, nil
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, parsed.Location())
	if parsed.After(today) {
		return fmt.Errorf("%w: accounting date %s is after the current date %s",
			ErrTransactionDateExceedsCurrentDate, date, today.Format("2006-01-02")), nil
	}
	if parsed.Equal(today) {
		return nil, nil
	}
	policy, found, err := s.repo.LoadPostingPolicy(ctx, tenantID, docType)
	if err != nil {
		return nil, err
	}
	if !found {
		// No policy row → no constraints (EPAS semantics).
		return nil, nil
	}
	if !policy.AllowBackdate {
		return fmt.Errorf("%w: accounting date %s is before the current date and %s does not allow backdating",
			ErrBackdateNotAllowed, date, docType), nil
	}
	days := int(today.Sub(parsed).Hours() / 24)
	if days > policy.MaxBackdateDays {
		return fmt.Errorf("%w: accounting date %s is %d days back, the %s policy allows %d",
			ErrTransactionDateExceedsBackdate, date, days, docType, policy.MaxBackdateDays), nil
	}
	if policy.CheckClosingLock {
		maxClosing, err := s.repo.MaxClosingDate(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		if maxClosing != "" {
			closingDate, err := time.Parse("2006-01-02", maxClosing)
			if err != nil {
				return nil, fmt.Errorf("parse max closing date %q: %w", maxClosing, err)
			}
			if parsed.Before(closingDate) {
				return fmt.Errorf("%w: accounting date %s precedes the latest closing entry %s",
					ErrPostingDateBeforeClosingLock, date, maxClosing), nil
			}
		}
	}
	return nil, nil
}

func trimDate(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// ── Closing read surface (used by the posting-case service + HTTP) ──

// ClosingCandidateRow is one HTTP item of GET /api/finance/closing/accounts.
// ClosingAmountMinor mirrors BalanceMinor — the FE prefills the closing
// amount with the account's full natural balance.
type ClosingCandidateRow struct {
	AccCode            string `json:"acc_code"`
	AccName            string `json:"acc_name"`
	AccPurpose         string `json:"acc_purpose"`
	AccNature          string `json:"acc_nature"`
	BalanceMinor       int64  `json:"balance_minor"`
	ClosingAmountMinor int64  `json:"closing_amount_minor"`
}

// ClosingCandidates lists the INC/EXP accounts with a positive natural
// balance as of onDate (default today) — the closing candidate picker.
func (s *PostingService) ClosingCandidates(ctx context.Context, tenantID, onDate string) ([]ClosingCandidateRow, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if trimDate(onDate) == "" {
		onDate = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", onDate); err != nil {
		return nil, fmt.Errorf("accounting_date must be YYYY-MM-DD")
	}
	rows, err := s.repo.ClosingCandidateAccounts(ctx, tenantID, onDate)
	if err != nil {
		return nil, err
	}
	out := make([]ClosingCandidateRow, 0, len(rows))
	for _, c := range rows {
		out = append(out, ClosingCandidateRow{
			AccCode:            c.AccountCode,
			AccName:            c.AccountName,
			AccPurpose:         c.AccPurpose,
			AccNature:          c.AccNature,
			BalanceMinor:       c.BalanceMinor,
			ClosingAmountMinor: c.BalanceMinor,
		})
	}
	return out, nil
}

// ClosingDest resolves the closing destination account for one purpose
// ("INC" | "EXP") from the FIN_CLOSING_*_DEST rules.
func (s *PostingService) ClosingDest(ctx context.Context, tenantID, purpose string) (string, error) {
	if err := requireTenant(tenantID); err != nil {
		return "", err
	}
	return s.repo.LoadClosingDest(ctx, tenantID, purpose)
}

// ResolveClosingAccount resolves one explicit account code (postable,
// effective on the date, default COA version) plus its purpose/nature.
func (s *PostingService) ResolveClosingAccount(ctx context.Context, tenantID, accCode, onDate string) (*repository.ClosingAccountRef, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	return s.repo.ResolveClosingAccount(ctx, tenantID, accCode, onDate)
}
