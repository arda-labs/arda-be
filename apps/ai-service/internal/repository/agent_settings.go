package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// AgentSettings is the per-tenant act-mode configuration: whether the assistant
// may execute confirm-kind tools without a human approval, and the risk ceiling
// it may do so for.
type AgentSettings struct {
	ActModeEnabled bool
	ActModeMaxRisk string
}

// AgentSettingsStore reads and writes the act-mode configuration. A tenant
// without a row defaults to disabled so an unconfigured tenant never
// auto-executes anything.
type AgentSettingsStore interface {
	GetAgentSettings(ctx context.Context, tenantID string) (*AgentSettings, error)
	UpsertAgentSettings(ctx context.Context, tenantID string, enabled bool, maxRisk string) error
}

const defaultActModeMaxRisk = "medium"

func (s *SQLRunStore) GetAgentSettings(ctx context.Context, tenantID string) (*AgentSettings, error) {
	fallback := &AgentSettings{ActModeEnabled: false, ActModeMaxRisk: defaultActModeMaxRisk}
	if s == nil || s.db == nil {
		return fallback, nil
	}
	var item AgentSettings
	err := s.db.QueryRowContext(ctx, `
		SELECT act_mode_enabled, act_mode_max_risk
		FROM public.ai_agent_settings
		WHERE tenant_id = $1
	`, tenantID).Scan(&item.ActModeEnabled, &item.ActModeMaxRisk)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fallback, nil
		}
		return nil, fmt.Errorf("query AI agent settings: %w", err)
	}
	if item.ActModeMaxRisk == "" {
		item.ActModeMaxRisk = defaultActModeMaxRisk
	}
	return &item, nil
}

// UpsertAgentSettings stores the tenant's act-mode choice. The risk ceiling is
// clamped to low/medium: high is never auto-approved.
func (s *SQLRunStore) UpsertAgentSettings(ctx context.Context, tenantID string, enabled bool, maxRisk string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("AI run store is not configured")
	}
	risk := strings.ToLower(strings.TrimSpace(maxRisk))
	if risk != "low" && risk != "medium" {
		risk = defaultActModeMaxRisk
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO public.ai_agent_settings (tenant_id, act_mode_enabled, act_mode_max_risk, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (tenant_id) DO UPDATE
		SET act_mode_enabled = EXCLUDED.act_mode_enabled,
		    act_mode_max_risk = EXCLUDED.act_mode_max_risk,
		    updated_at = now()
	`, tenantID, enabled, risk); err != nil {
		return fmt.Errorf("upsert AI agent settings: %w", err)
	}
	return nil
}
