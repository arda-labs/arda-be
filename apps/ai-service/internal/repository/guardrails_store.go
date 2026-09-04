package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type TenantGuardrails struct {
	TenantID               string    `json:"tenantId"`
	PromptInjectionDefense bool      `json:"promptInjectionDefense"`
	PIIMasking             bool      `json:"piiMasking"`
	HallucinationCheck     bool      `json:"hallucinationCheck"`
	ZeroRetention          bool      `json:"zeroRetention"`
	InjectionThreshold     float32   `json:"injectionThreshold"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type GuardrailsStore interface {
	GetGuardrails(ctx context.Context, tenantID string) (*TenantGuardrails, error)
	SaveGuardrails(ctx context.Context, g TenantGuardrails) (*TenantGuardrails, error)
}

func defaultGuardrails(tenantID string) TenantGuardrails {
	return TenantGuardrails{
		TenantID:               tenantID,
		PromptInjectionDefense: true,
		PIIMasking:             true,
		HallucinationCheck:     true,
		ZeroRetention:          true,
		InjectionThreshold:     0.85,
		CreatedAt:              time.Now(),
		UpdatedAt:              time.Now(),
	}
}

func (s *SQLRunStore) GetGuardrails(ctx context.Context, tenantID string) (*TenantGuardrails, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	var g TenantGuardrails
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, prompt_injection_defense, pii_masking,
		       hallucination_check, zero_retention, injection_threshold,
		       created_at, updated_at
		FROM public.ai_tenant_guardrails
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&g.TenantID, &g.PromptInjectionDefense, &g.PIIMasking,
		&g.HallucinationCheck, &g.ZeroRetention, &g.InjectionThreshold,
		&g.CreatedAt, &g.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		def := defaultGuardrails(tenantID)
		return &def, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant guardrails: %w", err)
	}
	return &g, nil
}

func (s *SQLRunStore) SaveGuardrails(ctx context.Context, g TenantGuardrails) (*TenantGuardrails, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	var res TenantGuardrails
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO public.ai_tenant_guardrails (
			tenant_id, prompt_injection_defense, pii_masking,
			hallucination_check, zero_retention, injection_threshold,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (tenant_id) DO UPDATE SET
			prompt_injection_defense = EXCLUDED.prompt_injection_defense,
			pii_masking = EXCLUDED.pii_masking,
			hallucination_check = EXCLUDED.hallucination_check,
			zero_retention = EXCLUDED.zero_retention,
			injection_threshold = EXCLUDED.injection_threshold,
			updated_at = now()
		RETURNING tenant_id, prompt_injection_defense, pii_masking,
		          hallucination_check, zero_retention, injection_threshold,
		          created_at, updated_at
	`,
		g.TenantID, g.PromptInjectionDefense, g.PIIMasking,
		g.HallucinationCheck, g.ZeroRetention, g.InjectionThreshold,
	).Scan(
		&res.TenantID, &res.PromptInjectionDefense, &res.PIIMasking,
		&res.HallucinationCheck, &res.ZeroRetention, &res.InjectionThreshold,
		&res.CreatedAt, &res.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("save tenant guardrails: %w", err)
	}
	return &res, nil
}
