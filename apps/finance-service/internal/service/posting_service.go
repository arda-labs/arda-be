package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// PostingService implements the gRPC PostingService contract v0.2: resolve
// analytics to COA accounts, validate balance, persist entries idempotently,
// enqueue outbox events. Original entries are immutable; corrections are
// reversal entries. Tenant scope arrives via gRPC metadata (X-Tenant-Id) and
// is threaded through as an explicit parameter.
type PostingService struct {
	repo *repository.PostingRepository
	db   *sql.DB
}

func NewPostingService(repo *repository.PostingRepository, db *sql.DB) *PostingService {
	return &PostingService{repo: repo, db: db}
}

// ValidatePosting resolves every line and reports per-line + global results
// without writing anything.
func (s *PostingService) ValidatePosting(ctx context.Context, tenantID string, req *financev1.PostingRequest) (*financev1.ValidationResult, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	result, _, err := s.resolve(ctx, tenantID, req)
	return result, err
}

// PostTransaction persists one balanced entry. Idempotency: when the
// idempotency key was used before, the original response replays.
func (s *PostingService) PostTransaction(ctx context.Context, tenantID string, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if req.GetIdempotencyKey() != "" {
		if previous, err := s.repo.FindEntryByIdempotencyKey(ctx, tenantID, req.GetIdempotencyKey()); err == nil && previous != nil {
			return previous, nil
		}
	}
	result, resolved, err := s.resolve(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	if !result.GetValid() {
		return nil, fmt.Errorf("posting rejected: %s", strings.Join(result.GetGlobalErrors(), "; "))
	}

	ref := req.GetBusinessReference()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fin_journal_entries
			(tenant_id, accounting_date, currency_code, status, description,
			 business_domain, business_doc_type, business_doc_id, business_doc_code, case_id,
			 idempotency_key, created_by)
		VALUES ($1,$2,$3,'POSTED',$4,$5,$6,$7,$8,$9,$10,$11)`,
		tenantID, req.GetAccountingDate(), req.GetCurrencyCode(), req.GetDescription(),
		ref.GetDomain(), ref.GetDocumentType(),
		nullUUIDText(ref.GetDocumentId()), ref.GetDocumentCode(),
		nullUUIDText(ref.GetCaseId()), nullText(req.GetIdempotencyKey()),
		ref.GetDocumentCode()); err != nil {
		return nil, fmt.Errorf("insert entry: %w", err)
	}

	var entryID string
	var entryNo, createdAt string
	if err := tx.QueryRowContext(ctx, `
		SELECT id, entry_no, created_at FROM fin_journal_entries
		WHERE tenant_id = $1 AND idempotency_key = $2 AND business_doc_type = $3
		ORDER BY entry_no DESC LIMIT 1`,
		tenantID, req.GetIdempotencyKey(), ref.GetDocumentType(),
	).Scan(&entryID, &entryNo, &createdAt); err != nil {
		return nil, fmt.Errorf("fetch entry: %w", err)
	}

	for _, l := range resolved {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fin_journal_lines
				(tenant_id, entry_id, line_no, direction, coa_version, account_code, account_name,
				 amount_minor, currency_code, counterparty_code, counterparty_name, description, analytics)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			tenantID, entryID, l.GetLineNo(), l.GetDirection(),
			l.GetCoaVersion(), l.GetAccountCode(), l.GetAccountName(), l.GetAmountMinor(),
			l.GetCurrencyCode(), nullText(l.GetResolvedAnalytics().GetCustomerCode()), "",
			l.GetDescription(), analyticsJSON(l.GetResolvedAnalytics())); err != nil {
			return nil, fmt.Errorf("insert line %d: %w", l.GetLineNo(), err)
		}
	}

	if err := s.repo.InsertOutbox(ctx, tx, tenantID, entryID, outboxPayload(entryID, req)); err != nil {
		return nil, fmt.Errorf("enqueue outbox: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &financev1.PostingResponse{
		JournalEntryId: entryID,
		EntryNo:        parseI64(entryNo),
		PostedAt:       createdAt,
		Replayed:       false,
	}, nil
}

// ReverseTransaction creates the mirrored entry and marks the original
// reversed — in one transaction.
func (s *PostingService) ReverseTransaction(ctx context.Context, req *financev1.ReverseRequest) (*financev1.PostingResponse, error) {
	tenantID := req.GetTenantId()
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	original, err := s.repo.GetEntryForReversal(ctx, tenantID, req.GetJournalEntryId())
	if err != nil {
		return nil, err
	}
	reversalDate := req.GetAccountingDate()
	if reversalDate == "" {
		reversalDate = original.AccountingDate
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fin_journal_entries
			(tenant_id, accounting_date, currency_code, status, description,
			 business_domain, business_doc_type, business_doc_id, idempotency_key, created_by)
		VALUES ($1,$2,$3,'POSTED',$4,$5,$6,$7,$8,$9)`,
		tenantID, reversalDate, original.Currency,
		"REVERSAL: "+req.GetReason(), original.Domain, original.DocType,
		nullUUIDText(original.DocID), nullText(req.GetIdempotencyKey()), req.GetActor()); err != nil {
		return nil, fmt.Errorf("insert reversal: %w", err)
	}

	var entryID string
	var entryNo, createdAt string
	if err := tx.QueryRowContext(ctx, `
		SELECT id, entry_no, created_at FROM fin_journal_entries
		WHERE tenant_id = $1 AND idempotency_key = $2 ORDER BY entry_no DESC LIMIT 1`,
		tenantID, req.GetIdempotencyKey()).Scan(&entryID, &entryNo, &createdAt); err != nil {
		return nil, fmt.Errorf("fetch reversal: %w", err)
	}

	for i, l := range original.Lines {
		flipped := "CREDIT"
		if l.Direction == "CREDIT" {
			flipped = "DEBIT"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO fin_journal_lines
				(tenant_id, entry_id, line_no, direction, coa_version, account_code, account_name,
				 amount_minor, currency_code, counterparty_code, counterparty_name, description, analytics)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			tenantID, entryID, i+1, flipped, l.CoaVersion, l.AccountCode, l.AccountName,
			l.AmountMinor, original.Currency, l.CounterpartyCode, l.CounterpartyName,
			"REVERSAL", l.Analytics); err != nil {
			return nil, fmt.Errorf("insert reversal line %d: %w", i+1, err)
		}
	}

	if err := s.repo.MarkReversed(ctx, tx, tenantID, req.GetJournalEntryId(), entryID); err != nil {
		return nil, err
	}
	if err := s.repo.InsertOutbox(ctx, tx, tenantID, entryID,
		fmtBytes(fmt.Sprintf(`{"entry_id":%q,"reversal_of":%q}`, entryID, req.GetJournalEntryId()))); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &financev1.PostingResponse{
		JournalEntryId: entryID,
		EntryNo:        parseI64(entryNo),
		PostedAt:       createdAt,
		Replayed:       false,
	}, nil
}

