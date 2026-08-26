package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/crypto"
)

var (
	ErrTenantSettingsNotFound = errors.New("ai tenant settings not found")
)

type TenantSettings struct {
	TenantID     string    `json:"tenantId"`
	ProviderType string    `json:"providerType"`
	BaseURL      string    `json:"baseUrl"`
	APIKey       string    `json:"apiKey"`
	ModelID      string    `json:"modelId"`
	Temperature  float32   `json:"temperature"`
	IsActive     bool      `json:"isActive"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type TenantSettingsStore interface {
	GetTenantSettings(ctx context.Context, tenantID string) (*TenantSettings, error)
	UpsertTenantSettings(ctx context.Context, settings TenantSettings) error
}

func (s *SQLRunStore) GetTenantSettings(ctx context.Context, tenantID string) (*TenantSettings, error) {
	if s == nil || s.db == nil {
		return nil, ErrTenantSettingsNotFound
	}

	query := `
		SELECT tenant_id, provider_type, base_url, api_key, model_id, temperature, is_active, created_at, updated_at
		FROM public.ai_tenant_settings
		WHERE tenant_id = $1 AND is_active = true
	`

	var item TenantSettings
	var rawAPIKey string
	err := s.db.QueryRowContext(ctx, query, tenantID).Scan(
		&item.TenantID,
		&item.ProviderType,
		&item.BaseURL,
		&rawAPIKey,
		&item.ModelID,
		&item.Temperature,
		&item.IsActive,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTenantSettingsNotFound
		}
		return nil, fmt.Errorf("query tenant settings: %w", err)
	}

	// Decrypt API key if encrypted
	if s.encryptionSecret != "" && strings.HasPrefix(rawAPIKey, "enc:v1:") {
		decrypted, decErr := crypto.Decrypt(rawAPIKey, s.encryptionSecret)
		if decErr == nil {
			item.APIKey = decrypted
		} else {
			item.APIKey = rawAPIKey
		}
	} else {
		item.APIKey = rawAPIKey
	}

	return &item, nil
}

func (s *SQLRunStore) UpsertTenantSettings(ctx context.Context, settings TenantSettings) error {
	if s == nil || s.db == nil {
		return errors.New("database not available")
	}

	apiKeyToSave := strings.TrimSpace(settings.APIKey)
	// If API key is provided and not already encrypted, encrypt it with AES-256-GCM
	if apiKeyToSave != "" && !strings.HasPrefix(apiKeyToSave, "enc:v1:") && s.encryptionSecret != "" {
		encrypted, err := crypto.Encrypt(apiKeyToSave, s.encryptionSecret)
		if err == nil {
			apiKeyToSave = encrypted
		}
	}

	query := `
		INSERT INTO public.ai_tenant_settings (
			tenant_id, provider_type, base_url, api_key, model_id, temperature, is_active, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
		ON CONFLICT (tenant_id) DO UPDATE SET
			provider_type = EXCLUDED.provider_type,
			base_url = EXCLUDED.base_url,
			api_key = CASE 
				WHEN EXCLUDED.api_key = '' OR EXCLUDED.api_key LIKE 'sk-%...%' THEN public.ai_tenant_settings.api_key
				ELSE EXCLUDED.api_key
			END,
			model_id = EXCLUDED.model_id,
			temperature = EXCLUDED.temperature,
			is_active = EXCLUDED.is_active,
			updated_at = now()
	`

	_, err := s.db.ExecContext(ctx, query,
		settings.TenantID,
		settings.ProviderType,
		settings.BaseURL,
		apiKeyToSave,
		settings.ModelID,
		settings.Temperature,
		settings.IsActive,
	)
	if err != nil {
		return fmt.Errorf("upsert tenant settings: %w", err)
	}

	return nil
}
