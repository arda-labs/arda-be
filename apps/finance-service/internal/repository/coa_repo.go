package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// CoaRepository implements the COA v2 definition layer (versions, chart tree,
// class→COA maps, account-number structures).
type CoaRepository struct {
	db *sql.DB
}

func NewCoaRepository(db *sql.DB) *CoaRepository {
	return &CoaRepository{db: db}
}

func requireTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return errors.New("tenant scope is required")
	}
	return nil
}

func (r *CoaRepository) ListVersions(ctx context.Context, tenantID string) ([]domain.CoaVersion, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, scope, parent_code, effective_date::text, is_default, is_active, created_at, updated_at
		FROM fin_coa_versions WHERE tenant_id = $1 ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.CoaVersion{}
	for rows.Next() {
		var v domain.CoaVersion
		var parent sql.NullString
		if err := rows.Scan(&v.ID, &v.TenantID, &v.Code, &v.Name, &v.Scope, &parent, &v.EffectiveDate, &v.IsDefault, &v.IsActive, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if parent.Valid {
			v.ParentCode = &parent.String
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (r *CoaRepository) UpsertVersion(ctx context.Context, v *domain.CoaVersion) (*domain.CoaVersion, error) {
	if err := requireTenant(v.TenantID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_coa_versions (tenant_id, code, name, scope, parent_code, effective_date, is_default, is_active)
		VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8)
		ON CONFLICT (tenant_id, code) DO UPDATE SET
			name = EXCLUDED.name, scope = EXCLUDED.scope, parent_code = EXCLUDED.parent_code,
			effective_date = EXCLUDED.effective_date, is_default = EXCLUDED.is_default,
			is_active = EXCLUDED.is_active, updated_at = now()
		RETURNING id, tenant_id, code, name, scope, parent_code, effective_date::text, is_default, is_active, created_at, updated_at`,
		v.TenantID, v.Code, v.Name, v.Scope, v.ParentCode, v.EffectiveDate, v.IsDefault, v.IsActive)
	out := &domain.CoaVersion{}
	var parent sql.NullString
	if err := row.Scan(&out.ID, &out.TenantID, &out.Code, &out.Name, &out.Scope, &parent, &out.EffectiveDate, &out.IsDefault, &out.IsActive, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nil, err
	}
	if parent.Valid {
		out.ParentCode = &parent.String
	}
	return out, nil
}

func (r *CoaRepository) ListAccounts(ctx context.Context, tenantID, versionCode, nature string) ([]domain.CoaAccount, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	// Empty version resolves to the tenant's active default (same semantics
	// as posting ResolveAccountDirect) — the off-balance picker calls
	// nature=B without pinning a version.
	if versionCode == "" {
		err := r.db.QueryRowContext(ctx, `
			SELECT code FROM fin_coa_versions
			WHERE tenant_id = $1 AND is_active
			ORDER BY is_default DESC, effective_date DESC
			LIMIT 1`, tenantID).Scan(&versionCode)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no active COA version for tenant")
		}
		if err != nil {
			return nil, err
		}
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, version_code, acc_code, name, acc_type, acc_nature, parent_code,
		       is_internal, is_postable, effective_date::text, expiry_date::text, description, created_at, updated_at
		FROM fin_coa_accounts
		WHERE tenant_id = $1 AND version_code = $2
		  AND ($3 = '' OR acc_nature = $3)
		ORDER BY acc_code`, tenantID, versionCode, nature)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.CoaAccount{}
	for rows.Next() {
		item, err := scanCoaAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanCoaAccount(s interface{ Scan(...any) error }) (domain.CoaAccount, error) {
	var a domain.CoaAccount
	var parent, expiry, desc sql.NullString
	err := s.Scan(&a.ID, &a.TenantID, &a.VersionCode, &a.AccCode, &a.Name, &a.AccType, &a.AccNature, &parent,
		&a.IsInternal, &a.IsPostable, &a.EffectiveDate, &expiry, &desc, &a.CreatedAt, &a.UpdatedAt)
	if parent.Valid {
		a.ParentCode = &parent.String
	}
	if expiry.Valid {
		a.ExpiryDate = &expiry.String
	}
	if desc.Valid {
		a.Description = &desc.String
	}
	return a, err
}

func (r *CoaRepository) UpsertAccount(ctx context.Context, a *domain.CoaAccount) (*domain.CoaAccount, error) {
	if err := requireTenant(a.TenantID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature, parent_code,
			is_internal, is_postable, effective_date, expiry_date, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, $11::date, $12)
		ON CONFLICT (tenant_id, version_code, acc_code) DO UPDATE SET
			name = EXCLUDED.name, acc_type = EXCLUDED.acc_type, acc_nature = EXCLUDED.acc_nature,
			parent_code = EXCLUDED.parent_code, is_internal = EXCLUDED.is_internal,
			is_postable = EXCLUDED.is_postable, effective_date = EXCLUDED.effective_date,
			expiry_date = EXCLUDED.expiry_date, description = EXCLUDED.description, updated_at = now()
		RETURNING id, tenant_id, version_code, acc_code, name, acc_type, acc_nature, parent_code,
			is_internal, is_postable, effective_date::text, expiry_date::text, description, created_at, updated_at`,
		a.TenantID, a.VersionCode, a.AccCode, a.Name, a.AccType, a.AccNature, a.ParentCode,
		a.IsInternal, a.IsPostable, a.EffectiveDate, a.ExpiryDate, a.Description)
	out, err := scanCoaAccount(row)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *CoaRepository) ListClassMaps(ctx context.Context, tenantID, classification, versionCode string) ([]domain.AccClassCoaMap, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code,
		       effective_date::text, expiry_date::text, created_at, updated_at
		FROM fin_acc_class_coa_maps
		WHERE tenant_id = $1
		  AND ($2 = '' OR classification = $2)
		  AND ($3 = '' OR coa_version = $3)
		ORDER BY classification, coa_version, coa_acc_code`, tenantID, classification, versionCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.AccClassCoaMap{}
	for rows.Next() {
		var m domain.AccClassCoaMap
		var expiry sql.NullString
		if err := rows.Scan(&m.ID, &m.TenantID, &m.Classification, &m.CoaVersion, &m.CoaAccCode, &m.DebtGroupCode, &m.CurrencyCode,
			&m.EffectiveDate, &expiry, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		if expiry.Valid {
			m.ExpiryDate = &expiry.String
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (r *CoaRepository) UpsertClassMap(ctx context.Context, m *domain.AccClassCoaMap) (*domain.AccClassCoaMap, error) {
	if err := requireTenant(m.TenantID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code, effective_date, expiry_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8::date)
		ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO UPDATE SET
			effective_date = EXCLUDED.effective_date, expiry_date = EXCLUDED.expiry_date, updated_at = now()
		RETURNING id, tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code,
		          effective_date::text, expiry_date::text, created_at, updated_at`,
		m.TenantID, m.Classification, m.CoaVersion, m.CoaAccCode, m.DebtGroupCode, m.CurrencyCode, m.EffectiveDate, m.ExpiryDate)
	out := &domain.AccClassCoaMap{}
	var expiry sql.NullString
	if err := row.Scan(&out.ID, &out.TenantID, &out.Classification, &out.CoaVersion, &out.CoaAccCode, &out.DebtGroupCode, &out.CurrencyCode,
		&out.EffectiveDate, &expiry, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nil, err
	}
	if expiry.Valid {
		out.ExpiryDate = &expiry.String
	}
	return out, nil
}

// ResolveClassification picks the effective class→COA map row for a business
// event: exact debt-group/currency match wins, then blank-value rows.
func (r *CoaRepository) ResolveClassification(ctx context.Context, tenantID, classification, versionCode, debtGroupCode, currencyCode, onDate string) (*domain.ResolvedCoaAccount, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if onDate == "" {
		onDate = ardatime.Today()
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT m.classification, m.coa_version, m.coa_acc_code, a.acc_type, a.acc_nature
		FROM fin_acc_class_coa_maps m
		JOIN fin_coa_accounts a
		  ON a.tenant_id = m.tenant_id AND a.version_code = m.coa_version AND a.acc_code = m.coa_acc_code
		WHERE m.tenant_id = $1
		  AND m.classification = $2
		  AND ($3 = '' OR m.coa_version = $3)
		  AND m.effective_date <= $4::date
		  AND (m.expiry_date IS NULL OR m.expiry_date > $4::date)
		  AND m.debt_group_code IN ($5, '')
		  AND m.currency_code IN ($6, '')
		ORDER BY (m.debt_group_code <> '') DESC, (m.currency_code <> '') DESC
		LIMIT 1`,
		tenantID, classification, versionCode, onDate, debtGroupCode, currencyCode)
	if row == nil {
		return nil, fmt.Errorf("no class map found")
	}
	out := &domain.ResolvedCoaAccount{Classification: classification}
	err := row.Scan(&out.Classification, &out.CoaVersion, &out.CoaAccCode, &out.AccType, &out.AccNature)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("no effective COA mapping for classification %q", classification)
	}
	return out, err
}

func (r *CoaRepository) ListStructures(ctx context.Context, tenantID string) ([]domain.AccStructure, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	structRows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, acc_type, total_length, is_active, created_at, updated_at
		FROM fin_acc_structures WHERE tenant_id = $1 ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer structRows.Close()
	structs := []domain.AccStructure{}
	ids := []string{}
	for structRows.Next() {
		var s domain.AccStructure
		if err := structRows.Scan(&s.ID, &s.TenantID, &s.Code, &s.Name, &s.AccType, &s.TotalLength, &s.IsActive, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		structs = append(structs, s)
		ids = append(ids, s.ID)
	}
	if err := structRows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return structs, nil
	}
	segRows, err := r.db.QueryContext(ctx, `
		SELECT id, structure_id, seq_no, name, length, source, fixed_value, is_required
		FROM fin_acc_structure_segments
		WHERE tenant_id = $1 AND structure_id = ANY($2)
		ORDER BY structure_id, seq_no`, tenantID, ids)
	if err != nil {
		return nil, err
	}
	defer segRows.Close()
	segments := map[string][]domain.AccStructureSegment{}
	for segRows.Next() {
		var seg domain.AccStructureSegment
		if err := segRows.Scan(&seg.ID, &seg.StructureID, &seg.SeqNo, &seg.Name, &seg.Length, &seg.Source, &seg.FixedValue, &seg.IsRequired); err != nil {
			return nil, err
		}
		segments[seg.StructureID] = append(segments[seg.StructureID], seg)
	}
	for i := range structs {
		structs[i].Segments = segments[structs[i].ID]
	}
	return structs, nil
}

func (r *CoaRepository) UpsertStructure(ctx context.Context, s *domain.AccStructure) (*domain.AccStructure, error) {
	if err := requireTenant(s.TenantID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_acc_structures (tenant_id, code, name, acc_type, total_length, is_active)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, code) DO UPDATE SET
			name = EXCLUDED.name, acc_type = EXCLUDED.acc_type, total_length = EXCLUDED.total_length,
			is_active = EXCLUDED.is_active, updated_at = now()
		RETURNING id, tenant_id, code, name, acc_type, total_length, is_active, created_at, updated_at`,
		s.TenantID, s.Code, s.Name, s.AccType, s.TotalLength, s.IsActive)
	out := &domain.AccStructure{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.Code, &out.Name, &out.AccType, &out.TotalLength, &out.IsActive, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nil, err
	}
	// Segments are replace-all per structure (mirrors EPAS structure detail).
	if _, err := r.db.ExecContext(ctx, `DELETE FROM fin_acc_structure_segments WHERE tenant_id = $1 AND structure_id = $2`, s.TenantID, out.ID); err != nil {
		return nil, err
	}
	for i := range s.Segments {
		seg := s.Segments[i]
		seg.StructureID = out.ID
		if seg.SeqNo == 0 {
			seg.SeqNo = i + 1
		}
		err := r.db.QueryRowContext(ctx, `
			INSERT INTO fin_acc_structure_segments (tenant_id, structure_id, seq_no, name, length, source, fixed_value, is_required)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id`,
			s.TenantID, seg.StructureID, seg.SeqNo, seg.Name, seg.Length, seg.Source, seg.FixedValue, seg.IsRequired).Scan(&seg.ID)
		if err != nil {
			return nil, err
		}
		out.Segments = append(out.Segments, seg)
	}
	return out, nil
}
