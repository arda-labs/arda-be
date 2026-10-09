// Package docno allocates immutable business document IDs and independently
// mutable display numbers. Callers must use the same transaction for Issue
// and the business write; any Issue error requires rolling the transaction
// back so a failed document does not consume a counter value.
package docno

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidSeries          = errors.New("invalid document number series")
	ErrSequenceOverflow       = errors.New("DOC_SEQ_OVERFLOW")
	ErrDocTypeRuleNotFound    = errors.New("DOC_TYPE_RULE_NOT_FOUND")
	ErrDocumentNumberExists   = errors.New("document number already exists or is retired")
	ErrDocumentNumberNotFound = errors.New("document number not found")
)

type ResetPeriod string

const (
	ResetNone  ResetPeriod = "NONE"
	ResetYear  ResetPeriod = "YEAR"
	ResetMonth ResetPeriod = "MONTH"
	ResetDay   ResetPeriod = "DAY"
)

type Series struct {
	ID             string
	TenantID       string
	OrgCode        string
	DocumentType   string
	Version        int
	EffectiveFrom  time.Time
	EffectiveTo    *time.Time
	ResetPeriod    ResetPeriod
	Pattern        string
	SequenceStart  int64
	SequenceWidth  int
	OverflowPolicy string
	Status         string
}

type Number struct {
	DocumentID   string
	TenantID     string
	SeriesID     string
	DisplayNo    string
	PeriodKey    string
	SequenceNo   int64
	BusinessDate time.Time
}

var sequenceToken = regexp.MustCompile(`\{SEQ:([1-9][0-9]?)\}`)

// ValidateSeries checks a series before it can be used. Period tokens are
// required to match the reset period so a daily series cannot accidentally
// reuse one number across different business dates.
func ValidateSeries(series Series) error {
	if strings.TrimSpace(series.ID) == "" || strings.TrimSpace(series.TenantID) == "" ||
		strings.TrimSpace(series.DocumentType) == "" || series.Version < 1 ||
		strings.TrimSpace(series.Pattern) == "" || series.SequenceStart < 1 || series.SequenceWidth < 1 || series.SequenceWidth > 18 || series.EffectiveFrom.IsZero() {
		return fmt.Errorf("%w: required fields or sequence range", ErrInvalidSeries)
	}
	if series.OverflowPolicy != "REJECT" {
		return fmt.Errorf("%w: unsupported overflow policy %q", ErrInvalidSeries, series.OverflowPolicy)
	}
	if series.Status != "ACTIVE" {
		return fmt.Errorf("%w: series is not ACTIVE", ErrInvalidSeries)
	}
	if series.EffectiveTo != nil && series.EffectiveTo.Before(series.EffectiveFrom) {
		return fmt.Errorf("%w: effective_to precedes effective_from", ErrInvalidSeries)
	}
	if len(sequenceToken.FindAllString(series.Pattern, -1)) != 1 {
		return fmt.Errorf("%w: pattern must have exactly one {SEQ:n} token", ErrInvalidSeries)
	}
	match := sequenceToken.FindStringSubmatch(series.Pattern)
	width, _ := strconv.Atoi(match[1])
	if width != series.SequenceWidth {
		return fmt.Errorf("%w: pattern sequence width %d does not match configured width %d", ErrInvalidSeries, width, series.SequenceWidth)
	}
	needYear, needMonth, needDay := false, false, false
	switch series.ResetPeriod {
	case ResetNone:
	case ResetYear:
		needYear = true
	case ResetMonth:
		needYear, needMonth = true, true
	case ResetDay:
		needYear, needMonth, needDay = true, true, true
	default:
		return fmt.Errorf("%w: unknown reset period %q", ErrInvalidSeries, series.ResetPeriod)
	}
	for token, required := range map[string]bool{"{YYYY}": needYear, "{MM}": needMonth, "{DD}": needDay} {
		if strings.Contains(series.Pattern, token) != required {
			return fmt.Errorf("%w: reset period %s requires date tokens YYYY=%t MM=%t DD=%t", ErrInvalidSeries, series.ResetPeriod, needYear, needMonth, needDay)
		}
	}
	known := sequenceToken.ReplaceAllString(series.Pattern, "")
	for _, token := range []string{"{YYYY}", "{MM}", "{DD}"} {
		known = strings.ReplaceAll(known, token, "")
	}
	if strings.ContainsAny(known, "{}") {
		return fmt.Errorf("%w: unsupported pattern token", ErrInvalidSeries)
	}
	return nil
}

