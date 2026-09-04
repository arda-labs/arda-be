package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type TenantRoutingRules struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"tenantId"`
	FastModel         string    `json:"fastModel"`
	CodeModel         string    `json:"codeModel"`
	SensitiveModel    string    `json:"sensitiveModel"`
	PrimaryProvider   string    `json:"primaryProvider"`
	SecondaryProvider string    `json:"secondaryProvider"`
	FailoverProvider  string    `json:"failoverProvider"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type RoutingStore interface {
	GetRoutingRules(ctx context.Context, tenantID string) (*TenantRoutingRules, error)
	SaveRoutingRules(ctx context.Context, rules TenantRoutingRules) (*TenantRoutingRules, error)
}

func defaultRoutingRules(tenantID string) TenantRoutingRules {
	return TenantRoutingRules{
		TenantID:          tenantID,
		FastModel:         "gemini-2.5-flash",
		CodeModel:         "claude-3.5-sonnet",
		SensitiveModel:    "qwen2.5:7b-instruct-q4_K_M",
		PrimaryProvider:   "gemini",
		SecondaryProvider: "openai",
		FailoverProvider:  "ollama",
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
}

func (s *SQLRunStore) GetRoutingRules(ctx context.Context, tenantID string) (*TenantRoutingRules, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	var r TenantRoutingRules
	err := s.db.QueryRowContext(ctx, `
		SELECT id::text, tenant_id, fast_model, code_model, sensitive_model,
		       primary_provider, secondary_provider, failover_provider,
		       created_at, updated_at
		FROM public.ai_tenant_routing_rules
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&r.ID, &r.TenantID, &r.FastModel, &r.CodeModel, &r.SensitiveModel,
		&r.PrimaryProvider, &r.SecondaryProvider, &r.FailoverProvider,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		def := defaultRoutingRules(tenantID)
		return &def, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant routing rules: %w", err)
	}
	return &r, nil
}

func (s *SQLRunStore) SaveRoutingRules(ctx context.Context, rules TenantRoutingRules) (*TenantRoutingRules, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	var r TenantRoutingRules
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO public.ai_tenant_routing_rules (
			tenant_id, fast_model, code_model, sensitive_model,
			primary_provider, secondary_provider, failover_provider,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (tenant_id) DO UPDATE SET
			fast_model = EXCLUDED.fast_model,
			code_model = EXCLUDED.code_model,
			sensitive_model = EXCLUDED.sensitive_model,
			primary_provider = EXCLUDED.primary_provider,
			secondary_provider = EXCLUDED.secondary_provider,
			failover_provider = EXCLUDED.failover_provider,
			updated_at = now()
		RETURNING id::text, tenant_id, fast_model, code_model, sensitive_model,
		          primary_provider, secondary_provider, failover_provider,
		          created_at, updated_at
	`,
		rules.TenantID, rules.FastModel, rules.CodeModel, rules.SensitiveModel,
		rules.PrimaryProvider, rules.SecondaryProvider, rules.FailoverProvider,
	).Scan(
		&r.ID, &r.TenantID, &r.FastModel, &r.CodeModel, &r.SensitiveModel,
		&r.PrimaryProvider, &r.SecondaryProvider, &r.FailoverProvider,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("save tenant routing rules: %w", err)
	}
	return &r, nil
}
