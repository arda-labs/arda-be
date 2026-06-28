package service

import (
	"context"
	"database/sql"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
)

type PlatformService struct {
	repo *repository.PlatformRepository
}

type ScopeSelector struct {
	TenantID  string
	ScopeType string
	ScopeID   string
}

func NewPlatformService(repo *repository.PlatformRepository) *PlatformService {
	return &PlatformService{repo: repo}
}

func (s *PlatformService) ListParameters(ctx context.Context, tenantID, scopeType, scopeID string) ([]domain.Parameter, error) {
	return s.repo.ListParameters(ctx, tenantID, scopeType, scopeID)
}

func (s *PlatformService) UpsertParameter(ctx context.Context, item domain.Parameter) (domain.Parameter, error) {
	return s.repo.UpsertParameter(ctx, item)
}

func (s *PlatformService) ResolveParameter(ctx context.Context, tenantID, key string, scopes []ScopeSelector) (domain.Parameter, error) {
	for _, scope := range scopes {
		if scope.TenantID == "" {
			scope.TenantID = tenantID
		}
		item, err := s.repo.GetParameter(ctx, scope.TenantID, key, scope.ScopeType, scope.ScopeID)
		if err == nil {
			return item, nil
		}
		if err != sql.ErrNoRows {
			return domain.Parameter{}, err
		}
	}
	return s.repo.GetParameter(ctx, tenantID, key, domain.ScopeGlobal, "")
}

func (s *PlatformService) ListLookupCategories(ctx context.Context, tenantID, scopeType, scopeID string) ([]domain.LookupCategory, error) {
	return s.repo.ListLookupCategories(ctx, tenantID, scopeType, scopeID)
}

func (s *PlatformService) UpsertLookupCategory(ctx context.Context, item domain.LookupCategory) (domain.LookupCategory, error) {
	return s.repo.UpsertLookupCategory(ctx, item)
}

func (s *PlatformService) ListLookupValues(ctx context.Context, categoryCode string) ([]domain.LookupValue, error) {
	return s.repo.ListLookupValues(ctx, categoryCode)
}

func (s *PlatformService) UpsertLookupValue(ctx context.Context, categoryCode string, item domain.LookupValue) (domain.LookupValue, error) {
	return s.repo.UpsertLookupValue(ctx, categoryCode, item)
}

func (s *PlatformService) ListOrganizations(ctx context.Context, tenantID string) ([]domain.Organization, error) {
	return s.repo.ListOrganizations(ctx, tenantID)
}

func (s *PlatformService) CreateOrganization(ctx context.Context, item domain.Organization) (domain.Organization, error) {
	return s.repo.CreateOrganization(ctx, item)
}

func (s *PlatformService) ListGeoAdminUnits(ctx context.Context, parentCode string, level int) ([]domain.GeoAdminUnit, error) {
	return s.repo.ListGeoAdminUnits(ctx, parentCode, level)
}

func (s *PlatformService) UpsertGeoAdminUnit(ctx context.Context, item domain.GeoAdminUnit) (domain.GeoAdminUnit, error) {
	return s.repo.UpsertGeoAdminUnit(ctx, item)
}

func (s *PlatformService) GetOrganizationByID(ctx context.Context, id string) (domain.Organization, error) {
	return s.repo.GetOrganizationByID(ctx, id)
}

func (s *PlatformService) UpdateOrganization(ctx context.Context, item domain.Organization) (domain.Organization, error) {
	return s.repo.UpdateOrganization(ctx, item)
}

func (s *PlatformService) DeleteOrganization(ctx context.Context, id string) error {
	return s.repo.DeleteOrganization(ctx, id)
}

func (s *PlatformService) DeleteParameter(ctx context.Context, id string) error {
	return s.repo.DeleteParameter(ctx, id)
}

func (s *PlatformService) DeleteLookupCategory(ctx context.Context, id string) error {
	return s.repo.DeleteLookupCategory(ctx, id)
}

func (s *PlatformService) DeleteLookupValue(ctx context.Context, id string) error {
	return s.repo.DeleteLookupValue(ctx, id)
}

func (s *PlatformService) ListFileTemplates(ctx context.Context, tenantID string) ([]domain.FileTemplate, error) {
	return s.repo.ListFileTemplates(ctx, tenantID)
}

func (s *PlatformService) GetFileTemplateByID(ctx context.Context, id string) (domain.FileTemplate, error) {
	return s.repo.GetFileTemplateByID(ctx, id)
}

func (s *PlatformService) CreateFileTemplate(ctx context.Context, item domain.FileTemplate) (domain.FileTemplate, error) {
	return s.repo.CreateFileTemplate(ctx, item)
}

func (s *PlatformService) UpdateFileTemplate(ctx context.Context, item domain.FileTemplate) (domain.FileTemplate, error) {
	return s.repo.UpdateFileTemplate(ctx, item)
}

func (s *PlatformService) DeleteFileTemplate(ctx context.Context, id string) error {
	return s.repo.DeleteFileTemplate(ctx, id)
}
