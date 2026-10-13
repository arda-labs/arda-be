package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// MenuService serves the DB-driven navigation tree. The effective list is
// tenant-aware (global seeds overridden by tenant-owned rows); filtering by
// user permission happens client-side where the session lives.
// menuSectionPattern keeps section codes short machine keys; labels live in
// the frontend i18n catalogue.
var menuSectionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,49}$`)

type MenuService struct {
	repo *repository.MenuRepository
}

func NewMenuService(repo *repository.MenuRepository) *MenuService {
	return &MenuService{repo: repo}
}

func (s *MenuService) ListEffective(ctx context.Context, tenantID string) ([]domain.MenuItem, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeTenantScopeRequired, "tenant scope is required")
	}
	items, err := s.repo.ListEffective(ctx, tenantID)
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeInternal, "list effective menu failed", err)
	}
	return items, nil
}

func (s *MenuService) List(ctx context.Context, tenantID string) ([]domain.MenuItem, error) {
	items, err := s.repo.List(ctx, tenantID)
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeInternal, "list menu failed", err)
	}
	return items, nil
}

func (s *MenuService) Upsert(ctx context.Context, tenantID string, item domain.MenuItem) (domain.MenuItem, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return domain.MenuItem{}, ardaerrors.New(ardaerrors.CodeTenantScopeRequired, "tenant scope is required")
	}
	if strings.TrimSpace(item.Code) == "" {
		return domain.MenuItem{}, ardaerrors.New(ardaerrors.CodeRequired, "code is required")
	}
	if strings.TrimSpace(item.Title) == "" {
		return domain.MenuItem{}, ardaerrors.New(ardaerrors.CodeRequired, "title is required")
	}
	item.Section = strings.TrimSpace(item.Section)
	if item.Section != "" && !menuSectionPattern.MatchString(item.Section) {
		return domain.MenuItem{}, ardaerrors.New(ardaerrors.CodeInvalidInput, "section must be a lowercase code")
	}
	// Runtime writes are always tenant-scoped; global seeds change via
	// migration, never through the API.
	item.TenantID = &tenantID
	saved, err := s.repo.Upsert(ctx, item)
	if err != nil {
		return domain.MenuItem{}, ardaerrors.Wrap(ardaerrors.CodeInternal, "upsert menu failed", err)
	}
	return saved, nil
}

func (s *MenuService) Delete(ctx context.Context, tenantID, id string) error {
	if err := s.repo.Delete(ctx, strings.TrimSpace(tenantID), id); err != nil {
		return ardaerrors.Wrap(ardaerrors.CodeNotFound, "delete menu failed", err)
	}
	return nil
}
