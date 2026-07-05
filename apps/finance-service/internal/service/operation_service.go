package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
)

const (
	incomingCaseType = "FINANCE_INCOMING_TRANSACTION"
	outgoingCaseType = "FINANCE_OUTGOING_TRANSACTION"
)

type FinanceOperationService struct {
	accountRepo *repository.AccountRepository
	txnRepo     *repository.TransactionRepository
	configRepo  *repository.ConfigRepository
}

type OperationCreateRequest struct {
	IdempotencyKey      string
	TxnType             string
	TxnDate             string
	Amount              string
	Currency            string
	Description         string
	SourceRef           string
	CounterpartyName    string
	CounterpartyAccount string
	Priority            string
	CreatedBy           string
}

func NewFinanceOperationService(accountRepo *repository.AccountRepository, txnRepo *repository.TransactionRepository, configRepo *repository.ConfigRepository) *FinanceOperationService {
	return &FinanceOperationService{accountRepo: accountRepo, txnRepo: txnRepo, configRepo: configRepo}
}

func (s *FinanceOperationService) CreateIncoming(ctx context.Context, tenantID string, req OperationCreateRequest) (*domain.Transaction, error) {
	return s.create(ctx, tenantID, domain.TxnDirectionIncoming, incomingCaseType, "Giao dich den", req)
}

func (s *FinanceOperationService) CreateOutgoing(ctx context.Context, tenantID string, req OperationCreateRequest) (*domain.Transaction, error) {
	return s.create(ctx, tenantID, domain.TxnDirectionOutgoing, outgoingCaseType, "Giao dich di", req)
}

func (s *FinanceOperationService) create(ctx context.Context, tenantID string, direction domain.TransactionDirection, caseType, operationName string, req OperationCreateRequest) (*domain.Transaction, error) {
	if req.IdempotencyKey == "" {
		return nil, fmt.Errorf("idempotency key required")
	}
	if req.TxnType == "" || req.Amount == "" {
		return nil, fmt.Errorf("txnType and amount required")
	}
	if tenantID == "" {
		tenantID = "default"
	}
	if req.Currency == "" {
		req.Currency = "VND"
	}
	if req.TxnDate == "" {
		req.TxnDate = time.Now().Format("2006-01-02")
	}
	if req.CreatedBy == "" {
		req.CreatedBy = "00000000-0000-0000-0000-000000000000"
	}

	existing, err := s.txnRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
	if err == nil && existing != nil {
		return existing, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	preview, err := s.journalPreview(ctx, tenantID, string(direction), req.TxnType, req.Amount, req.Currency, req.Description)
	if err != nil {
		return nil, err
	}

	txn := &domain.Transaction{
		TenantID:            tenantID,
		IdempotencyKey:      req.IdempotencyKey,
		TxnType:             req.TxnType,
		Direction:           direction,
		CaseType:            caseType,
		OperationName:       operationName,
		TxnDate:             req.TxnDate,
		Status:              domain.TxnPending,
		Amount:              req.Amount,
		Currency:            req.Currency,
		Description:         req.Description,
		SourceRef:           req.SourceRef,
		CounterpartyName:    req.CounterpartyName,
		CounterpartyAccount: req.CounterpartyAccount,
		CurrentStep:         "DRAFT",
		Priority:            req.Priority,
		CreatedBy:           req.CreatedBy,
		Metadata: map[string]any{
			"journalPreview": preview,
		},
	}
	if txn.Priority == "" {
		txn.Priority = "NORMAL"
	}

	if err := s.txnRepo.Create(ctx, txn); err != nil {
		return nil, err
	}
	return txn, nil
}

func (s *FinanceOperationService) journalPreview(ctx context.Context, tenantID, direction, txnType, amount, currency, description string) ([]domain.LedgerEntry, error) {
	def, err := s.configRepo.FindJournalDefinition(ctx, tenantID, direction, txnType)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("journal definition not found for %s %s", direction, txnType)
	}
	if err != nil {
		return nil, err
	}
	if len(def.Lines) == 0 {
		return nil, fmt.Errorf("journal definition %s has no lines", def.Code)
	}

	lineDescription := description
	if lineDescription == "" {
		lineDescription = def.Name
	}

	entries := make([]domain.LedgerEntry, 0, len(def.Lines))
	var debitTotal, creditTotal float64
	for _, line := range def.Lines {
		accountCode, err := s.resolveJournalAccountRef(ctx, tenantID, direction, txnType, line)
		if err != nil {
			return nil, err
		}
		account, err := s.accountRepo.GetByTenantCode(ctx, tenantID, accountCode)
		if err != nil {
			return nil, fmt.Errorf("journal line %d account %s not found: %w", line.LineSeq, accountCode, err)
		}
		if !account.IsActive {
			return nil, fmt.Errorf("journal line %d uses inactive account %s", line.LineSeq, accountCode)
		}
		lineAmount, err := resolveJournalAmount(line.AmountSource, amount)
		if err != nil {
			return nil, err
		}
		desc := lineDescription
		if line.DescriptionTemplate != "" {
			desc = line.DescriptionTemplate
		}
		entryType := domain.EntryType(strings.ToUpper(line.EntryType))
		if entryType != domain.EntryDebit && entryType != domain.EntryCredit {
			return nil, fmt.Errorf("journal line %d has invalid entry type %s", line.LineSeq, line.EntryType)
		}
		entries = append(entries, domain.LedgerEntry{
			AccountID:   account.ID,
			AccountCode: account.Code,
			EntryType:   entryType,
			Amount:      lineAmount,
			Currency:    currency,
			Description: desc,
		})
		if entryType == domain.EntryDebit {
			debitTotal += parseAmount(lineAmount)
		} else {
			creditTotal += parseAmount(lineAmount)
		}
	}
	if debitTotal != creditTotal {
		return nil, fmt.Errorf("journal preview is unbalanced: debit=%.6f credit=%.6f", debitTotal, creditTotal)
	}
	return entries, nil
}

