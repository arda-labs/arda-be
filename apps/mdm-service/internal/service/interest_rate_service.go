package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
	"github.com/arda-labs/arda/apps/mdm-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

var rateTypes = map[string]bool{"central": true, "loan": true, "deposit": true}
var applyTypes = map[string]bool{"by_balance": true, "by_term": true, "negotiated": true}

type InterestRateService struct {
	repo *repository.InterestRateRepository
}

func NewInterestRateService(repo *repository.InterestRateRepository) *InterestRateService {
	return &InterestRateService{repo: repo}
}

func (s *InterestRateService) List(ctx context.Context, tenantID, q string, includeInactive bool) ([]domain.InterestRate, error) {
	return s.repo.List(ctx, tenantID, q, includeInactive)
}

func (s *InterestRateService) Get(ctx context.Context, tenantID, id string) (domain.InterestRate, error) {
	item, err := s.repo.Get(ctx, tenantID, id)
	return item, mapRepoError(err)
}

func (s *InterestRateService) Create(ctx context.Context, tenantID string, item domain.InterestRate) (domain.InterestRate, error) {
	if err := validateRate(&item); err != nil {
		return domain.InterestRate{}, err
	}
	item.ID = newID("rate")
	if tenantID != "" {
		item.TenantID = &tenantID
	}
	created, err := s.repo.Create(ctx, item)
	return created, mapRepoError(err)
}

func (s *InterestRateService) Update(ctx context.Context, tenantID, id string, item domain.InterestRate) (domain.InterestRate, error) {
	if err := validateRate(&item); err != nil {
		return domain.InterestRate{}, err
	}
	item.ID = id
	item.TenantID = &tenantID
	updated, err := s.repo.Update(ctx, item)
	return updated, mapRepoError(err)
}

func (s *InterestRateService) Delete(ctx context.Context, tenantID, id string) error {
	return mapRepoError(s.repo.Delete(ctx, tenantID, id))
}

func validateRate(item *domain.InterestRate) error {
	if !codePattern.MatchString(item.Code) {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "code must match [A-Za-z0-9][A-Za-z0-9_-]{0,31}")
	}
	if strings.TrimSpace(item.Name) == "" {
		return ardaerrors.New(ardaerrors.CodeRequired, "name is required")
	}
	if !rateTypes[item.RateType] {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "rate_type must be one of central|loan|deposit")
	}
	if !applyTypes[item.ApplyType] {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "apply_type must be one of by_balance|by_term|negotiated")
	}
	return nil
}

func (s *InterestRateService) ListTiers(ctx context.Context, tenantID, rateID string) ([]domain.InterestRateTier, error) {
	tiers, err := s.repo.ListTiers(ctx, tenantID, rateID)
	return tiers, mapRepoError(err)
}

func (s *InterestRateService) CreateTier(ctx context.Context, tenantID, rateID string, tier domain.InterestRateTier) (domain.InterestRateTier, error) {
	if err := validateTier(&tier); err != nil {
		return domain.InterestRateTier{}, err
	}
	tier.ID = newID("tier")
	created, err := s.repo.CreateTier(ctx, tenantID, rateID, tier)
	return created, mapRepoError(err)
}

func (s *InterestRateService) UpdateTier(ctx context.Context, tenantID, rateID, tierID string, tier domain.InterestRateTier) (domain.InterestRateTier, error) {
	if err := validateTier(&tier); err != nil {
		return domain.InterestRateTier{}, err
	}
	updated, err := s.repo.UpdateTier(ctx, tenantID, rateID, tierID, tier)
	return updated, mapRepoError(err)
}

func (s *InterestRateService) DeleteTier(ctx context.Context, tenantID, rateID, tierID string) error {
	return mapRepoError(s.repo.DeleteTier(ctx, tenantID, rateID, tierID))
}

func validateTier(tier *domain.InterestRateTier) error {
	if !datePattern.MatchString(tier.EffectiveFrom) {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "effective_from must be YYYY-MM-DD")
	}
	if tier.EffectiveTo != nil && !datePattern.MatchString(*tier.EffectiveTo) {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "effective_to must be YYYY-MM-DD")
	}
	if tier.EffectiveTo != nil && *tier.EffectiveTo <= tier.EffectiveFrom {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "effective_to must be after effective_from")
	}
	if tier.RateValue < 0 {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "rate_value must be >= 0")
	}
	if tier.AmountFrom != nil && tier.AmountTo != nil && *tier.AmountTo <= *tier.AmountFrom {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "amount_to must be greater than amount_from")
	}
	return nil
}
