package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

// Tenant-owned overrides win over the global default tree: a tenant row with
// the same code replaces the global entry in the effective menu.
const effectiveMenuSQL = `
	SELECT id, tenant_id, parent_id, code, title, path, icon, remote,
	       required_permission, sort_order, is_active, created_at, updated_at
	FROM (
		SELECT m.*, ROW_NUMBER() OVER (
			PARTITION BY code
			ORDER BY (tenant_id IS NOT NULL) DESC, created_at ASC
		) AS rn
		FROM plt_menus m
		WHERE (tenant_id IS NULL OR tenant_id = $1)
	) ranked
	WHERE rn = 1 AND is_active
	ORDER BY sort_order, code`

type MenuRepository struct {
	db *sql.DB
}

func NewMenuRepository(db *sql.DB) *MenuRepository {
	return &MenuRepository{db: db}
}

const menuColumns = `id, tenant_id, parent_id, code, title, path, icon, remote, required_permission, sort_order, is_active, created_at, updated_at`

type scanner interface {
	Scan(...any) error
}

func scanMenu(s scanner) (domain.MenuItem, error) {
	var item domain.MenuItem
	err := s.Scan(&item.ID, &item.TenantID, &item.ParentID, &item.Code, &item.Title, &item.Path,
		&item.Icon, &item.Remote, &item.RequiredPermission, &item.SortOrder, &item.IsActive,
		&item.CreatedAt, &item.UpdatedAt)
	return item, err
}

// ListEffective returns the active menu tree for a tenant: global defaults
// overridden by tenant-owned rows sharing the same code.
func (r *MenuRepository) ListEffective(ctx context.Context, tenantID string) ([]domain.MenuItem, error) {
	rows, err := r.db.QueryContext(ctx, effectiveMenuSQL, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.MenuItem, 0)
	for rows.Next() {
		item, err := scanMenu(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// List returns menu rows visible to the tenant: global default rows plus
// tenant-owned rows (overrides). The admin CRUD surface needs both to show
// the full tree and to allow overriding global entries by code.
func (r *MenuRepository) List(ctx context.Context, tenantID string) ([]domain.MenuItem, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s FROM plt_menus
		WHERE tenant_id IS NULL OR ($1 <> '' AND tenant_id = $1)
		ORDER BY sort_order, code`, menuColumns), tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.MenuItem, 0)
	for rows.Next() {
		item, err := scanMenu(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *MenuRepository) Upsert(ctx context.Context, item domain.MenuItem) (domain.MenuItem, error) {
	if strings.TrimSpace(item.Code) == "" || strings.TrimSpace(item.Title) == "" {
		return domain.MenuItem{}, errors.New("code and title are required")
	}
	if item.ID == "" {
		item.ID = NewID("menu")
	}
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		INSERT INTO plt_menus (id, tenant_id, parent_id, code, title, path, icon, remote, required_permission, sort_order, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (code, COALESCE(tenant_id, '')) DO UPDATE SET
			parent_id = EXCLUDED.parent_id, title = EXCLUDED.title, path = EXCLUDED.path,
			icon = EXCLUDED.icon, remote = EXCLUDED.remote,
			required_permission = EXCLUDED.required_permission, sort_order = EXCLUDED.sort_order,
			is_active = EXCLUDED.is_active, updated_at = now()
		RETURNING %s`, menuColumns),
		item.ID, item.TenantID, item.ParentID, item.Code, item.Title, item.Path, item.Icon,
		item.Remote, item.RequiredPermission, item.SortOrder, item.IsActive)
	return scanMenu(row)
}

func (r *MenuRepository) Delete(ctx context.Context, tenantID, id string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM plt_menus WHERE id = $1 AND tenant_id IS NOT NULL AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return errors.New("menu item not found or is a global seed")
	}
	return nil
}
