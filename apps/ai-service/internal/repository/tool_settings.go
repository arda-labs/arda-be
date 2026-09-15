package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ToolSetting is one platform-level runtime override on the AI tool catalog
// (ADR-003). Row absence means "follow the contract default in generated.go";
// a row with enabled=false is the operational kill switch. The runtime can
// never enable beyond the contract default.
type ToolSetting struct {
	MethodName string    `json:"methodName"`
	Enabled    bool      `json:"enabled"`
	UpdatedBy  string    `json:"updatedBy"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type ToolSettingsStore interface {
	ListToolSettings(ctx context.Context) ([]ToolSetting, error)
	UpsertToolSetting(ctx context.Context, methodName string, enabled bool, updatedBy string) (time.Time, error)
	DeleteToolSetting(ctx context.Context, methodName string) error
}

func (s *SQLRunStore) ListToolSettings(ctx context.Context) ([]ToolSetting, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT method_name, enabled, updated_by, updated_at
		FROM public.ai_tool_settings
		ORDER BY method_name
	`)
	if err != nil {
		return nil, fmt.Errorf("list tool settings: %w", err)
	}
	defer rows.Close()

	items := make([]ToolSetting, 0)
	for rows.Next() {
		var item ToolSetting
		if err := rows.Scan(&item.MethodName, &item.Enabled, &item.UpdatedBy, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan tool setting: %w", err)
		}
		item.MethodName = strings.TrimSpace(item.MethodName)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tool settings: %w", err)
	}
	return items, nil
}

func (s *SQLRunStore) UpsertToolSetting(ctx context.Context, methodName string, enabled bool, updatedBy string) (time.Time, error) {
	if s == nil || s.db == nil {
		return time.Time{}, fmt.Errorf("database not available")
	}
	var updatedAt time.Time
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO public.ai_tool_settings (method_name, enabled, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (method_name) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			updated_by = EXCLUDED.updated_by,
			updated_at = now()
		RETURNING updated_at
	`, strings.TrimSpace(methodName), enabled, strings.TrimSpace(updatedBy)).Scan(&updatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("upsert tool setting: %w", err)
	}
	return updatedAt, nil
}

func (s *SQLRunStore) DeleteToolSetting(ctx context.Context, methodName string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("database not available")
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM public.ai_tool_settings WHERE method_name = $1
	`, strings.TrimSpace(methodName)); err != nil {
		return fmt.Errorf("delete tool setting: %w", err)
	}
	return nil
}

// compile-time check: the SQL store satisfies the governance store contract.
var _ ToolSettingsStore = (*SQLRunStore)(nil)
