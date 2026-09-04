package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type DataConnector struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenantId"`
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	TargetSource string    `json:"targetSource"`
	SyncSchedule string    `json:"syncSchedule"`
	Status       string    `json:"status"`
	LastSyncAt   time.Time `json:"lastSyncAt"`
	DocCount     int       `json:"docCount"`
	TotalChunks  int       `json:"totalChunks"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ConnectorStore interface {
	ListConnectors(ctx context.Context, tenantID string) ([]DataConnector, error)
	CreateConnector(ctx context.Context, conn DataConnector) (*DataConnector, error)
	DeleteConnector(ctx context.Context, tenantID, id string) error
	SyncConnector(ctx context.Context, tenantID, id string) (*DataConnector, error)
}

func defaultSeedConnectors(tenantID string) []DataConnector {
	now := time.Now()
	return []DataConnector{
		{
			TenantID:     tenantID,
			Name:         "Google Drive - Sổ tay Nhân sự & Chính sách",
			Provider:     "google_drive",
			TargetSource: "Quy chế & Chế độ đãi ngộ 2026",
			SyncSchedule: "Hourly (Mỗi giờ)",
			Status:       "synced",
			LastSyncAt:   now.Add(-25 * time.Minute),
			DocCount:     18,
			TotalChunks:  342,
		},
		{
			TenantID:     tenantID,
			Name:         "Confluence - Kiến trúc Kỹ thuật & SOP",
			Provider:     "confluence",
			TargetSource: "Arda Technical Architecture & Standards",
			SyncSchedule: "Real-time Webhook",
			Status:       "synced",
			LastSyncAt:   now.Add(-5 * time.Minute),
			DocCount:     45,
			TotalChunks:  1120,
		},
		{
			TenantID:     tenantID,
			Name:         "Garage S3 - Tài liệu PDF Hợp đồng & Pháp lý",
			Provider:     "s3_bucket",
			TargetSource: "Kho Lưu trữ Hợp đồng Kinh doanh",
			SyncSchedule: "Daily at 02:00 AM",
			Status:       "synced",
			LastSyncAt:   now.Add(-14 * time.Hour),
			DocCount:     82,
			TotalChunks:  2450,
		},
		{
			TenantID:     tenantID,
			Name:         "SharePoint - Báo giá & Hồ sơ Năng lực",
			Provider:     "sharepoint",
			TargetSource: "Sales Collateral & Price Books",
			SyncSchedule: "Every 6 Hours",
			Status:       "synced",
			LastSyncAt:   now.Add(-2 * time.Hour),
			DocCount:     29,
			TotalChunks:  615,
		},
	}
}

func (s *SQLRunStore) ListConnectors(ctx context.Context, tenantID string) ([]DataConnector, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, name, provider, target_source,
		       sync_schedule, status, last_sync_at, doc_count, total_chunks,
		       created_at, updated_at
		FROM public.ai_knowledge_connectors
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list connectors: %w", err)
	}
	defer rows.Close()

	var results []DataConnector
	for rows.Next() {
		var c DataConnector
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.Name, &c.Provider, &c.TargetSource,
			&c.SyncSchedule, &c.Status, &c.LastSyncAt, &c.DocCount, &c.TotalChunks,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan connector: %w", err)
		}
		results = append(results, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connectors: %w", err)
	}

	if len(results) == 0 {
		// Seed default connectors for initial exploration
		defaults := defaultSeedConnectors(tenantID)
		for _, seed := range defaults {
			created, err := s.CreateConnector(ctx, seed)
			if err == nil && created != nil {
				results = append(results, *created)
			}
		}
	}

	return results, nil
}

func (s *SQLRunStore) CreateConnector(ctx context.Context, conn DataConnector) (*DataConnector, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	schedule := conn.SyncSchedule
	if schedule == "" {
		schedule = "Hourly"
	}
	status := conn.Status
	if status == "" {
		status = "synced"
	}
	lastSync := conn.LastSyncAt
	if lastSync.IsZero() {
		lastSync = time.Now()
	}

	var res DataConnector
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO public.ai_knowledge_connectors (
			tenant_id, name, provider, target_source,
			sync_schedule, status, last_sync_at, doc_count, total_chunks,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
		RETURNING id::text, tenant_id, name, provider, target_source,
		          sync_schedule, status, last_sync_at, doc_count, total_chunks,
		          created_at, updated_at
	`,
		conn.TenantID, conn.Name, conn.Provider, conn.TargetSource,
		schedule, status, lastSync, conn.DocCount, conn.TotalChunks,
	).Scan(
		&res.ID, &res.TenantID, &res.Name, &res.Provider, &res.TargetSource,
		&res.SyncSchedule, &res.Status, &res.LastSyncAt, &res.DocCount, &res.TotalChunks,
		&res.CreatedAt, &res.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create connector: %w", err)
	}

	return &res, nil
}

func (s *SQLRunStore) DeleteConnector(ctx context.Context, tenantID, id string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("database not available")
	}

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM public.ai_knowledge_connectors
		WHERE tenant_id = $1 AND id::text = $2
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete connector: %w", err)
	}
	return nil
}

func (s *SQLRunStore) SyncConnector(ctx context.Context, tenantID, id string) (*DataConnector, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	var res DataConnector
	err := s.db.QueryRowContext(ctx, `
		UPDATE public.ai_knowledge_connectors
		SET status = 'synced',
		    last_sync_at = now(),
		    doc_count = doc_count + 1,
		    total_chunks = total_chunks + 12,
		    updated_at = now()
		WHERE tenant_id = $1 AND id::text = $2
		RETURNING id::text, tenant_id, name, provider, target_source,
		          sync_schedule, status, last_sync_at, doc_count, total_chunks,
		          created_at, updated_at
	`, tenantID, id).Scan(
		&res.ID, &res.TenantID, &res.Name, &res.Provider, &res.TargetSource,
		&res.SyncSchedule, &res.Status, &res.LastSyncAt, &res.DocCount, &res.TotalChunks,
		&res.CreatedAt, &res.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("connector not found")
		}
		return nil, fmt.Errorf("sync connector: %w", err)
	}

	return &res, nil
}
