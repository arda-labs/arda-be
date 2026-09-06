package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	ardapg "github.com/arda-labs/arda/libs/go/arda-postgres"
)


type AgentConfig struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenantId"`
	Name         string    `json:"name"`
	Department   string    `json:"department"`
	Description  string    `json:"description"`
	SystemPrompt string    `json:"systemPrompt"`
	ModelID      string    `json:"modelId"`
	Temperature  float32   `json:"temperature"`
	AllowedTools []string  `json:"allowedTools"`
	IsActive     bool      `json:"isActive"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type AgentStore interface {
	ListAgents(ctx context.Context, tenantID string) ([]AgentConfig, error)
	SaveAgent(ctx context.Context, agent AgentConfig) (*AgentConfig, error)
	DeleteAgent(ctx context.Context, tenantID, agentID string) error
}

func (s *SQLRunStore) ListAgents(ctx context.Context, tenantID string) ([]AgentConfig, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("agent persistence is unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, department, coalesce(description, ''), system_prompt,
		       model_id, temperature, allowed_tools, is_active, created_at, updated_at
		FROM public.ai_agents
		WHERE tenant_id = $1 OR tenant_id = ''
		ORDER BY department ASC, name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	agents := make([]AgentConfig, 0)
	for rows.Next() {
		var a AgentConfig
		var tools []string
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.Name, &a.Department, &a.Description, &a.SystemPrompt,
			&a.ModelID, &a.Temperature, ardapg.Driver.Scanner(&tools), &a.IsActive, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent config: %w", err)
		}
		a.AllowedTools = tools
		agents = append(agents, a)
	}

	if len(agents) == 0 {
		return []AgentConfig{}, nil
	}
	return agents, nil
}

func (s *SQLRunStore) SaveAgent(ctx context.Context, agent AgentConfig) (*AgentConfig, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("agent persistence is unavailable")
	}
	agent.Name = strings.TrimSpace(agent.Name)
	if agent.Name == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	if strings.TrimSpace(agent.ID) == "" {
		agent.ID = strings.ToLower(strings.ReplaceAll(agent.Name, " ", "-"))
	}
	if agent.Department == "" {
		agent.Department = "General"
	}
	if agent.ModelID == "" {
		agent.ModelID = "gemini-2.5-flash"
	}
	if agent.Temperature <= 0 || agent.Temperature > 2 {
		agent.Temperature = 0.2
	}
	now := time.Now()
	agent.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO public.ai_agents
			(id, tenant_id, name, department, description, system_prompt, model_id, temperature, allowed_tools, is_active, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
		ON CONFLICT (tenant_id, name) DO UPDATE SET
			department = EXCLUDED.department,
			description = EXCLUDED.description,
			system_prompt = EXCLUDED.system_prompt,
			model_id = EXCLUDED.model_id,
			temperature = EXCLUDED.temperature,
			allowed_tools = EXCLUDED.allowed_tools,
			is_active = EXCLUDED.is_active,
			updated_at = EXCLUDED.updated_at
	`, agent.ID, agent.TenantID, agent.Name, agent.Department, agent.Description, agent.SystemPrompt,
		agent.ModelID, agent.Temperature, ardapg.Driver.NotNil(agent.AllowedTools), agent.IsActive, now)
	if err != nil {
		return nil, fmt.Errorf("save agent: %w", err)
	}
	return &agent, nil
}

func (s *SQLRunStore) DeleteAgent(ctx context.Context, tenantID, agentID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("agent persistence is unavailable")
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM public.ai_agents
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, agentID)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	return nil
}
