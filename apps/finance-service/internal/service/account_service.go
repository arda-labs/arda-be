package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
)

// AccountService manages the account master (fin_accounts CRUD). Balances
// live on the journal (fin_journal_lines) — never materialized here.
type AccountService struct {
	repo *repository.AccountRepository
}

func NewAccountService(repo *repository.AccountRepository) *AccountService {
	return &AccountService{repo: repo}
}

func (s *AccountService) ListAccounts(ctx context.Context, tenantID string) ([]domain.Account, error) {
	return s.repo.List(ctx, tenantID)
}

func (s *AccountService) GetAccount(ctx context.Context, tenantID, id string) (*domain.Account, error) {
	return s.repo.GetByID(ctx, tenantID, id)
}

func (s *AccountService) GetAccountByCode(ctx context.Context, tenantID, code string) (*domain.Account, error) {
	return s.repo.GetByCode(ctx, tenantID, code)
}

func (s *AccountService) CreateAccount(ctx context.Context, acct *domain.Account) (*domain.Account, error) {
	if acct == nil {
		return nil, fmt.Errorf("account is required")
	}
	if strings.TrimSpace(acct.Code) == "" || strings.TrimSpace(acct.Name) == "" {
		return nil, fmt.Errorf("code and name are required")
	}
	return s.repo.Create(ctx, acct)
}
