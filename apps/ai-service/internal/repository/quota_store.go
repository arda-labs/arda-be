package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type DepartmentBudget struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenantId"`
	Department   string    `json:"department"`
	MonthlyLimit float64   `json:"monthlyLimit"`
	Spent        float64   `json:"spent"`
	RPMLimit     int       `json:"rpmLimit"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type QuotaSettings struct {
	TenantID   string `json:"tenantId"`
	WebhookURL string `json:"webhookUrl"`
}

type QuotaStore interface {
	ListDepartmentBudgets(ctx context.Context, tenantID string) ([]DepartmentBudget, error)
	SaveDepartmentBudgets(ctx context.Context, tenantID string, budgets []DepartmentBudget) error
	GetQuotaSettings(ctx context.Context, tenantID string) (*QuotaSettings, error)
	SaveQuotaSettings(ctx context.Context, settings QuotaSettings) error
}

func defaultBudgets(tenantID string) []DepartmentBudget {
	return []DepartmentBudget{
		{TenantID: tenantID, Department: "Tech & DevOps", MonthlyLimit: 300, Spent: 118.2, RPMLimit: 120},
		{TenantID: tenantID, Department: "Sales & Marketing", MonthlyLimit: 150, Spent: 42.5, RPMLimit: 60},
		{TenantID: tenantID, Department: "HR & Internal Ops", MonthlyLimit: 80, Spent: 15.4, RPMLimit: 30},
		{TenantID: tenantID, Department: "Finance & Accounting", MonthlyLimit: 100, Spent: 22.1, RPMLimit: 40},
	}
}

func (s *SQLRunStore) ListDepartmentBudgets(ctx context.Context, tenantID string) ([]DepartmentBudget, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, department, monthly_limit, spent, rpm_limit, created_at, updated_at
		FROM public.ai_department_budgets
		WHERE tenant_id = $1
		ORDER BY department ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list department budgets: %w", err)
	}
	defer rows.Close()

	var list []DepartmentBudget
	for rows.Next() {
		var b DepartmentBudget
		if err := rows.Scan(&b.ID, &b.TenantID, &b.Department, &b.MonthlyLimit, &b.Spent, &b.RPMLimit, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan department budget: %w", err)
		}
		list = append(list, b)
	}
	if len(list) == 0 {
		defs := defaultBudgets(tenantID)
		_ = s.SaveDepartmentBudgets(ctx, tenantID, defs)
		return defs, nil
	}
	return list, nil
}

func (s *SQLRunStore) SaveDepartmentBudgets(ctx context.Context, tenantID string, budgets []DepartmentBudget) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("database not available")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, b := range budgets {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO public.ai_department_budgets (
				tenant_id, department, monthly_limit, spent, rpm_limit, updated_at
			) VALUES ($1, $2, $3, $4, $5, now())
			ON CONFLICT (tenant_id, department) DO UPDATE SET
				monthly_limit = EXCLUDED.monthly_limit,
				spent = EXCLUDED.spent,
				rpm_limit = EXCLUDED.rpm_limit,
				updated_at = now()
		`, tenantID, b.Department, b.MonthlyLimit, b.Spent, b.RPMLimit)
		if err != nil {
			return fmt.Errorf("upsert department budget %s: %w", b.Department, err)
		}
	}

	return tx.Commit()
}

func (s *SQLRunStore) GetQuotaSettings(ctx context.Context, tenantID string) (*QuotaSettings, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	var set QuotaSettings
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, webhook_url
		FROM public.ai_tenant_quota_settings
		WHERE tenant_id = $1
	`, tenantID).Scan(&set.TenantID, &set.WebhookURL)
	if err == sql.ErrNoRows {
		return &QuotaSettings{
			TenantID:   tenantID,
			WebhookURL: "https://hooks.slack.com/services/T00/B00/XXXX",
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get quota settings: %w", err)
	}
	return &set, nil
}

func (s *SQLRunStore) SaveQuotaSettings(ctx context.Context, settings QuotaSettings) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("database not available")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO public.ai_tenant_quota_settings (tenant_id, webhook_url, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (tenant_id) DO UPDATE SET
			webhook_url = EXCLUDED.webhook_url,
			updated_at = now()
	`, settings.TenantID, settings.WebhookURL)
	if err != nil {
		return fmt.Errorf("save quota settings: %w", err)
	}
	return nil
}
