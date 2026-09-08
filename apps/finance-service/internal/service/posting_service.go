package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
// idempotency key already belongs to a POSTED/REVERSED entry, that response
// replays; when it belongs to a PENDING entry (created by ReservePosting),
// the pending entry converts to POSTED — its reserved amounts graduate to
// posted counters and the outbox event fires. Direct posts (no pending twin)
// book straight to posted counters without an availability check — they are
// system-executed decisions (EOD jobs, cash, synchronous settles); flows
// with a human approval window must go through ReservePosting.
func (s *PostingService) PostTransaction(ctx context.Context, tenantID string, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if req.GetIdempotencyKey() != "" {
		previous, err := s.repo.FindEntryByIdempotencyKey(ctx, tenantID, req.GetIdempotencyKey())
		if err != nil {
			return nil, err
		}
		if previous != nil {
			switch previous.GetStatus() {
			case "PENDING":
				return s.postPendingEntry(ctx, tenantID, previous.GetJournalEntryId(), req.GetMetadata()["actor"])
			case "VOID":
				return nil, fmt.Errorf("idempotency key %q was released (VOID); issue a new posting", req.GetIdempotencyKey())
			default:
				return previous, nil
			}
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
			 idempotency_key, created_by, posted_at)
		VALUES ($1,$2,$3,'POSTED',$4,$5,$6,$7,$8,$9,$10,$11,now())`,
		tenantID, req.GetAccountingDate(), req.GetCurrencyCode(), req.GetDescription(),
		ref.GetDomain(), ref.GetDocumentType(),
		nullUUIDText(ref.GetDocumentId()), ref.GetDocumentCode(),
		nullUUIDText(ref.GetCaseId()), nullText(req.GetIdempotencyKey()),
		metadataActor(req, ref.GetDocumentCode())); err != nil {
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

	if err := s.postLinesToBalances(ctx, tx, tenantID, resolved); err != nil {
		return nil, err
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
		Status:         "POSTED",
		Replayed:       false,
	}, nil
}

// ReservePosting creates the PENDING entry and holds its amounts on
// fin_account_balances (the two-phase balance reservation). Every outflow
// line must fit inside the account's available value — opening + posted +
// reserved — so N pending proposals cannot together overdraft an account.
// Idempotency: replaying the same content returns the pending entry; a
// changed proposal releases the stale hold and re-reserves (the EPAS
// updateTransaction rebuild semantics that make the maker-edit loop safe).
func (s *PostingService) ReservePosting(ctx context.Context, tenantID string, req *financev1.PostingRequest) (*financev1.PostingResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if key := req.GetIdempotencyKey(); key != "" {
		previous, err := s.repo.FindEntryByIdempotencyKey(ctx, tenantID, key)
		if err != nil {
			return nil, err
		}
		if previous != nil {
			handled, err := s.handleExistingForReserve(ctx, tenantID, req, previous)
			if err != nil || handled {
				return previous, err
			}
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
		VALUES ($1,$2,$3,'PENDING',$4,$5,$6,$7,$8,$9,$10,$11)`,
		tenantID, req.GetAccountingDate(), req.GetCurrencyCode(), req.GetDescription(),
		ref.GetDomain(), ref.GetDocumentType(),
		nullUUIDText(ref.GetDocumentId()), ref.GetDocumentCode(),
		nullUUIDText(ref.GetCaseId()), nullText(req.GetIdempotencyKey()),
		metadataActor(req, ref.GetDocumentCode())); err != nil {
		return nil, fmt.Errorf("insert pending entry: %w", err)
	}

	var entryID string
	var entryNo, createdAt string
	if err := tx.QueryRowContext(ctx, `
		SELECT id, entry_no, created_at FROM fin_journal_entries
		WHERE tenant_id = $1 AND idempotency_key = $2 AND business_doc_type = $3
		ORDER BY entry_no DESC LIMIT 1`,
		tenantID, req.GetIdempotencyKey(), ref.GetDocumentType(),
	).Scan(&entryID, &entryNo, &createdAt); err != nil {
		return nil, fmt.Errorf("fetch pending entry: %w", err)
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

	if err := s.reserveLines(ctx, tx, tenantID, resolved, req.GetAccountingDate()); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &financev1.PostingResponse{
		JournalEntryId: entryID,
		EntryNo:        parseI64(entryNo),
		PostedAt:       createdAt,
		Status:         "PENDING",
		Replayed:       false,
	}, nil
}

// handleExistingForReserve resolves what an existing idempotency-key holder
// means for a new ReservePosting call: replay (returns handled=true), rebuild
// (releases the stale hold, handled=false → caller creates fresh), or an
// error. POSTED/REVERSED holders always replay.
func (s *PostingService) handleExistingForReserve(ctx context.Context, tenantID string, req *financev1.PostingRequest, previous *financev1.PostingResponse) (bool, error) {
	switch previous.GetStatus() {
	case "PENDING":
		stored, err := s.repo.ListEntryLines(ctx, tenantID, previous.GetJournalEntryId())
		if err != nil {
			return false, err
		}
		result, resolved, err := s.resolve(ctx, tenantID, req)
		if err != nil {
			return false, err
		}
		if !result.GetValid() {
			return false, fmt.Errorf("posting rejected: %s", strings.Join(result.GetGlobalErrors(), "; "))
		}
		if pendingLinesMatch(stored, resolved) {
			return true, nil
		}
		if _, err := s.releasePendingEntry(ctx, tenantID, previous.GetJournalEntryId(),
			"reserve rebuilt: proposal edited", "reserve-rebuild"); err != nil {
			return false, err
		}
		return false, nil
	case "VOID":
		// release freed the key — re-reserve fresh.
		return false, nil
	default:
		return true, nil
	}
}

// ReleasePosting frees the reserved amounts of a PENDING entry and stamps it
// VOID (the maker-checker reject/cancel path). Releasing an already-VOID
// entry replays; POSTED entries are immutable — reverse them instead.
func (s *PostingService) ReleasePosting(ctx context.Context, tenantID string, req *financev1.ReleaseRequest) (*financev1.PostingResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if req.GetJournalEntryId() == "" {
		return nil, fmt.Errorf("journal_entry_id is required")
	}
	return s.releasePendingEntry(ctx, tenantID, req.GetJournalEntryId(), req.GetReason(), req.GetActor())
}

// postPendingEntry converts a PENDING entry to POSTED inside one transaction:
// re-checks the period, re-validates actual balances (money may have really
// left via other posted paths while the entry sat pending), moves the holds
// to posted counters and fires the outbox event. Idempotent: an already
// POSTED entry replays; a VOID entry is a caller error.
func (s *PostingService) postPendingEntry(ctx context.Context, tenantID, entryID, actor string) (*financev1.PostingResponse, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var accountingDate, currency, status, voidReason string
	var entryNo int64
	var createdAt string
	err = tx.QueryRowContext(ctx, `
		SELECT entry_no, accounting_date::text, currency_code, status, COALESCE(void_reason,''), created_at::text
		FROM fin_journal_entries WHERE tenant_id = $1 AND id = $2 FOR UPDATE`,
		tenantID, entryID).Scan(&entryNo, &accountingDate, &currency, &status, &voidReason, &createdAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("journal entry not found")
	}
	if err != nil {
		return nil, err
	}
	switch status {
	case "POSTED":
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &financev1.PostingResponse{JournalEntryId: entryID, EntryNo: entryNo, PostedAt: createdAt, Status: "POSTED", Replayed: true}, nil
	case "VOID":
		return nil, fmt.Errorf("entry %s is VOID (%s) and cannot be posted", entryID, voidReason)
	case "PENDING":
	default:
		return nil, fmt.Errorf("entry status %s cannot be posted", status)
	}

	if err := s.repo.EnsurePeriodOpen(ctx, tenantID, accountingDate); err != nil {
		return nil, fmt.Errorf("PERIOD_CLOSED: %w", err)
	}

	lines, err := s.repo.ListEntryLines(ctx, tenantID, entryID)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("entry %s has no lines", entryID)
	}

	if err := s.moveLinesToPosted(ctx, tx, tenantID, lines, accountingDate); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_journal_entries SET status = 'POSTED', posted_at = now(), updated_by = $3, updated_at = now()
		WHERE tenant_id = $1 AND id = $2`, tenantID, entryID, nullText(actor)); err != nil {
		return nil, fmt.Errorf("mark entry posted: %w", err)
	}

	if err := s.repo.InsertOutbox(ctx, tx, tenantID, entryID, outboxPayloadFromEntry(entryID, accountingDate, currency, lines)); err != nil {
		return nil, fmt.Errorf("enqueue outbox: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &financev1.PostingResponse{
		JournalEntryId: entryID,
		EntryNo:        entryNo,
		PostedAt:       time.Now().UTC().Format(time.RFC3339),
		Status:         "POSTED",
		Replayed:       false,
	}, nil
}

// releasePendingEntry voids a PENDING entry: reserved counters are freed and
// the idempotency key is released so the document can re-reserve later.
func (s *PostingService) releasePendingEntry(ctx context.Context, tenantID, entryID, reason, actor string) (*financev1.PostingResponse, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var status, createdAt string
	var entryNo int64
	err = tx.QueryRowContext(ctx, `
		SELECT entry_no, status, created_at::text
		FROM fin_journal_entries WHERE tenant_id = $1 AND id = $2 FOR UPDATE`,
		tenantID, entryID).Scan(&entryNo, &status, &createdAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("journal entry not found")
	}
	if err != nil {
		return nil, err
	}
	switch status {
	case "VOID":
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &financev1.PostingResponse{JournalEntryId: entryID, EntryNo: entryNo, PostedAt: createdAt, Status: "VOID", Replayed: true}, nil
	case "PENDING":
	case "POSTED", "REVERSED":
		return nil, fmt.Errorf("posted entry %s cannot be released; reverse it instead", entryID)
	default:
		return nil, fmt.Errorf("entry status %s cannot be released", status)
	}

	lines, err := s.repo.ListEntryLines(ctx, tenantID, entryID)
	if err != nil {
		return nil, err
	}
	if len(lines) > 0 {
		keys := balanceKeysFromRows(lines)
		balances, err := s.repo.EnsureAndLockBalances(ctx, tx, tenantID, keys)
		if err != nil {
			return nil, err
		}
		byKey := balanceIndex(balances)
		for _, l := range lines {
			row := byKey[repository.BalanceKey{CoaVersion: l.CoaVersion, AccountCode: l.AccountCode, CurrencyCode: l.Currency}]
			applyRelease(row, l.Direction, l.AmountMinor)
		}
		for _, row := range byKey {
			if err := s.repo.SaveBalance(ctx, tx, tenantID, *row); err != nil {
				return nil, err
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_journal_entries
		SET status = 'VOID', void_reason = $3, idempotency_key = NULL,
		    updated_by = $4, updated_at = now()
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, entryID, nullText(reason), nullText(actor)); err != nil {
		return nil, fmt.Errorf("void entry: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &financev1.PostingResponse{
		JournalEntryId: entryID,
		EntryNo:        entryNo,
		PostedAt:       createdAt,
		Status:         "VOID",
		Replayed:       false,
	}, nil
}

// reserveLines locks the balance rows for the resolved lines and applies the
// availability reservation, checking every outflow line against the
// account's effective available value (opening + posted + reserved).
func (s *PostingService) reserveLines(ctx context.Context, tx *sql.Tx, tenantID string, lines []*financev1.ValidationLine, accountingDate string) error {
	keys := balanceKeysFromLines(lines)
	balances, err := s.repo.EnsureAndLockBalances(ctx, tx, tenantID, keys)
	if err != nil {
		return err
	}
	byKey := balanceIndex(balances)
	natures, err := s.repo.LoadAccountNatures(ctx, tenantID, keys)
	if err != nil {
		return err
	}
	openings, err := s.repo.LoadOpeningSides(ctx, tenantID, keys, accountingDate)
	if err != nil {
		return err
	}
	for _, l := range lines {
		k := repository.BalanceKey{CoaVersion: l.GetCoaVersion(), AccountCode: l.GetAccountCode(), CurrencyCode: l.GetCurrencyCode()}
		row := byKey[k]
		nature := natures[k]
		opening := naturalSignedOpening(openings[k], nature)
		if err := checkReserve(*row, opening, nature, l.GetDirection(), l.GetAmountMinor()); err != nil {
			return err
		}
		applyReserve(row, l.GetDirection(), l.GetAmountMinor())
	}
	for _, row := range byKey {
		if err := s.repo.SaveBalance(ctx, tx, tenantID, *row); err != nil {
			return err
		}
	}
	return nil
}

// moveLinesToPosted graduates the holds of a pending entry to posted
// counters, re-validating actual coverage per outflow line (belt-and-braces
// — the move itself never changes availability).
func (s *PostingService) moveLinesToPosted(ctx context.Context, tx *sql.Tx, tenantID string, lines []repository.JournalLineRow, accountingDate string) error {
	keys := balanceKeysFromRows(lines)
	balances, err := s.repo.EnsureAndLockBalances(ctx, tx, tenantID, keys)
	if err != nil {
		return err
	}
	byKey := balanceIndex(balances)
	natures, err := s.repo.LoadAccountNatures(ctx, tenantID, keys)
	if err != nil {
		return err
	}
	openings, err := s.repo.LoadOpeningSides(ctx, tenantID, keys, accountingDate)
	if err != nil {
		return err
	}
	for _, l := range lines {
		k := repository.BalanceKey{CoaVersion: l.CoaVersion, AccountCode: l.AccountCode, CurrencyCode: l.Currency}
		row := byKey[k]
		nature := natures[k]
		opening := naturalSignedOpening(openings[k], nature)
		if err := checkPostActual(*row, opening, nature, l.Direction, l.AmountMinor); err != nil {
			return err
		}
		applyPost(row, l.Direction, l.AmountMinor)
	}
	for _, row := range byKey {
		if err := s.repo.SaveBalance(ctx, tx, tenantID, *row); err != nil {
			return err
		}
	}
	return nil
}

// postLinesToBalances books direct-post lines straight to the posted
// counters — no availability check by design (system-executed decisions).
func (s *PostingService) postLinesToBalances(ctx context.Context, tx *sql.Tx, tenantID string, lines []*financev1.ValidationLine) error {
	keys := balanceKeysFromLines(lines)
	if len(keys) == 0 {
		return nil
	}
	balances, err := s.repo.EnsureAndLockBalances(ctx, tx, tenantID, keys)
	if err != nil {
		return err
	}
	byKey := balanceIndex(balances)
	for _, l := range lines {
		k := repository.BalanceKey{CoaVersion: l.GetCoaVersion(), AccountCode: l.GetAccountCode(), CurrencyCode: l.GetCurrencyCode()}
		applyDirectPost(byKey[k], l.GetDirection(), l.GetAmountMinor())
	}
	for _, row := range byKey {
		if err := s.repo.SaveBalance(ctx, tx, tenantID, *row); err != nil {
			return err
		}
	}
	return nil
}

func balanceKeysFromLines(lines []*financev1.ValidationLine) []repository.BalanceKey {
	seen := map[repository.BalanceKey]struct{}{}
	out := make([]repository.BalanceKey, 0, len(lines))
	for _, l := range lines {
		k := repository.BalanceKey{CoaVersion: l.GetCoaVersion(), AccountCode: l.GetAccountCode(), CurrencyCode: l.GetCurrencyCode()}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func balanceKeysFromRows(rows []repository.JournalLineRow) []repository.BalanceKey {
	seen := map[repository.BalanceKey]struct{}{}
	out := make([]repository.BalanceKey, 0, len(rows))
	for _, l := range rows {
		k := repository.BalanceKey{CoaVersion: l.CoaVersion, AccountCode: l.AccountCode, CurrencyCode: l.Currency}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func balanceIndex(rows []repository.BalanceRow) map[repository.BalanceKey]*repository.BalanceRow {
	out := make(map[repository.BalanceKey]*repository.BalanceRow, len(rows))
	for i := range rows {
		out[rows[i].Key] = &rows[i]
	}
	return out
}

// pendingLinesMatch compares a pending entry's stored lines with a freshly
// resolved request (line_no, direction, account, coa version, amount,
// currency) — equal content means the reserve replay is safe.
func pendingLinesMatch(stored []repository.JournalLineRow, resolved []*financev1.ValidationLine) bool {
	if len(stored) != len(resolved) {
		return false
	}
	for i, l := range stored {
		r := resolved[i]
		if l.LineNo != r.GetLineNo() || l.Direction != r.GetDirection() ||
			l.AccountCode != r.GetAccountCode() || l.CoaVersion != r.GetCoaVersion() ||
			l.AmountMinor != r.GetAmountMinor() || l.Currency != r.GetCurrencyCode() {
			return false
		}
	}
	return true
}

func metadataActor(req *financev1.PostingRequest, fallback string) string {
	if actor := req.GetMetadata()["actor"]; actor != "" {
		return actor
	}
	return fallback
}

func outboxPayloadFromEntry(entryID, accountingDate, currency string, lines []repository.JournalLineRow) []byte {
	debit, credit := int64(0), int64(0)
	for _, l := range lines {
		if l.Direction == "DEBIT" {
			debit += l.AmountMinor
		} else {
			credit += l.AmountMinor
		}
	}
	tmpl := `{"entry_id":%q,"accounting_date":%q,"currency_code":%q,"debit_minor":%d,"credit_minor":%d}`
	return fmtBytes(fmt.Sprintf(tmpl, entryID, accountingDate, currency, debit, credit))
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

	// Reversal lines post straight to the posted counters (nature-free — the
	// flipped directions net the original entry out of the balances).
	reversalKeys := balanceKeysFromRows(original.Lines)
	reversalBalances, err := s.repo.EnsureAndLockBalances(ctx, tx, tenantID, reversalKeys)
	if err != nil {
		return nil, err
	}
	reversalIndex := balanceIndex(reversalBalances)
	for _, l := range original.Lines {
		flipped := "CREDIT"
		if l.Direction == "CREDIT" {
			flipped = "DEBIT"
		}
		k := repository.BalanceKey{CoaVersion: l.CoaVersion, AccountCode: l.AccountCode, CurrencyCode: l.Currency}
		applyDirectPost(reversalIndex[k], flipped, l.AmountMinor)
	}
	for _, row := range reversalIndex {
		if err := s.repo.SaveBalance(ctx, tx, tenantID, *row); err != nil {
			return nil, err
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
		Status:         "POSTED",
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

// JournalListFilter is the paged journal-list contract for the HTTP read
// surface: q ILIKEs document type / document code / description, sort is a
// whitelist key (entry_no | accounting_date), paging is SQL LIMIT/OFFSET.
type JournalListFilter struct {
	FromDate     string
	ToDate       string
	DocumentType string
	Search       string
	Sort         string
	Order        string
	Page         int
	PerPage      int
}

// journalOrderClause maps the parsed sort onto a whitelisted ORDER BY. The
// ledger default stays newest-first (entry_no DESC); a whitelisted sort key
// with an explicit order overrides it.
func journalOrderClause(sort, order string) string {
	column := "entry_no"
	if sort == "accounting_date" {
		column = "accounting_date"
	}
	if sort != "" && order == "asc" {
		return column + " ASC"
	}
	return column + " DESC"
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

// ListJournalPaged is the paged journal-list contract: same narrowing as
// ListJournal plus q ILIKE (document type / document code / description),
// a whitelisted ORDER BY, SQL LIMIT/OFFSET and the unfiltered total.
func (s *PostingService) ListJournalPaged(ctx context.Context, tenantID string, f JournalListFilter) ([]JournalEntryRow, int, error) {
	page := f.Page
	if page < 1 {
		page = 1
	}
	perPage := f.PerPage
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}

	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if f.FromDate != "" {
		args = append(args, f.FromDate)
		where = append(where, fmt.Sprintf("accounting_date >= $%d::date", len(args)))
	}
	if f.ToDate != "" {
		args = append(args, f.ToDate)
		where = append(where, fmt.Sprintf("accounting_date <= $%d::date", len(args)))
	}
	if f.DocumentType != "" {
		args = append(args, f.DocumentType)
		where = append(where, fmt.Sprintf("business_doc_type = $%d", len(args)))
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		where = append(where, fmt.Sprintf(
			"(business_doc_type ILIKE $%d OR COALESCE(business_doc_code,'') ILIKE $%d OR COALESCE(description,'') ILIKE $%d)",
			len(args), len(args), len(args)))
	}

	wc := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM fin_journal_entries WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count journal entries: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, entry_no, accounting_date::text, currency_code, status,
		       COALESCE(description,''), business_domain, business_doc_type,
		       COALESCE(business_doc_code,''), COALESCE(case_id::text,''), created_at::text
		FROM fin_journal_entries
		WHERE %s
		ORDER BY %s
		LIMIT %d OFFSET %d`, wc, journalOrderClause(f.Sort, f.Order), perPage, (page-1)*perPage)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list journal entries: %w", err)
	}
	defer rows.Close()
	out := []JournalEntryRow{}
	for rows.Next() {
		var e JournalEntryRow
		if err := rows.Scan(&e.ID, &e.EntryNo, &e.AccountingDate, &e.CurrencyCode, &e.Status,
			&e.Description, &e.BusinessDomain, &e.DocumentType, &e.DocumentCode, &e.CaseID, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
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
