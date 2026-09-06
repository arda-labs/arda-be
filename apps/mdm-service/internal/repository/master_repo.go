package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
)

// NewID generates a prefixed random-hex identifier (`<prefix>_<32hex>`).
func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure mdm id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

// CatalogRepository implements uniform CRUD for every simple master-data
// catalog. Table names come from the trusted service registry, never from
// request input, so they are interpolated rather than bound as parameters.
type CatalogRepository struct {
	db *sql.DB
}

// Sentinel errors mapped to HTTP statuses by the service layer.
var (
	ErrNotFound = errors.New("mdm: catalog item not found")
	ErrConflict = errors.New("mdm: catalog code conflict")
)

func NewCatalogRepository(db *sql.DB) *CatalogRepository {
	return &CatalogRepository{db: db}
}

const catalogColumns = `id, tenant_id, code, name, description, is_active, attributes, created_at, updated_at`

func scanCatalogItem(scanner interface{ Scan(...any) error }) (domain.CatalogItem, error) {
	var item domain.CatalogItem
	var attrs []byte
	err := scanner.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Description, &item.IsActive, &attrs, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return domain.CatalogItem{}, err
	}
	if len(attrs) > 0 && string(attrs) != "null" {
		item.Attributes = json.RawMessage(attrs)
	}
	return item, nil
}

func (r *CatalogRepository) List(ctx context.Context, table, tenantID string, includeInactive bool) ([]domain.CatalogItem, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM %s
		WHERE (tenant_id IS NULL OR tenant_id = $1)
		  AND ($2 OR is_active)
		ORDER BY code`, catalogColumns, table), tenantID, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.CatalogItem, 0)
	for rows.Next() {
		item, err := scanCatalogItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *CatalogRepository) Get(ctx context.Context, table, tenantID, id string) (domain.CatalogItem, error) {
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM %s
		WHERE id = $1 AND (tenant_id IS NULL OR tenant_id = $2)`, catalogColumns, table), id, tenantID)
	item, err := scanCatalogItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CatalogItem{}, fmt.Errorf("%w", ErrNotFound)
	}
	return item, err
}

func (r *CatalogRepository) Create(ctx context.Context, table string, item domain.CatalogItem) (domain.CatalogItem, error) {
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, tenant_id, code, name, description, is_active, attributes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (code, COALESCE(tenant_id, '')) DO NOTHING
		RETURNING %s`, table, catalogColumns),
		item.ID, item.TenantID, item.Code, item.Name, item.Description, item.IsActive, nullIfEmpty(item.Attributes))
	created, err := scanCatalogItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CatalogItem{}, fmt.Errorf("%w", ErrConflict)
	}
	return created, err
}

func (r *CatalogRepository) Update(ctx context.Context, table string, item domain.CatalogItem) (domain.CatalogItem, error) {
	row := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET code = $3, name = $4, description = $5, is_active = $6, attributes = $7, updated_at = now()
		WHERE id = $1 AND (tenant_id IS NULL OR tenant_id = $2)
		RETURNING %s`, table, catalogColumns),
		item.ID, item.TenantID, item.Code, item.Name, item.Description, item.IsActive, nullIfEmpty(item.Attributes))
	updated, err := scanCatalogItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CatalogItem{}, fmt.Errorf("%w", ErrNotFound)
	}
	return updated, err
}

func (r *CatalogRepository) Delete(ctx context.Context, table, tenantID, id string) error {
	res, err := r.db.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE id = $1 AND (tenant_id IS NULL OR tenant_id = $2)`, table), id, tenantID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}

func nullIfEmpty(attrs json.RawMessage) any {
	if len(attrs) == 0 || strings.TrimSpace(string(attrs)) == "" || string(attrs) == "null" {
		return nil
	}
	return []byte(attrs)
}
