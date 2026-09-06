package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// PostingRepository owns all journal SQL for the PostingService.
type PostingRepository struct {
	db *sql.DB
}

func NewPostingRepository(db *sql.DB) *PostingRepository {
	return &PostingRepository{db: db}
}

// ResolvedAccount is a COA classification resolution snapshot.
type ResolvedAccount struct {
	CoaVersion  string
	AccountCode string
	AccountName string
}

// ResolveAccount maps an analytics classification to a COA account effective
// on the accounting date (exact debt-group/currency match wins, then blank).
func (r *PostingRepository) ResolveAccount(ctx context.Context, tenantID, classification, debtGroup, currency, onDate string) (*ResolvedAccount, error) {
	if onDate == "" {
		onDate = time.Now().Format("2006-01-02")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT m.coa_version, m.coa_acc_code, COALESCE(a.name, '')
		FROM fin_acc_class_coa_maps m
		JOIN fin_coa_accounts a
		  ON a.tenant_id = m.tenant_id AND a.version_code = m.coa_version AND a.acc_code = m.coa_acc_code
		WHERE m.tenant_id = $1
		  AND m.classification = $2
		  AND m.effective_date <= $3::date
		  AND (m.expiry_date IS NULL OR m.expiry_date > $3::date)
		  AND m.debt_group_code IN ($4, '')
		  AND m.currency_code IN ($5, '')
		ORDER BY (m.debt_group_code <> '') DESC, (m.currency_code <> '') DESC
		LIMIT 1`,
		tenantID, classification, onDate, debtGroup, currency)
	var out ResolvedAccount
	err := row.Scan(&out.CoaVersion, &out.AccountCode, &out.AccountName)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no effective COA mapping for classification %q on %s", classification, onDate)
	}
	return &out, err
}

// RegisteredDimension is one whitelisted analytics dimension key.
type RegisteredDimension struct {
	Key         string
	DataType    string
	RequiredFor []string
	RefCatalog  sql.NullString
}

// ListDimensionKeys loads the registry (global + tenant rows).
func (r *PostingRepository) ListDimensionKeys(ctx context.Context, tenantID string) (map[string]RegisteredDimension, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT key, data_type, required_for, ref_catalog
		FROM fin_dimension_keys
		WHERE is_active AND (tenant_id IS NULL OR tenant_id = $1)`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]RegisteredDimension{}
	for rows.Next() {
		var d RegisteredDimension
		var req []byte
		if err := rows.Scan(&d.Key, &d.DataType, &req, &d.RefCatalog); err != nil {
			return nil, err
		}
		for _, t := range parseTextArray(string(req)) {
			d.RequiredFor = append(d.RequiredFor, t)
		}
		out[d.Key] = d
	}
	return out, rows.Err()
}

// AccountingRule is one seeded posting rule line for a document type.
type AccountingRule struct {
	LineNo      int32
	Direction   string
	ResType     string
	AccountRef  sql.NullString
	ClassCode   sql.NullString
	Dimensions  []string
	Description sql.NullString
}

// ListRules returns the active rule lines for a document type.
func (r *PostingRepository) ListRules(ctx context.Context, tenantID, documentType string) ([]AccountingRule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT line_no, direction, resolution_type, account_ref, acc_classification, required_dimensions, description_template
		FROM fin_accounting_rules
		WHERE tenant_id = $1 AND document_type = $2 AND is_active
		ORDER BY line_no`, tenantID, documentType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountingRule
	for rows.Next() {
		var rule AccountingRule
		var dims []byte
		if err := rows.Scan(&rule.LineNo, &rule.Direction, &rule.ResType, &rule.AccountRef, &rule.ClassCode, &dims, &rule.Description); err != nil {
			return nil, err
		}
		rule.Dimensions = parseTextArray(string(dims))
		out = append(out, rule)
	}
	return out, rows.Err()
}

// EnsurePeriodOpen returns an error when accounting_date falls in a closed
// or missing period.
func (r *PostingRepository) EnsurePeriodOpen(ctx context.Context, tenantID, accountingDate string) error {
	row := r.db.QueryRowContext(ctx, `
		SELECT status FROM fin_periods
		WHERE tenant_id = $1 AND $2::date BETWEEN start_date AND end_date`,
		tenantID, accountingDate)
	var status string
	if err := row.Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("no accounting period open for %s", accountingDate)
		}
		return err
	}
	if status != "OPEN" {
		return fmt.Errorf("PERIOD_CLOSED: period containing %s is %s", accountingDate, status)
	}
	return nil
}

// FindEntryByIdempotencyKey returns the entry id + entry_no of a previously
// posted entry, or nil when the key is unused.
func (r *PostingRepository) FindEntryByIdempotencyKey(ctx context.Context, tenantID, key string) (*financev1.PostingResponse, error) {
	if key == "" {
		return nil, nil
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, entry_no, version, created_at FROM fin_journal_entries
		WHERE tenant_id = $1 AND idempotency_key = $2`, tenantID, key)
	var id string
	var entryNo int64
	var version int32
	var postedAt time.Time
	err := row.Scan(&id, &entryNo, &version, &postedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &financev1.PostingResponse{
		JournalEntryId: id,
		EntryNo:        entryNo,
		Version:        version,
		PostedAt:       postedAt.Format(time.RFC3339),
		Replayed:       true,
	}, nil
}

