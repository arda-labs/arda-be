package service

import (
	"context"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"strings"
)

var productTypes = map[string]bool{"TERM": true, "LIMIT": true}

func (s *LoanService) ListProducts(ctx context.Context, tenantID string, includeInactive bool, isActive, q, sort, order string) ([]domain.LoanProduct, error) {
	items, err := s.repo.ListProducts(ctx, tenantID, includeInactive, isActive, q, sort, order)
	return items, mapRepoError(err)
}

func (s *LoanService) UpsertProduct(ctx context.Context, tenantID, createdBy string, in *domain.LoanProduct) (*domain.LoanProduct, error) {
	if strings.TrimSpace(in.Code) == "" || !codePattern.MatchString(in.Code) {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "product_code is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "name is required")
	}
	if !productTypes[in.ProductType] {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "product_type must be TERM or LIMIT")
	}
	if in.ProductType == "" {
		in.ProductType = "TERM"
	}
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	if in.TermUnit == "" {
		in.TermUnit = "MONTH"
	}
	in.ID = repository.NewID("prd")
	in.TenantID = tenantID
	in.CreatedBy = createdBy
	return s.repo.UpsertProduct(ctx, in)
}