// resolve maps request lines to accounts via registry-validated analytics.
func (s *PostingService) resolve(ctx context.Context, tenantID string, req *financev1.PostingRequest) (*financev1.ValidationResult, []*financev1.ValidationLine, error) {
	registry, err := s.repo.ListDimensionKeys(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	var globalErrors []string
	lines := make([]*financev1.ValidationLine, 0, len(req.GetLines()))
	coaVersion := ""
	balanced := map[string]map[string]int64{}

	// Period check first: fail fast before per-line work.
	if err := s.repo.EnsurePeriodOpen(ctx, tenantID, req.GetAccountingDate()); err != nil {
		globalErrors = append(globalErrors, "PERIOD_CLOSED")
	}

	for _, in := range req.GetLines() {
		vl := &financev1.ValidationLine{
			LineNo:       in.GetLineNo(),
			Direction:    in.GetDirection(),
			AmountMinor:  in.GetAmountMinor(),
			CurrencyCode: in.GetCurrencyCode(),
			Description:  in.GetDescription(),
		}
		if vl.GetCurrencyCode() == "" {
			vl.CurrencyCode = req.GetCurrencyCode()
		}
		if vl.GetAmountMinor() <= 0 {
			vl.Errors = append(vl.Errors, "INVALID_AMOUNT")
		}
		if in.GetDirection() != "DEBIT" && in.GetDirection() != "CREDIT" {
			vl.Errors = append(vl.Errors, "INVALID_DIRECTION")
		}
		// Dimension registry validation (contract §8.2): unknown keys rejected.
		analytics := in.GetAnalytics()
		for key := range analytics.GetDimensions() {
			if _, ok := registry[key]; !ok {
				vl.Errors = append(vl.Errors, "UNKNOWN_DIMENSION:"+key)
			}
		}
		classification := analytics.GetAccClassification()
		if classification == "" {
			vl.Errors = append(vl.Errors, "CLASSIFICATION_REQUIRED")
		} else {
			resolved, err := s.repo.ResolveAccount(ctx, tenantID, classification,
				analytics.GetDebtGroupCode(), vl.GetCurrencyCode(), req.GetAccountingDate())
			if err != nil {
				vl.Errors = append(vl.Errors, "ACCOUNT_UNRESOLVED")
			} else {
				vl.Resolved = true
				vl.AccountCode = resolved.AccountCode
				vl.AccountName = resolved.AccountName
				vl.CoaVersion = resolved.CoaVersion
				vl.ResolvedAnalytics = analytics
				if coaVersion == "" {
					coaVersion = resolved.CoaVersion
				}
			}
		}
		if vl.GetCurrencyCode() == "" {
			vl.CurrencyCode = "VND"
		}
		if balanced[vl.GetCurrencyCode()] == nil {
			balanced[vl.GetCurrencyCode()] = map[string]int64{}
		}
		balanced[vl.GetCurrencyCode()][vl.GetDirection()] += vl.GetAmountMinor()
		lines = append(lines, vl)
	}

	for currency, sums := range balanced {
		if sums["DEBIT"] != sums["CREDIT"] {
			globalErrors = append(globalErrors, fmt.Sprintf("UNBALANCED:%s", currency))
		}
	}
	if len(lines) == 0 {
		globalErrors = append(globalErrors, "NO_LINES")
	}
	valid := len(globalErrors) == 0
	for _, l := range lines {
		if len(l.GetErrors()) > 0 {
			valid = false
		}
	}
	return &financev1.ValidationResult{
		Valid:        valid,
		Lines:        lines,
		GlobalErrors: globalErrors,
		CoaVersionId: coaVersion,
	}, lines, nil
}

func requireTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("tenant scope is required")
	}
	return nil
}