// InsertEntry + InsertLines persist one journal entry and its lines in one
// transaction. The caller owns tx lifecycle.
func (r *PostingRepository) InsertEntry(ctx context.Context, tx *sql.Tx, tenantID string, e *financev1.PostingResponse, accountingDate, currency, description string, ref *financev1.BusinessReference, idempotencyKey, actor string) error {
	var docID, caseID any
	if ref != nil {
		docID = nullText(ref.DocumentId)
		caseID = nullText(ref.CaseId)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO fin_journal_entries
			(tenant_id, accounting_date, currency_code, status, description,
			 business_domain, business_doc_type, business_doc_id, business_doc_code, case_id,
			 idempotency_key, created_by)
		VALUES ($1,$2,$3,'POSTED',$4,$5,$6,$7,$8,$9,$10,$11)`,
		tenantID, accountingDate, currency, description,
		ref.GetDomain(), ref.GetDocumentType(), docID, ref.GetDocumentCode(), caseID,
		nullText(idempotencyKey), actor)
	_ = e
	return err
}

func (r *PostingRepository) InsertLines(ctx context.Context, tx *sql.Tx, tenantID, entryID string, lines []*financev1.ValidationLine) error {
	for _, l := range lines {
		analytics := encodeAnalytics(l.ResolvedAnalytics)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO fin_journal_lines
				(tenant_id, entry_id, line_no, direction, coa_version, account_code, account_name,
				 amount_minor, currency_code, counterparty_code, counterparty_name, description, analytics)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			tenantID, entryID, l.LineNo, l.Direction, l.GetCoaVersion(), l.AccountCode, l.AccountName,
			l.AmountMinor, l.CurrencyCode, nullText(l.GetResolvedAnalytics().GetCustomerCode()), "", l.Description, analytics)
		if err != nil {
			return err
		}
	}
	return nil
}

// LastEntryID returns the id of the most recent entry for idempotency replay.
func (r *PostingRepository) LastEntryID(ctx context.Context, tx *sql.Tx) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM fin_journal_entries ORDER BY entry_no DESC LIMIT 1`).Scan(&id)
	return id, err
}

// GetEntryForReversal loads the original entry header + lines.
func (r *PostingRepository) GetEntryForReversal(ctx context.Context, tenantID, entryID string) (*EntryForReversal, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT accounting_date::text, currency_code, status, reversed_by_entry_id, business_domain, business_doc_type, COALESCE(business_doc_id::text,'')
		FROM fin_journal_entries WHERE tenant_id = $1 AND id = $2`, tenantID, entryID)
	var e EntryForReversal
	var reversed sql.NullString
	err := row.Scan(&e.AccountingDate, &e.Currency, &e.Status, &reversed, &e.Domain, &e.DocType, &e.DocID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("journal entry not found")
	}
	if err != nil {
		return nil, err
	}
	if reversed.Valid {
		return nil, fmt.Errorf("entry already reversed")
	}
	if e.Status != "POSTED" {
		return nil, fmt.Errorf("entry status %s cannot be reversed", e.Status)
	}
	lines, err := r.db.QueryContext(ctx, `
		SELECT line_no, direction, coa_version, account_code, account_name, amount_minor, currency_code, counterparty_code, counterparty_name, description, analytics
		FROM fin_journal_lines WHERE tenant_id = $1 AND entry_id = $2 ORDER BY line_no`, tenantID, entryID)
	if err != nil {
		return nil, err
	}
	defer lines.Close()
	for lines.Next() {
		var l JournalLineRow
		if err := lines.Scan(&l.LineNo, &l.Direction, &l.CoaVersion, &l.AccountCode, &l.AccountName, &l.AmountMinor, &l.Currency, &l.CounterpartyCode, &l.CounterpartyName, &l.Description, &l.Analytics); err != nil {
			return nil, err
		}
		e.Lines = append(e.Lines, l)
	}
	return &e, lines.Err()
}

// MarkReversed links the original entry to its reversal.
func (r *PostingRepository) MarkReversed(ctx context.Context, tx *sql.Tx, tenantID, originalID, reversalID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE fin_journal_entries SET reversed_by_entry_id = $3, updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, tenantID, originalID, reversalID)
	return err
}

// InsertOutbox enqueues the journal.posted event in the same tx.
func (r *PostingRepository) InsertOutbox(ctx context.Context, tx *sql.Tx, tenantID, entryID string, payload []byte) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO fin_outbox (tenant_id, event_type, aggregate_id, payload)
		VALUES ($1, 'finance.journal.posted.v1', $2, $3)`, tenantID, entryID, payload)
	return err
}

// EntryForReversal is the header + lines needed to build a reversal entry.
type EntryForReversal struct {
	AccountingDate string
	Currency       string
	Status         string
	Domain         string
	DocType        string
	DocID          string
	Lines          []JournalLineRow
}

// JournalLineRow is one stored line.
type JournalLineRow struct {
	LineNo           int32
	Direction        string
	CoaVersion       string
	AccountCode      string
	AccountName      string
	AmountMinor      int64
	Currency         string
	CounterpartyCode sql.NullString
	CounterpartyName sql.NullString
	Description      sql.NullString
	Analytics        []byte
}

func parseTextArray(s string) []string {
	trimmed := trimSpace(s)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	trimmed = trimSpace(trimmed[1 : len(trimmed)-1])
	if trimmed == "" {
		return nil
	}
	var out []string
	for _, part := range splitComma(trimmed) {
		out = append(out, trimSpace(trimQuotes(part)))
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func encodeAnalytics(a *financev1.Analytics) []byte {
	if a == nil {
		return []byte("{}")
	}
	m := map[string]string{
		"acc_classification": a.GetAccClassification(),
		"debt_group_code":    a.GetDebtGroupCode(),
		"org_unit_code":      a.GetOrgUnitCode(),
		"fund_source_code":   a.GetFundSourceCode(),
		"customer_code":      a.GetCustomerCode(),
		"contract_code":      a.GetContractCode(),
	}
	for k, v := range a.GetDimensions() {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}
