package service

import (
	"context"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
)

// AccountingConfigService exposes the accounting configuration catalogs
// (process configs, classifications, journal definitions, named accounts).
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