func analyticsJSON(a *financev1.Analytics) []byte {
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

func outboxPayload(entryID string, req *financev1.PostingRequest) []byte {
	debit, credit := int64(0), int64(0)
	for _, l := range req.GetLines() {
		if l.GetDirection() == "DEBIT" {
			debit += l.GetAmountMinor()
		} else {
			credit += l.GetAmountMinor()
		}
	}
	tmpl := `{"entry_id":%q,"business_domain":%q,"business_doc_type":%q,"business_doc_id":%q,"accounting_date":%q,"currency_code":%q,"debit_minor":%d,"credit_minor":%d}`
	return fmtBytes(fmt.Sprintf(tmpl,
		entryID, req.GetBusinessReference().GetDomain(), req.GetBusinessReference().GetDocumentType(),
		req.GetBusinessReference().GetDocumentId(), req.GetAccountingDate(), req.GetCurrencyCode(), debit, credit))
}

func nullUUIDText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseI64(s string) int64 {
	var out int64
	_, _ = fmt.Sscanf(s, "%d", &out)
	return out
}

func fmtBytes(s string) []byte { return []byte(s) }

// ── Journal read + Opening Balance (P1a.5/P1a.6) ──

// JournalFilter narrows the journal listing.
type JournalFilter struct {
	FromDate     string
	ToDate       string
	DocumentType string
	Limit        int
}

// JournalEntryRow is one listed entry header.
type JournalEntryRow struct {
	ID             string `json:"id"`
	EntryNo        int64  `json:"entry_no"`
	AccountingDate string `json:"accounting_date"`
	CurrencyCode   string `json:"currency_code"`
	Status         string `json:"status"`
	Description    string `json:"description"`
	BusinessDomain string `json:"business_domain"`
	DocumentType   string `json:"document_type"`
	DocumentCode   string `json:"document_code"`
	CaseID         string `json:"case_id"`
	CreatedAt      string `json:"created_at"`
}

// ListJournal returns recent entries (header only) ordered newest first.
func (s *PostingService) ListJournal(ctx context.Context, tenantID string, f JournalFilter) ([]JournalEntryRow, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, entry_no, accounting_date::text, currency_code, status,
		       COALESCE(description,''), business_domain, business_doc_type,
		       COALESCE(business_doc_code,''), COALESCE(case_id::text,''), created_at::text
		FROM fin_journal_entries
		WHERE tenant_id = $1
		  AND ($2 = '' OR accounting_date >= $2::date)
		  AND ($3 = '' OR accounting_date <= $3::date)
		  AND ($4 = '' OR business_doc_type = $4)
		ORDER BY entry_no DESC
		LIMIT $5`, tenantID, f.FromDate, f.ToDate, f.DocumentType, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalEntryRow{}
	for rows.Next() {
		var e JournalEntryRow
		if err := rows.Scan(&e.ID, &e.EntryNo, &e.AccountingDate, &e.CurrencyCode, &e.Status,
			&e.Description, &e.BusinessDomain, &e.DocumentType, &e.DocumentCode, &e.CaseID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// OpeningBalanceInput is one opening-balance line (per account+currency).
type OpeningBalanceInput struct {
	AccountingDate string
	CoaVersion     string
	AccountCode    string
	CurrencyCode   string
	Direction      string
	AmountMinor    int64
	Description    string
	SourceKey      string
	Actor          string
}

// UpsertOpeningBalance stores one opening-balance row (idempotent per
// tenant+date+version+account+currency).
func (s *PostingService) UpsertOpeningBalance(ctx context.Context, tenantID string, in OpeningBalanceInput) (string, error) {
	if in.AccountingDate == "" || in.AccountCode == "" || in.AmountMinor <= 0 {
		return "", fmt.Errorf("accounting_date, account_code and positive amount_minor are required")
	}
	if in.Direction != "DEBIT" && in.Direction != "CREDIT" {
		return "", fmt.Errorf("direction must be DEBIT or CREDIT")
	}
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO fin_opening_balances
			(tenant_id, accounting_date, coa_version, account_code, currency_code, direction, amount_minor, description, source_key, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (tenant_id, accounting_date, coa_version, account_code, currency_code)
		DO UPDATE SET direction = EXCLUDED.direction, amount_minor = EXCLUDED.amount_minor,
		              description = EXCLUDED.description, source_key = EXCLUDED.source_key,
		              updated_by = EXCLUDED.created_by, updated_at = now(), version = fin_opening_balances.version + 1
		RETURNING id`,
		tenantID, in.AccountingDate, in.CoaVersion, in.AccountCode, in.CurrencyCode,
		in.Direction, in.AmountMinor, in.Description, nullText(in.SourceKey), in.Actor).Scan(&id)
	return id, err
}