// Lookup resolves an active or retired display number to the current record.
// Retired numbers remain aliases, while the returned DisplayNo is current.
func Lookup(ctx context.Context, db *sql.DB, tenantID, displayNo string) (Number, error) {
	var number Number
	err := db.QueryRowContext(ctx, `
		SELECT n.document_id, n.tenant_id, n.series_id::text, n.display_no,
		       n.period_key, n.sequence_no, n.business_date
		FROM doc_number_alias a
		JOIN document_number n
		  ON n.tenant_id = a.tenant_id AND n.document_id = a.document_id
		WHERE a.tenant_id = $1 AND a.display_no = $2 AND n.status = 'ACTIVE'`, tenantID, displayNo).
		Scan(&number.DocumentID, &number.TenantID, &number.SeriesID, &number.DisplayNo, &number.PeriodKey, &number.SequenceNo, &number.BusinessDate)
	if errors.Is(err, sql.ErrNoRows) {
		return Number{}, ErrDocumentNumberNotFound
	}
	if err != nil {
		return Number{}, fmt.Errorf("lookup document number: %w", err)
	}
	return number, nil
}

// PeriodKey produces the counter partition from the supplied business date.
// It never consults the host clock.
func PeriodKey(reset ResetPeriod, businessDate time.Time) (string, error) {
	date := businessDate.Format("2006-01-02")
	switch reset {
	case ResetNone:
		return "ALL", nil
	case ResetYear:
		return businessDate.Format("2006"), nil
	case ResetMonth:
		return businessDate.Format("2006-01"), nil
	case ResetDay:
		return date, nil
	default:
		return "", fmt.Errorf("%w: unknown reset period %q", ErrInvalidSeries, reset)
	}
}

// FormatDisplayNumber renders one validated sequence without truncating it.
func FormatDisplayNumber(series Series, businessDate time.Time, sequence int64) (string, error) {
	if err := ValidateSeries(series); err != nil {
		return "", err
	}
	if businessDate.IsZero() {
		return "", fmt.Errorf("%w: business date is required", ErrInvalidSeries)
	}
	day := time.Date(businessDate.Year(), businessDate.Month(), businessDate.Day(), 0, 0, 0, 0, time.UTC)
	from := time.Date(series.EffectiveFrom.Year(), series.EffectiveFrom.Month(), series.EffectiveFrom.Day(), 0, 0, 0, 0, time.UTC)
	if day.Before(from) || (series.EffectiveTo != nil && day.After(time.Date(series.EffectiveTo.Year(), series.EffectiveTo.Month(), series.EffectiveTo.Day(), 0, 0, 0, 0, time.UTC))) {
		return "", fmt.Errorf("%w: business date is outside series effective dates", ErrInvalidSeries)
	}
	if sequence < series.SequenceStart {
		return "", fmt.Errorf("%w: sequence precedes configured start", ErrInvalidSeries)
	}
	sequenceText := strconv.FormatInt(sequence, 10)
	if len(sequenceText) > series.SequenceWidth {
		return "", ErrSequenceOverflow
	}
	sequenceText = strings.Repeat("0", series.SequenceWidth-len(sequenceText)) + sequenceText
	pattern := strings.ReplaceAll(series.Pattern, "{YYYY}", businessDate.Format("2006"))
	pattern = strings.ReplaceAll(pattern, "{MM}", businessDate.Format("01"))
	pattern = strings.ReplaceAll(pattern, "{DD}", businessDate.Format("02"))
	pattern = sequenceToken.ReplaceAllString(pattern, sequenceText)
	return pattern, nil
}

