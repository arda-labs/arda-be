package service

import (
	"context"
	"database/sql"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
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

func (s *PlatformService) GetGlobalParameter(ctx context.Context, key string) (domain.Parameter, error) {
	return s.repo.GetGlobalParameter(ctx, key)
}

func (s *PlatformService) ListLookupCategories(ctx context.Context, tenantID, scopeType, scopeID string) ([]domain.LookupCategory, error) {
	return s.repo.ListLookupCategories(ctx, tenantID, scopeType, scopeID)
}

func (s *PlatformService) UpsertLookupCategory(ctx context.Context, item domain.LookupCategory) (domain.LookupCategory, error) {
	return s.repo.UpsertLookupCategory(ctx, item)
}

func (s *PlatformService) ListLookupValues(ctx context.Context, tenantID, categoryCode string) ([]domain.LookupValue, error) {
	return s.repo.ListLookupValues(ctx, tenantID, categoryCode)
}

func (s *PlatformService) UpsertLookupValue(ctx context.Context, tenantID, categoryCode string, item domain.LookupValue) (domain.LookupValue, error) {
	return s.repo.UpsertLookupValue(ctx, tenantID, categoryCode, item)
}

func (s *PlatformService) ListOrganizations(ctx context.Context, params repository.ListOrganizationsParams) ([]domain.Organization, int, error) {
	return s.repo.ListOrganizations(ctx, params)
}

func (s *PlatformService) CreateOrganization(ctx context.Context, item domain.Organization) (domain.Organization, error) {
	return s.repo.CreateOrganization(ctx, item)
}

func (s *PlatformService) ListGeoAdminUnits(ctx context.Context, parentCode string, level int) ([]domain.GeoAdminUnit, error) {
	return s.repo.ListGeoAdminUnits(ctx, parentCode, level)
}

// ListGeoAdminUnitsPaged is the SQL-paged list used by the admin wards catalog.
func (s *PlatformService) ListGeoAdminUnitsPaged(ctx context.Context, params repository.ListGeoAdminUnitsParams) ([]domain.GeoAdminUnit, int, error) {
	return s.repo.ListGeoAdminUnitsPaged(ctx, params)
}

func (s *PlatformService) UpsertGeoAdminUnit(ctx context.Context, item domain.GeoAdminUnit) (domain.GeoAdminUnit, error) {
	return s.repo.UpsertGeoAdminUnit(ctx, item)
}

func (s *PlatformService) GetOrganizationByID(ctx context.Context, tenantID, id string) (domain.Organization, error) {
	return s.repo.GetOrganizationByID(ctx, tenantID, id)
}

func (s *PlatformService) UpdateOrganization(ctx context.Context, item domain.Organization) (domain.Organization, error) {
	return s.repo.UpdateOrganization(ctx, item)
}

func (s *PlatformService) DeleteOrganization(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteOrganization(ctx, tenantID, id)
}

func (s *PlatformService) DeleteParameter(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteParameter(ctx, tenantID, id)
}

func (s *PlatformService) DeleteLookupCategory(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteLookupCategory(ctx, tenantID, id)
}

func (s *PlatformService) DeleteLookupValue(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteLookupValue(ctx, tenantID, id)
}

func (s *PlatformService) ListCreditInstitutions(ctx context.Context, tenantID, status, query string) ([]domain.CreditInstitution, error) {
	return s.repo.ListCreditInstitutions(ctx, tenantID, status, query)
}

// ListCreditInstitutionsPaged is the SQL-paged list used by the admin catalog.
func (s *PlatformService) ListCreditInstitutionsPaged(ctx context.Context, params repository.ListCreditInstitutionsParams) ([]domain.CreditInstitution, int, error) {
	return s.repo.ListCreditInstitutionsPaged(ctx, params)
}

func (s *PlatformService) GetCreditInstitutionByID(ctx context.Context, tenantID, id string) (domain.CreditInstitution, error) {
	return s.repo.GetCreditInstitutionByID(ctx, tenantID, id)
}

func (s *PlatformService) CreateCreditInstitution(ctx context.Context, item domain.CreditInstitution) (domain.CreditInstitution, error) {
	return s.repo.CreateCreditInstitution(ctx, item)
}

func (s *PlatformService) UpdateCreditInstitution(ctx context.Context, item domain.CreditInstitution) (domain.CreditInstitution, error) {
	return s.repo.UpdateCreditInstitution(ctx, item)
}

func (s *PlatformService) DeleteCreditInstitution(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteCreditInstitution(ctx, tenantID, id)
}

func (s *PlatformService) ListAreas(ctx context.Context, tenantID, status, areaTypeCode, parentID, query string) ([]domain.Area, error) {
	return s.repo.ListAreas(ctx, tenantID, status, areaTypeCode, parentID, query)
}

// ListAreasPaged is the SQL-paged list used by the admin catalog.
func (s *PlatformService) ListAreasPaged(ctx context.Context, params repository.ListAreasParams) ([]domain.Area, int, error) {
	return s.repo.ListAreasPaged(ctx, params)
}

func (s *PlatformService) GetAreaByID(ctx context.Context, tenantID, id string) (domain.Area, error) {
	return s.repo.GetAreaByID(ctx, tenantID, id)
}

func (s *PlatformService) CreateArea(ctx context.Context, item domain.Area) (domain.Area, error) {
	return s.repo.CreateArea(ctx, item)
}

func (s *PlatformService) UpdateArea(ctx context.Context, item domain.Area) (domain.Area, error) {
	return s.repo.UpdateArea(ctx, item)
}

func (s *PlatformService) DeleteArea(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteArea(ctx, tenantID, id)
}

func (s *PlatformService) ListFileTemplates(ctx context.Context, tenantID string) ([]domain.FileTemplate, error) {
	return s.repo.ListFileTemplates(ctx, tenantID)
}

func (s *PlatformService) GetFileTemplateByID(ctx context.Context, tenantID, id string) (domain.FileTemplate, error) {
	return s.repo.GetFileTemplateByID(ctx, tenantID, id)
}

func (s *PlatformService) CreateFileTemplate(ctx context.Context, item domain.FileTemplate) (domain.FileTemplate, error) {
	return s.repo.CreateFileTemplate(ctx, item)
}

func (s *PlatformService) UpdateFileTemplate(ctx context.Context, item domain.FileTemplate) (domain.FileTemplate, error) {
	return s.repo.UpdateFileTemplate(ctx, item)
}

func (s *PlatformService) DeleteFileTemplate(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteFileTemplate(ctx, tenantID, id)
}

// ListWorkingHours returns the weekly shifts (W6a).
func (s *PlatformService) ListWorkingHours(ctx context.Context, tenantID, orgCode string) ([]repository.WorkingHour, error) {
	return s.repo.ListWorkingHours(ctx, tenantID, orgCode)
}

// UpsertWorkingHour creates or updates one shift.
func (s *PlatformService) UpsertWorkingHour(ctx context.Context, tenantID, actor string, in *repository.WorkingHour) (*repository.WorkingHour, error) {
	if in.DayOfWeek < 1 || in.DayOfWeek > 7 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "day_of_week must be 1..7")
	}
	if in.StartTime == "" || in.EndTime == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "start_time and end_time are required")
	}
	in.TenantID = tenantID
	in.CreatedBy = actor
	created, err := s.repo.UpsertWorkingHour(ctx, in)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, err.Error())
	}
	return created, nil
}

// SetWorkingHourActive toggles one shift.
func (s *PlatformService) SetWorkingHourActive(ctx context.Context, tenantID, id string, active bool) error {
	if err := s.repo.SetWorkingHourActive(ctx, tenantID, id, active); err != nil {
		return ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	return nil
}
