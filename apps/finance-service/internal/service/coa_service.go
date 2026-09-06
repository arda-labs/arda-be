package service

import (
	"context"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// CoaService is the thin validation layer over the COA definition repository.
type CoaService struct {
	repo CoaRepository
}

// CoaRepository is the persistence contract (keeps the service testable).
type CoaRepository interface {
	ListVersions(ctx context.Context, tenantID string) ([]domain.CoaVersion, error)
	UpsertVersion(ctx context.Context, v *domain.CoaVersion) (*domain.CoaVersion, error)
	ListAccounts(ctx context.Context, tenantID, versionCode string) ([]domain.CoaAccount, error)
	UpsertAccount(ctx context.Context, a *domain.CoaAccount) (*domain.CoaAccount, error)
	ListClassMaps(ctx context.Context, tenantID, classification, versionCode string) ([]domain.AccClassCoaMap, error)
	UpsertClassMap(ctx context.Context, m *domain.AccClassCoaMap) (*domain.AccClassCoaMap, error)
	ResolveClassification(ctx context.Context, tenantID, classification, versionCode, debtGroupCode, currencyCode, onDate string) (*domain.ResolvedCoaAccount, error)
	ListStructures(ctx context.Context, tenantID string) ([]domain.AccStructure, error)
	UpsertStructure(ctx context.Context, s *domain.AccStructure) (*domain.AccStructure, error)
}

func NewCoaService(repo CoaRepository) *CoaService {
	return &CoaService{repo: repo}
}

var coaTypes = map[string]bool{"ASSET": true, "LIABILITY": true, "EQUITY": true, "INCOME": true, "EXPENSE": true}
var coaNatures = map[string]bool{"DEBIT": true, "CREDIT": true}

func (s *CoaService) ListVersions(ctx context.Context, tenantID string) ([]domain.CoaVersion, error) {
	return s.repo.ListVersions(ctx, tenantID)
}

func (s *CoaService) UpsertVersion(ctx context.Context, tenantID string, v *domain.CoaVersion) (*domain.CoaVersion, error) {
	if strings.TrimSpace(v.Code) == "" || strings.TrimSpace(v.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code and name are required")
	}
	v.TenantID = tenantID
	if v.Scope == "" {
		v.Scope = "GENERAL"
	}
	if v.EffectiveDate == "" {
		v.EffectiveDate = "1970-01-01"
	}
	return s.repo.UpsertVersion(ctx, v)
}

func (s *CoaService) ListAccounts(ctx context.Context, tenantID, versionCode string) ([]domain.CoaAccount, error) {
	if strings.TrimSpace(versionCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "version query is required")
	}
	return s.repo.ListAccounts(ctx, tenantID, versionCode)
}

func (s *CoaService) UpsertAccount(ctx context.Context, tenantID string, a *domain.CoaAccount) (*domain.CoaAccount, error) {
	if strings.TrimSpace(a.VersionCode) == "" || strings.TrimSpace(a.AccCode) == "" || strings.TrimSpace(a.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "versionCode, accCode and name are required")
	}
	if !coaTypes[a.AccType] || !coaNatures[a.AccNature] {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "accType/accNature invalid")
	}
	a.TenantID = tenantID
	if a.EffectiveDate == "" {
		a.EffectiveDate = "1970-01-01"
	}
	return s.repo.UpsertAccount(ctx, a)
}

func (s *CoaService) ListClassMaps(ctx context.Context, tenantID, classification, versionCode string) ([]domain.AccClassCoaMap, error) {
	return s.repo.ListClassMaps(ctx, tenantID, classification, versionCode)
}

func (s *CoaService) UpsertClassMap(ctx context.Context, tenantID string, m *domain.AccClassCoaMap) (*domain.AccClassCoaMap, error) {
	if strings.TrimSpace(m.Classification) == "" || strings.TrimSpace(m.CoaVersion) == "" || strings.TrimSpace(m.CoaAccCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "classification, coaVersion and coaAccCode are required")
	}
	m.TenantID = tenantID
	if m.EffectiveDate == "" {
		m.EffectiveDate = "1970-01-01"
	}
	return s.repo.UpsertClassMap(ctx, m)
}

func (s *CoaService) ResolveClassification(ctx context.Context, tenantID, classification, versionCode, debtGroupCode, currencyCode, onDate string) (*domain.ResolvedCoaAccount, error) {
	if strings.TrimSpace(classification) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "classification is required")
	}
	resolved, err := s.repo.ResolveClassification(ctx, tenantID, classification, versionCode, debtGroupCode, currencyCode, onDate)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, err.Error())
	}
	return resolved, nil
}

func (s *CoaService) ListStructures(ctx context.Context, tenantID string) ([]domain.AccStructure, error) {
	return s.repo.ListStructures(ctx, tenantID)
}

func (s *CoaService) UpsertStructure(ctx context.Context, tenantID string, st *domain.AccStructure) (*domain.AccStructure, error) {
	if strings.TrimSpace(st.Code) == "" || strings.TrimSpace(st.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "code and name are required")
	}
	if !coaTypes[st.AccType] {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "accType invalid")
	}
	st.TenantID = tenantID
	return s.repo.UpsertStructure(ctx, st)
}