// Issue allocates and persists a number inside the caller's transaction.
// The business date is explicit so callers can bind numbering to the same
// canonical date used by their domain operation.
func Issue(ctx context.Context, tx *sql.Tx, series Series, documentID string, businessDate time.Time) (Number, error) {
	if err := ValidateSeries(series); err != nil {
		return Number{}, err
	}
	if err := validatePersistedSeries(ctx, tx, series); err != nil {
		return Number{}, err
	}
	if strings.TrimSpace(documentID) == "" || businessDate.IsZero() {
		return Number{}, fmt.Errorf("%w: document ID and business date are required", ErrInvalidSeries)
	}
	periodKey, err := PeriodKey(series.ResetPeriod, businessDate)
	if err != nil {
		return Number{}, err
	}
	var sequence int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO doc_counter (series_id, period_key, last_value)
		VALUES ($1, $2, $3)
		ON CONFLICT (series_id, period_key) DO UPDATE
		SET last_value = doc_counter.last_value + 1
		RETURNING last_value`, series.ID, periodKey, series.SequenceStart).Scan(&sequence)
	if err != nil {
		return Number{}, fmt.Errorf("allocate document sequence: %w", err)
	}
	displayNo, err := FormatDisplayNumber(series, businessDate, sequence)
	if err != nil {
		return Number{}, err
	}
	number := Number{DocumentID: documentID, TenantID: series.TenantID, SeriesID: series.ID, DisplayNo: displayNo, PeriodKey: periodKey, SequenceNo: sequence, BusinessDate: businessDate}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO document_number (tenant_id, document_id, series_id, display_no, period_key, sequence_no, business_date)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT DO NOTHING`, number.TenantID, number.DocumentID, number.SeriesID, number.DisplayNo, number.PeriodKey, number.SequenceNo, number.BusinessDate)
	if err != nil {
		return Number{}, fmt.Errorf("persist document number: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return Number{}, ErrDocumentNumberExists
	}
	result, err = tx.ExecContext(ctx, `INSERT INTO doc_number_alias (tenant_id, display_no, document_id, status) VALUES ($1,$2,$3,'ACTIVE') ON CONFLICT DO NOTHING`, number.TenantID, number.DisplayNo, number.DocumentID)
	if err != nil {
		return Number{}, fmt.Errorf("reserve document number: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return Number{}, ErrDocumentNumberExists
	}
	return number, nil
}

func validatePersistedSeries(ctx context.Context, tx *sql.Tx, series Series) error {
	var orgCode, documentType, effectiveFrom, effectiveTo, resetPeriod, pattern, overflowPolicy, status string
	var version, sequenceWidth int
	var sequenceStart int64
	err := tx.QueryRowContext(ctx, `
		SELECT org_code, document_type, version, effective_from::text,
		       COALESCE(effective_to::text, ''), reset_period, pattern,
		       sequence_start, sequence_width, overflow_policy, status
		FROM doc_series WHERE tenant_id=$1 AND id=$2 FOR SHARE`, series.TenantID, series.ID).
		Scan(&orgCode, &documentType, &version, &effectiveFrom, &effectiveTo, &resetPeriod, &pattern, &sequenceStart, &sequenceWidth, &overflowPolicy, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: series is not registered for tenant", ErrInvalidSeries)
	}
	if err != nil {
		return fmt.Errorf("load document number series: %w", err)
	}
	inputTo := ""
	if series.EffectiveTo != nil {
		inputTo = series.EffectiveTo.Format("2006-01-02")
	}
	if orgCode != series.OrgCode || documentType != series.DocumentType || version != series.Version ||
		effectiveFrom != series.EffectiveFrom.Format("2006-01-02") || effectiveTo != inputTo ||
		ResetPeriod(resetPeriod) != series.ResetPeriod || pattern != series.Pattern || sequenceStart != series.SequenceStart ||
		sequenceWidth != series.SequenceWidth || overflowPolicy != series.OverflowPolicy || status != series.Status {
		return fmt.Errorf("%w: supplied series does not match its persisted configuration", ErrInvalidSeries)
	}
	return nil
}

// RecordGap records a number intentionally skipped after it was allocated.
// It does not create or advance a counter value.
func RecordGap(ctx context.Context, tx *sql.Tx, number Number, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("document number gap reason is required")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO doc_gap (tenant_id, series_id, period_key, sequence_no, reason) VALUES ($1,$2,$3,$4,$5)`, number.TenantID, number.SeriesID, number.PeriodKey, number.SequenceNo, strings.TrimSpace(reason))
	return err
}

type RuleSelector struct {
	TenantID       string
	OrgCode        string
	Ledger         string
	PaymentMethod  string
	EntryDirection string
}

type DocumentTypeRule struct {
	OrgCode        string
	Ledger         string
	PaymentMethod  string
	EntryDirection string
	DocumentType   string
	Priority       int
}

// ResolveDocumentTypeRule requires callers to opt into a GLOBAL fallback.
// If an org-specific rule is absent, the explicit fallback is logged.
func ResolveDocumentTypeRule(selector RuleSelector, rules []DocumentTypeRule, allowGlobalFallback bool, logger *slog.Logger) (DocumentTypeRule, error) {
	match := func(rule DocumentTypeRule, org string) bool {
		return rule.OrgCode == org && rule.Ledger == selector.Ledger && rule.PaymentMethod == selector.PaymentMethod && rule.EntryDirection == selector.EntryDirection
	}
	var orgRules []DocumentTypeRule
	var globalRules []DocumentTypeRule
	for _, rule := range rules {
		if match(rule, selector.OrgCode) && selector.OrgCode != "" {
			orgRules = append(orgRules, rule)
		}
		if rule.OrgCode == "" && match(rule, "") {
			globalRules = append(globalRules, rule)
		}
	}
	choose := func(candidates []DocumentTypeRule) (DocumentTypeRule, bool) {
		if len(candidates) == 0 {
			return DocumentTypeRule{}, false
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].Priority < candidates[j].Priority })
		return candidates[0], true
	}
	if selected, ok := choose(orgRules); ok {
		return selected, nil
	}
	if !allowGlobalFallback {
		return DocumentTypeRule{}, ErrDocTypeRuleNotFound
	}
	if selected, ok := choose(globalRules); ok {
		if logger != nil && selector.OrgCode != "" {
			logger.Warn("document type rule used explicit GLOBAL fallback", "tenant_id", selector.TenantID, "org_code", selector.OrgCode, "ledger", selector.Ledger, "payment_method", selector.PaymentMethod, "entry_direction", selector.EntryDirection)
		}
		return selected, nil
	}
	return DocumentTypeRule{}, ErrDocTypeRuleNotFound
}
