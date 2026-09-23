package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AgentSettings is the per-tenant act-mode configuration: whether the assistant
// may execute confirm-kind tools without a human approval, and the risk ceiling
// it may do so for.
type AgentSettings struct {
	ActModeEnabled bool
	ActModeMaxRisk string
}

// AgentSettingsStore reads the act-mode configuration. A tenant without a row
// defaults to disabled so an unconfigured tenant never auto-executes anything.
type AgentSettingsStore interface {
	GetAgentSettings(ctx context.Context, tenantID string) (*AgentSettings, error)
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
