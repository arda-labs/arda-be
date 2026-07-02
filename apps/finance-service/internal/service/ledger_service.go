package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
)

const defaultCurrencyParameter = "finance.default_currency"

type ParameterResolver interface {
	ResolveString(ctx context.Context, tenantID, key string) (string, error)
}

// LedgerService implements double-entry accounting.
type LedgerService struct {
	accountRepo *repository.AccountRepository
	txnRepo     *repository.TransactionRepository
	params      ParameterResolver
	logger      *slog.Logger
}

// NewLedgerService creates a ledger service.
func NewLedgerService(accountRepo *repository.AccountRepository, txnRepo *repository.TransactionRepository) *LedgerService {
	return &LedgerService{
		accountRepo: accountRepo,
		txnRepo:     txnRepo,
		logger:      slog.Default(),
	}
}

func (s *LedgerService) WithParameterResolver(params ParameterResolver) *LedgerService {
	s.params = params
	return s
}

// PostTransaction validates and posts a double-entry transaction.
func (s *LedgerService) PostTransaction(ctx context.Context, txn *domain.Transaction) (*domain.Transaction, error) {
	var totalDebit, totalCredit float64
	for _, e := range txn.Entries {
		amt := parseDecimal(e.Amount)
		switch e.EntryType {
		case domain.EntryDebit:
			totalDebit += amt
		case domain.EntryCredit:
			totalCredit += amt
		}
	}
	if totalDebit != totalCredit {
		return nil, fmt.Errorf("debit (%.2f) != credit (%.2f): entries must balance", totalDebit, totalCredit)
	}

	for i, e := range txn.Entries {
		acct, err := s.accountRepo.GetByID(ctx, e.AccountID)
		if err != nil || acct == nil {
			return nil, fmt.Errorf("entry %d: account %s not found", i, e.AccountID)
		}
		if !acct.IsActive {
			return nil, fmt.Errorf("entry %d: account %s is inactive", i, e.AccountID)
		}
		cur := e.Currency
		if cur == "" {
			cur = acct.Currency
		}
		if cur == "" {
			cur = "VND"
		}
		txn.Entries[i].Currency = cur
	}

	if txn.IdempotencyKey != "" {
		existing, err := s.txnRepo.GetByIdempotencyKey(ctx, txn.IdempotencyKey)
		if err == nil && existing != nil {
			s.logger.Info("idempotent request", "key", txn.IdempotencyKey, "existing_id", existing.ID)
			entries, _ := s.txnRepo.GetEntriesByTransaction(ctx, existing.ID)
			existing.Entries = entries
			return existing, nil
		}
	}

	entryID := newEntryID()
	defaultCurrency := s.resolveDefaultCurrency(ctx, txn.TenantID)
	for i := range txn.Entries {
		txn.Entries[i].EntryID = entryID
		if txn.Entries[i].Currency == "" {
			txn.Entries[i].Currency = defaultCurrency
		}
	}
	if txn.Status == "" {
		txn.Status = domain.TxnPosted
	}
	if txn.TxnDate == "" {
		txn.TxnDate = time.Now().Format("2006-01-02")
	}
	if txn.TenantID == "" {
		txn.TenantID = "default"
	}
	if txn.Currency == "" {
		txn.Currency = defaultCurrency
	}
	if txn.Amount == "" {
		txn.Amount = fmt.Sprintf("%.6f", totalDebit)
	}
	if txn.CreatedBy == "" {
		txn.CreatedBy = "00000000-0000-0000-0000-000000000000"
	}

	if err := s.txnRepo.Create(ctx, txn); err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}

	for i := range txn.Entries {
		txn.Entries[i].TransactionID = txn.ID
	}
	if err := s.txnRepo.InsertLedgerEntries(ctx, txn.Entries); err != nil {
		return nil, fmt.Errorf("insert ledger entries: %w", err)
	}

	for _, e := range txn.Entries {
		acct, err := s.accountRepo.GetByID(ctx, e.AccountID)
		if err == nil && acct != nil {
			if err := s.txnRepo.UpdateBalance(ctx, e.AccountID, e.Amount, acct.NormalBalance, e.EntryType); err != nil {
				s.logger.Warn("balance update failed", "account", e.AccountID, "err", err)
			}
		}
	}

	s.logger.Info("transaction posted",
		"id", txn.ID, "type", txn.TxnType,
		"entries", len(txn.Entries),
		"amount", totalDebit,
	)

	return txn, nil
}

