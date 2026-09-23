package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/decision"
	ardacrypto "github.com/arda-labs/arda/libs/go/arda-crypto"
)

type DecisionSettingsStore interface {
	GetDecisionSettings(context.Context, string) (decision.Settings, error)
	SaveDecisionSettings(context.Context, string, decision.Settings, *string) error
}

type DecisionRecorder interface {
	RecordDecision(context.Context, RunContext, *decision.Result, string, float64, int64, bool) error
}

func (s *SQLRunStore) RecordDecision(ctx context.Context, run RunContext, result *decision.Result, skill string, confidence float64, latencyMs int64, lowConfidence bool) error {
	payload, err := json.Marshal(map[string]any{"model_id": result.Model, "skill": skill,
		"input_tokens": result.Usage.InputTokens, "output_tokens": result.Usage.OutputTokens,
		"confidence": confidence, "latency_ms": latencyMs, "low_confidence": lowConfidence})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE public.ai_runs SET decision_usage = $4::jsonb
		WHERE tenant_id = $1 AND actor_user_id = $2 AND external_run_id = $3`,
		run.TenantID, run.ActorUserID, run.ExternalRun, string(payload))
	return err
}

func (s *SQLRunStore) GetDecisionSettings(ctx context.Context, tenantID string) (decision.Settings, error) {
	item := decision.Defaults()
	var encrypted string
	err := s.db.QueryRowContext(ctx, `SELECT enabled, model_id, min_confidence, api_key
		FROM public.ai_decision_settings WHERE tenant_id = $1`, tenantID).
		Scan(&item.Enabled, &item.ModelID, &item.MinConfidence, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return item, err
	}
	item.APIKey, err = s.decryptSecret(encrypted)
	return item, err
}

// A nil key preserves the existing encrypted credential atomically. Empty clears it.
func (s *SQLRunStore) SaveDecisionSettings(ctx context.Context, tenantID string, item decision.Settings, key *string) error {
	if strings.TrimSpace(tenantID) == "" || !item.Valid() {
		return errors.New("invalid decision settings")
	}
	var encrypted any
	if key != nil {
		value := strings.TrimSpace(*key)
		if value != "" {
			if s.encryptionSecret == "" {
				return errors.New("encryption secret is not configured")
			}
			var err error
			value, err = ardacrypto.Encrypt(value, s.encryptionSecret)
			if err != nil {
				return err
			}
		}
		encrypted = value
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO public.ai_decision_settings
		(tenant_id, enabled, model_id, min_confidence, api_key)
		VALUES ($1::varchar(64), $2, $3, $4, COALESCE($5::text,
		    (SELECT api_key FROM public.ai_decision_settings WHERE tenant_id = $1::varchar(64)), ''))
		ON CONFLICT (tenant_id) DO UPDATE SET enabled = EXCLUDED.enabled,
		model_id = EXCLUDED.model_id, min_confidence = EXCLUDED.min_confidence,
		api_key = COALESCE($5::text, ai_decision_settings.api_key), updated_at = now()`,
		tenantID, item.Enabled, item.ModelID, item.MinConfidence, encrypted)
	return err
}