// OpeningBalanceRow is one listed opening-balance line.
type OpeningBalanceRow struct {
	AccountingDate string `json:"accounting_date"`
	CoaVersion     string `json:"coa_version"`
	AccountCode    string `json:"account_code"`
	CurrencyCode   string `json:"currency_code"`
	Direction      string `json:"direction"`
	AmountMinor    int64  `json:"amount_minor"`
	Description    string `json:"description"`
	SourceKey      string `json:"source_key"`
}

// ListOpeningBalances returns the opening balances effective on a date.
func (s *PostingService) ListOpeningBalances(ctx context.Context, tenantID, onDate string) ([]OpeningBalanceRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT accounting_date::text, coa_version, account_code, currency_code, direction, amount_minor,
		       COALESCE(description,''), COALESCE(source_key,'')
		FROM fin_opening_balances
		WHERE tenant_id = $1
		  AND accounting_date = (
		        SELECT MAX(accounting_date) FROM fin_opening_balances
		        WHERE tenant_id = $1 AND accounting_date <= $2::date)
		ORDER BY account_code`, tenantID, onDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OpeningBalanceRow{}
	for rows.Next() {
		var r OpeningBalanceRow
		if err := rows.Scan(&r.AccountingDate, &r.CoaVersion, &r.AccountCode, &r.CurrencyCode,
			&r.Direction, &r.AmountMinor, &r.Description, &r.SourceKey); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