// ReverseTransaction reverses a posted transaction.
func (s *LedgerService) ReverseTransaction(ctx context.Context, originalID, reason, userID string) (*domain.Transaction, error) {
	original, err := s.txnRepo.GetByID(ctx, originalID)
	if err != nil {
		return nil, fmt.Errorf("original transaction not found: %w", err)
	}

	entries, err := s.txnRepo.GetEntriesByTransaction(ctx, originalID)
	if err != nil {
		return nil, err
	}

	reversalEntries := make([]domain.LedgerEntry, len(entries))
	for i, e := range entries {
		reversalType := domain.EntryCredit
		if e.EntryType == domain.EntryCredit {
			reversalType = domain.EntryDebit
		}
		reversalEntries[i] = domain.LedgerEntry{
			AccountID:   e.AccountID,
			EntryType:   reversalType,
			Amount:      e.Amount,
			Currency:    e.Currency,
			Description: fmt.Sprintf("Reversal: %s", reason),
		}
	}

	rev := &domain.Transaction{
		TenantID:    original.TenantID,
		TxnType:     original.TxnType + "_REVERSAL",
		TxnDate:     time.Now().Format("2006-01-02"),
		Status:      domain.TxnPosted,
		Description: fmt.Sprintf("Reversal of %s: %s", originalID, reason),
		SourceRef:   originalID,
		CreatedBy:   userID,
		Metadata:    map[string]any{"reversal_reason": reason},
		Entries:     reversalEntries,
	}

	result, err := s.PostTransaction(ctx, rev)
	if err != nil {
		return nil, err
	}

	s.txnRepo.UpdateStatus(ctx, originalID, domain.TxnReversed, userID)
	return result, nil
}

// ── Account helpers ──

func (s *LedgerService) CreateAccount(ctx context.Context, a *domain.Account) (*domain.Account, error) {
	return s.accountRepo.Create(ctx, a)
}

func (s *LedgerService) GetAccount(ctx context.Context, id string) (*domain.Account, error) {
	return s.accountRepo.GetByID(ctx, id)
}

func (s *LedgerService) GetAccountBalance(ctx context.Context, id string) (*domain.AccountBalance, error) {
	return s.accountRepo.GetBalance(ctx, id)
}

func (s *LedgerService) ListAccounts(ctx context.Context, tenantID string) ([]domain.Account, error) {
	return s.accountRepo.List(ctx, tenantID)
}

func (s *LedgerService) GetTransaction(ctx context.Context, id string) (*domain.Transaction, error) {
	txn, err := s.txnRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	entries, err := s.txnRepo.GetEntriesByTransaction(ctx, id)
	if err == nil {
		txn.Entries = entries
	}
	return txn, nil
}

func (s *LedgerService) ListTransactions(ctx context.Context, tenantID, status string, from, to time.Time, page, size int) ([]domain.Transaction, int, error) {
	return s.txnRepo.List(ctx, tenantID, status, from, to, page, size)
}

// ── Helpers ──

func (s *LedgerService) resolveDefaultCurrency(ctx context.Context, tenantID string) string {
	if s.params == nil {
		return "VND"
	}
	value, err := s.params.ResolveString(ctx, tenantID, defaultCurrencyParameter)
	if err != nil {
		s.logger.Debug("default currency parameter unavailable", "tenant_id", tenantID, "err", err)
		return "VND"
	}
	if value == "" {
		return "VND"
	}
	return value
}

func parseDecimal(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func newEntryID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

var _ = fmt.Sprintf
