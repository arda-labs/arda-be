package service

import (
	"context"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// CounterpartyService manages the partner master (TK đối tác, W4c-E).
type CounterpartyService struct {
	repo *repository.ConfigRepository
}

func NewCounterpartyService(repo *repository.ConfigRepository) *CounterpartyService {
	return &CounterpartyService{repo: repo}
}

// List returns partner rows.
func (s *CounterpartyService) List(ctx context.Context, tenantID, q, partyType string, includeInactive bool) ([]repository.Counterparty, error) {
	return s.repo.ListCounterparties(ctx, tenantID, q, partyType, includeInactive)
}

// Upsert creates or updates one partner.
func (s *CounterpartyService) Upsert(ctx context.Context, tenantID, actor string, in *repository.Counterparty) (*repository.Counterparty, error) {
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code and name are required")
	}
	in.TenantID = tenantID
	in.CreatedBy = actor
	created, err := s.repo.UpsertCounterparty(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// SetActive soft-deletes/restores one partner.
func (s *CounterpartyService) SetActive(ctx context.Context, tenantID, id string, active bool) error {
	if err := s.repo.SetCounterpartyActive(ctx, tenantID, id, active); err != nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	return nil
}

// UpdateByID updates one partner by id.
func (s *CounterpartyService) UpdateByID(ctx context.Context, tenantID, id, actor string, in *repository.Counterparty) (*repository.Counterparty, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "name is required")
	}
	updated, err := s.repo.UpdateCounterparty(ctx, tenantID, id, actor, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	return updated, nil
}

// ListAccounts returns the bank/GL accounts of one partner.
func (s *CounterpartyService) ListAccounts(ctx context.Context, tenantID, counterpartyID string) ([]repository.CounterpartyAccount, error) {
	return s.repo.ListCounterpartyAccounts(ctx, tenantID, counterpartyID)
}

// UpsertAccount creates or updates one partner account.
func (s *CounterpartyService) UpsertAccount(ctx context.Context, tenantID string, in *repository.CounterpartyAccount) (*repository.CounterpartyAccount, error) {
	if strings.TrimSpace(in.AccountNo) == "" || strings.TrimSpace(in.CounterpartyID) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "counterparty_id and account_no are required")
	}
	in.TenantID = tenantID
	created, err := s.repo.UpsertCounterpartyAccount(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}
