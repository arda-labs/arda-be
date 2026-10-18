package service

import (
	"context"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

// interestConfigStore is the read side of lnm_product_interest_config.
type interestConfigStore interface {
	ListInterestConfigVersions(ctx context.Context, tenantID, productCode, orgCode string) ([]domain.InterestConfigRecord, error)
}

// InterestConfigResolver resolves the interest configuration of a loan product
// for a calculation date. Failures are coded *domain.InterestError values (no
// version in force, unsupported or unknown codes) except a store failure, which
// is returned as itself so a database outage is never mistaken for "not
// configured".
type InterestConfigResolver struct {
	store interestConfigStore
}

func NewInterestConfigResolver(store interestConfigStore) *InterestConfigResolver {
	return &InterestConfigResolver{store: store}
}

// Resolve returns the configuration in force on asOf (ISO YYYY-MM-DD business
// date, never derived from the wall clock here) for the product and the org of
// the agreement ("" when it has none).
func (r *InterestConfigResolver) Resolve(ctx context.Context, tenantID, productCode, orgCode, asOf string) (domain.InterestConfig, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(productCode) == "" {
		return domain.InterestConfig{}, &domain.InterestError{Code: domain.ErrCodeProductConfigRequestInvalid, Message: "tenant and product code are required"}
	}
	if _, err := time.Parse("2006-01-02", asOf); err != nil {
		return domain.InterestConfig{}, &domain.InterestError{Code: domain.ErrCodeProductConfigRequestInvalid, Message: "calculation date must be an ISO YYYY-MM-DD business date"}
	}
	versions, err := r.store.ListInterestConfigVersions(ctx, tenantID, productCode, orgCode)
	if err != nil {
		return domain.InterestConfig{}, err
	}
	return domain.ResolveInterestConfig(versions, orgCode, asOf)
}
