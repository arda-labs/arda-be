package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

var ErrQuotaExceeded = fmt.Errorf("AI quota exceeded")

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
	TenantID          string `json:"tenantId"`
	WebhookURL        string `json:"webhookUrl"`
	MonthlyTokenLimit int64  `json:"monthlyTokenLimit"`
	TokensUsed        int64  `json:"tokensUsed"`
	PeriodStart       string `json:"periodStart"`
}

type QuotaStore interface {
	ListDepartmentBudgets(ctx context.Context, tenantID string) ([]DepartmentBudget, error)
	SaveDepartmentBudgets(ctx context.Context, tenantID string, budgets []DepartmentBudget) error
	GetQuotaSettings(ctx context.Context, tenantID string) (*QuotaSettings, error)
	SaveQuotaSettings(ctx context.Context, settings QuotaSettings) error
}

// QuotaGate reserves an estimated token allowance before a model run. The
// reservation is keyed by external run id, so retries/replays cannot consume
// the same allowance twice.
type QuotaGate interface {
	ReserveQuota(ctx context.Context, tenantID, externalRunID string, estimatedTokens int64) error
	FinalizeQuota(ctx context.Context, tenantID, externalRunID string, actualTokens int64) error
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
		return []DepartmentBudget{}, nil
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
				tenant_id, department, monthly_limit, rpm_limit, updated_at
			) VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (tenant_id, department) DO UPDATE SET
				monthly_limit = EXCLUDED.monthly_limit,
				rpm_limit = EXCLUDED.rpm_limit,
				updated_at = now()
		`, tenantID, b.Department, b.MonthlyLimit, b.RPMLimit)
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
		SELECT tenant_id, webhook_url, monthly_token_limit, tokens_used, period_start::text
		FROM public.ai_tenant_quota_settings
		WHERE tenant_id = $1
	`, tenantID).Scan(&set.TenantID, &set.WebhookURL, &set.MonthlyTokenLimit, &set.TokensUsed, &set.PeriodStart)
	if err == sql.ErrNoRows {
		return &QuotaSettings{TenantID: tenantID}, nil
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
		INSERT INTO public.ai_tenant_quota_settings (tenant_id, webhook_url, monthly_token_limit, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (tenant_id) DO UPDATE SET
			webhook_url = EXCLUDED.webhook_url,
			monthly_token_limit = EXCLUDED.monthly_token_limit,
			updated_at = now()
	`, settings.TenantID, settings.WebhookURL, settings.MonthlyTokenLimit)
	if err != nil {
		return fmt.Errorf("save quota settings: %w", err)
	}
	return nil
}

func (s *SQLRunStore) ReserveQuota(ctx context.Context, tenantID, externalRunID string, estimatedTokens int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("database not available")
	}
	if estimatedTokens <= 0 {
		estimatedTokens = 4096
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin quota reservation: %w", err)
	}
	defer tx.Rollback()
	var limit, used int64
	var period string
	err = tx.QueryRowContext(ctx, `
		SELECT monthly_token_limit, tokens_used, period_start::text
		FROM public.ai_tenant_quota_settings
		WHERE tenant_id = $1 FOR UPDATE`, tenantID).Scan(&limit, &used, &period)
	if err == sql.ErrNoRows {
		// No tenant quota configured means the platform default applies.
		return tx.Commit()
	}
	if err != nil {
		return fmt.Errorf("load quota settings: %w", err)
	}
	currentPeriod := ardatime.NowCtx(ctx).Format("2006-01-02")[:8] + "01"
	if !strings.HasPrefix(period, currentPeriod[:7]) {
		used = 0
		period = currentPeriod
		if _, err := tx.ExecContext(ctx, `UPDATE public.ai_tenant_quota_settings SET tokens_used = 0, period_start = $2::date, updated_at = now() WHERE tenant_id = $1`, tenantID, period); err != nil {
			return fmt.Errorf("reset quota period: %w", err)
		}
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT status FROM public.ai_quota_reservations WHERE tenant_id = $1 AND external_run_id = $2 FOR UPDATE`, tenantID, externalRunID).Scan(&existing)
	if err == nil {
		if existing == "RESERVED" {
			return tx.Commit()
		}
		return ErrQuotaExceeded
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("load quota reservation: %w", err)
	}
	if limit > 0 && used+estimatedTokens > limit {
		return ErrQuotaExceeded
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.ai_quota_reservations (tenant_id, external_run_id, period_start, reserved_tokens)
		VALUES ($1, $2, $3::date, $4)`, tenantID, externalRunID, period, estimatedTokens); err != nil {
		return fmt.Errorf("save quota reservation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.ai_tenant_quota_settings SET tokens_used = tokens_used + $2, updated_at = now() WHERE tenant_id = $1`, tenantID, estimatedTokens); err != nil {
		return fmt.Errorf("reserve quota tokens: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quota reservation: %w", err)
	}
	return nil
}

func (s *SQLRunStore) FinalizeQuota(ctx context.Context, tenantID, externalRunID string, actualTokens int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("database not available")
	}
	if actualTokens < 0 {
		actualTokens = 0
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var reserved int64
	err = tx.QueryRowContext(ctx, `SELECT reserved_tokens FROM public.ai_quota_reservations WHERE tenant_id = $1 AND external_run_id = $2 AND status = 'RESERVED' FOR UPDATE`, tenantID, externalRunID).Scan(&reserved)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	delta := actualTokens - reserved
	if _, err := tx.ExecContext(ctx, `UPDATE public.ai_quota_reservations SET actual_tokens = $3, status = 'FINALIZED', finalized_at = now() WHERE tenant_id = $1 AND external_run_id = $2`, tenantID, externalRunID, actualTokens); err != nil {
		return err
	}
	if delta != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE public.ai_tenant_quota_settings SET tokens_used = GREATEST(0, tokens_used + $2), updated_at = now() WHERE tenant_id = $1`, tenantID, delta); err != nil {
			return err
		}
	}
	return tx.Commit()
}