func (s *FinanceOperationService) resolveJournalAccountRef(ctx context.Context, tenantID, direction, txnType string, line domain.JournalLine) (string, error) {
	ref := strings.TrimSpace(line.AccountRef)
	switch strings.ToUpper(line.AccountResolutionType) {
	case "", "FIXED_CODE":
		if ref == "" {
			return "", fmt.Errorf("journal line %d missing account_ref", line.LineSeq)
		}
		return ref, nil
	case "CLASSIFICATION":
		items, err := s.configRepo.ListAccountClassifications(ctx, tenantID)
		if err != nil {
			return "", err
		}
		for _, item := range items {
			if item.Code == ref && strings.EqualFold(item.Direction, direction) {
				if item.TxnType != "" && txnType != "" && !strings.EqualFold(item.TxnType, txnType) {
					continue
				}
				return item.AccountCode, nil
			}
		}
		return "", fmt.Errorf("classification %s not found for %s %s", ref, direction, txnType)
	case "REGULATORY":
		items, err := s.configRepo.ListRegulatoryAccounts(ctx, tenantID)
		if err != nil {
			return "", err
		}
		for _, item := range items {
			if item.Code == ref {
				return item.AccountCode, nil
			}
		}
		return "", fmt.Errorf("regulatory account %s not found", ref)
	case "INTERNAL":
		items, err := s.configRepo.ListInternalAccounts(ctx, tenantID)
		if err != nil {
			return "", err
		}
		for _, item := range items {
			if item.Code == ref {
				return item.AccountCode, nil
			}
		}
		return "", fmt.Errorf("internal account %s not found", ref)
	default:
		return "", fmt.Errorf("unsupported account resolution %s on line %d", line.AccountResolutionType, line.LineSeq)
	}
}

func resolveJournalAmount(source, txnAmount string) (string, error) {
	switch strings.ToUpper(source) {
	case "", "TRANSACTION_AMOUNT":
		if strings.TrimSpace(txnAmount) == "" {
			return "", fmt.Errorf("transaction amount required")
		}
		return strings.TrimSpace(txnAmount), nil
	default:
		return "", fmt.Errorf("unsupported amount source %s", source)
	}
}

func parseAmount(value string) float64 {
	var out float64
	_, _ = fmt.Sscan(value, &out)
	return out
}

func (s *FinanceOperationService) Search(ctx context.Context, f repository.TransactionSearchFilter) ([]domain.Transaction, int, error) {
	return s.txnRepo.Search(ctx, f)
}

func (s *FinanceOperationService) Get(ctx context.Context, id string) (*domain.Transaction, error) {
	return s.txnRepo.GetByID(ctx, id)
}

type AccountingConfigService struct {
	repo *repository.ConfigRepository
}

func NewAccountingConfigService(repo *repository.ConfigRepository) *AccountingConfigService {
	return &AccountingConfigService{repo: repo}
}

func (s *AccountingConfigService) ListProcessConfigs(ctx context.Context, tenantID string) ([]domain.ProcessConfig, error) {
	return s.repo.ListProcessConfigs(ctx, tenantID)
}

func (s *AccountingConfigService) ListAccountClassifications(ctx context.Context, tenantID string) ([]domain.AccountClassification, error) {
	return s.repo.ListAccountClassifications(ctx, tenantID)
}

func (s *AccountingConfigService) ListJournalDefinitions(ctx context.Context, tenantID string) ([]domain.JournalDefinition, error) {
	return s.repo.ListJournalDefinitions(ctx, tenantID)
}

func (s *AccountingConfigService) ListRegulatoryAccounts(ctx context.Context, tenantID string) ([]domain.NamedAccountMapping, error) {
	return s.repo.ListRegulatoryAccounts(ctx, tenantID)
}

func (s *AccountingConfigService) ListInternalAccounts(ctx context.Context, tenantID string) ([]domain.NamedAccountMapping, error) {
	return s.repo.ListInternalAccounts(ctx, tenantID)
}
