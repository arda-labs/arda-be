package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ImportTransaction is one staged QCMS import (fe_statistical #20).
type ImportTransaction struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	ImportTypeCode string          `json:"import_type_code"`
	PeriodCode     string          `json:"period_code"`
	Status         string          `json:"status"`
	RowCount       int             `json:"row_count"`
	Payload        json.RawMessage `json:"payload"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type ListImportTransactionsParams struct {
	TenantID       string
	ImportTypeCode string
	PeriodCode     string
	Status         string
}

func (r *StatisticalRepository) ListImportTransactions(ctx context.Context, p ListImportTransactionsParams) ([]ImportTransaction, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, import_type_code, period_code, status, row_count, payload,
		       created_by, created_at, updated_at
		FROM rpt_import_transactions
		WHERE tenant_id = $1
		  AND ($2 = '' OR import_type_code = $2)
		  AND ($3 = '' OR period_code = $3)
		  AND ($4 = '' OR status = $4)
		ORDER BY created_at DESC LIMIT 500`,
		p.TenantID, p.ImportTypeCode, p.PeriodCode, p.Status)
	if err != nil {
		return nil, fmt.Errorf("list import transactions: %w", err)
	}
	defer rows.Close()

	items := []ImportTransaction{}
	for rows.Next() {
		var it ImportTransaction
		var payload []byte
		if err := rows.Scan(&it.ID, &it.TenantID, &it.ImportTypeCode, &it.PeriodCode, &it.Status,
			&it.RowCount, &payload, &it.CreatedBy, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		it.Payload = json.RawMessage(payload)
		items = append(items, it)
	}
	return items, rows.Err()
}

func (r *StatisticalRepository) UpsertImportTransaction(ctx context.Context, in *ImportTransaction) (*ImportTransaction, error) {
	payload := in.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	var out ImportTransaction
	var raw []byte
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_import_transactions (
			tenant_id, import_type_code, period_code, status, row_count, payload, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id, import_type_code, period_code) DO UPDATE SET
			status = EXCLUDED.status, row_count = EXCLUDED.row_count, payload = EXCLUDED.payload,
			updated_by = EXCLUDED.created_by, updated_at = now(), version = rpt_import_transactions.version + 1
		RETURNING id, tenant_id, import_type_code, period_code, status, row_count, payload,
		          created_by, created_at, updated_at`,
		in.TenantID, in.ImportTypeCode, in.PeriodCode, in.Status, in.RowCount, string(payload), in.CreatedBy,
	).Scan(&out.ID, &out.TenantID, &out.ImportTypeCode, &out.PeriodCode, &out.Status,
		&out.RowCount, &raw, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert import transaction: %w", err)
	}
	out.Payload = json.RawMessage(raw)
	return &out, nil
}

func (r *StatisticalRepository) SetImportTransactionStatus(ctx context.Context, tenantID, id, status, actor string) (*ImportTransaction, error) {
	var out ImportTransaction
	var raw []byte
	err := r.db.QueryRowContext(ctx, `
		UPDATE rpt_import_transactions
		SET status = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, import_type_code, period_code, status, row_count, payload,
		          created_by, created_at, updated_at`, tenantID, id, status, actor).
		Scan(&out.ID, &out.TenantID, &out.ImportTypeCode, &out.PeriodCode, &out.Status,
			&out.RowCount, &raw, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("set import status: %w", err)
	}
	out.Payload = json.RawMessage(raw)
	return &out, nil
}
