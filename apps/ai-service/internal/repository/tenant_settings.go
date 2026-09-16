package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	ardacrypto "github.com/arda-labs/arda/libs/go/arda-crypto"
)

var (
	ErrTenantSettingsNotFound = errors.New("ai tenant settings not found")
)

// TenantSettings is the single active model configuration for a tenant. It is
// owned by the tenant through the AI Settings UI; the deployment only supplies
// shared security controls (gateway token, base-URL allowlist).
type TenantSettings struct {
	TenantID     string `json:"tenantId"`
	BaseURL      string `json:"baseUrl"`
	ProviderType string `json:"providerType"`
	APIKey       string `json:"apiKey"`
	ModelID      string `json:"modelId"`
}

type TenantSettingsStore interface {
	GetTenantSettings(ctx context.Context, tenantID string) (*TenantSettings, error)
	UpsertTenantSettings(ctx context.Context, settings TenantSettings) error
}

func (s *SQLRunStore) GetTenantSettings(ctx context.Context, tenantID string) (*TenantSettings, error) {
	if s == nil || s.db == nil {
		return nil, ErrTenantSettingsNotFound
	}

	var item TenantSettings
	var rawAPIKey string
	// Preferred source: the applied profile + applied model.
	err := s.db.QueryRowContext(ctx, `
		SELECT p.tenant_id, p.base_url, p.provider_type, p.api_key, m.model_id
		FROM public.ai_model_profiles p
		JOIN public.ai_profile_models m ON m.profile_id = p.id
		WHERE p.tenant_id = $1 AND p.is_active = true AND m.is_active = true
		LIMIT 1
	`, tenantID).Scan(&item.TenantID, &item.BaseURL, &item.ProviderType, &rawAPIKey, &item.ModelID)
	if err == nil {
		apiKey, decryptErr := s.decryptSecret(rawAPIKey)
		if decryptErr != nil {
			slog.Error("decrypt tenant model api key failed", "tenant_id", tenantID, "err", decryptErr)
			return nil, fmt.Errorf("decrypt tenant model api key: %w", decryptErr)
		}
		item.APIKey = apiKey
		return &item, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query active model profile: %w", err)
	}

	// Legacy fallback for tenants configured before profiles existed.
	err = s.db.QueryRowContext(ctx, `
		SELECT tenant_id, base_url, api_key, model_id
		FROM public.ai_tenant_settings
		WHERE tenant_id = $1 AND is_active = true
	`, tenantID).Scan(&item.TenantID, &item.BaseURL, &rawAPIKey, &item.ModelID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTenantSettingsNotFound
		}
		return nil, fmt.Errorf("query tenant settings: %w", err)
	}

	apiKey, decryptErr := s.decryptSecret(rawAPIKey)
	if decryptErr != nil {
		slog.Error("decrypt tenant model api key failed", "tenant_id", tenantID, "err", decryptErr)
		return nil, fmt.Errorf("decrypt tenant model api key: %w", decryptErr)
	}
	item.APIKey = apiKey
	item.ProviderType = "openai-compatible"
	return &item, nil
}

func (s *SQLRunStore) UpsertTenantSettings(ctx context.Context, settings TenantSettings) error {
	if s == nil || s.db == nil {
		return errors.New("database not available")
	}

	apiKeyToSave, err := s.encryptSecret(strings.TrimSpace(settings.APIKey))
	if err != nil {
		return fmt.Errorf("encrypt tenant api key: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO public.ai_tenant_settings (
			tenant_id, base_url, api_key, model_id, is_active, created_at, updated_at
		) VALUES ($1, $2, $3, $4, true, now(), now())
		ON CONFLICT (tenant_id) DO UPDATE SET
			base_url = EXCLUDED.base_url,
			api_key = EXCLUDED.api_key,
			model_id = EXCLUDED.model_id,
			is_active = true,
			updated_at = now()
	`, settings.TenantID, strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/"),
		apiKeyToSave, strings.TrimSpace(settings.ModelID))
	if err != nil {
		return fmt.Errorf("upsert tenant settings: %w", err)
	}
	return nil
}

// decryptSecret resolves a stored secret to plaintext. It fails closed: a
// value marked `enc:v1:` that cannot be decrypted (missing/rotated secret or
// damaged ciphertext) must never be forwarded to a provider as ciphertext.
func (s *SQLRunStore) decryptSecret(raw string) (string, error) {
	if raw == "" || !strings.HasPrefix(raw, "enc:v1:") {
		return raw, nil
	}
	if strings.TrimSpace(s.encryptionSecret) == "" {
		return "", errors.New("encryption secret is not configured")
	}
	decrypted, err := ardacrypto.Decrypt(raw, s.encryptionSecret)
	if err != nil {
		return "", fmt.Errorf("decrypt encrypted value: %w", err)
	}
	return decrypted, nil
}
