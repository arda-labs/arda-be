package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	TenantID string `json:"tenantId"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	ModelID  string `json:"modelId"`
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
	err := s.db.QueryRowContext(ctx, `
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

	item.APIKey = s.decryptSecret(rawAPIKey)
	return &item, nil
}

func (s *SQLRunStore) UpsertTenantSettings(ctx context.Context, settings TenantSettings) error {
	if s == nil || s.db == nil {
		return errors.New("database not available")
	}

	apiKeyToSave := strings.TrimSpace(settings.APIKey)
	if apiKeyToSave != "" && !strings.HasPrefix(apiKeyToSave, "enc:v1:") && s.encryptionSecret != "" {
		encrypted, err := ardacrypto.Encrypt(apiKeyToSave, s.encryptionSecret)
		if err != nil {
			return fmt.Errorf("encrypt tenant api key: %w", err)
		}
		apiKeyToSave = encrypted
	}

	_, err := s.db.ExecContext(ctx, `
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

func (s *SQLRunStore) decryptSecret(raw string) string {
	if s.encryptionSecret != "" && strings.HasPrefix(raw, "enc:v1:") {
		if decrypted, err := ardacrypto.Decrypt(raw, s.encryptionSecret); err == nil {
			return decrypted
		}
	}
	return raw
}
