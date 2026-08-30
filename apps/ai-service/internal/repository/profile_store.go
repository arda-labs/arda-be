package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	ardacrypto "github.com/arda-labs/arda/libs/go/arda-crypto"
)

var (
	ErrProfileNotFound   = errors.New("ai tenant setting profile not found")
	ErrProfileNameExists = errors.New("ai tenant setting profile name already exists")
)

// TenantSettingProfile is one saved model configuration. Exactly one profile
// per tenant carries is_active; activating it syncs the values into
// ai_tenant_settings, which is what the agent loop reads.
type TenantSettingProfile struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenantId"`
	Name         string    `json:"name"`
	ProviderType string    `json:"providerType"`
	BaseURL      string    `json:"baseUrl"`
	APIKey       string    `json:"apiKey"`
	ModelID      string    `json:"modelId"`
	Temperature  float32   `json:"temperature"`
	IsActive     bool      `json:"isActive"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ProfileStore interface {
	ListProfiles(ctx context.Context, tenantID string) ([]TenantSettingProfile, error)
	CreateProfile(ctx context.Context, profile TenantSettingProfile) (*TenantSettingProfile, error)
	DeleteProfile(ctx context.Context, tenantID, profileID string) error
	// ActivateProfile flips the active flags and syncs the profile into
	// ai_tenant_settings atomically. It returns the activated profile.
	ActivateProfile(ctx context.Context, tenantID, profileID string) (*TenantSettingProfile, error)
}

func (s *SQLRunStore) ListProfiles(ctx context.Context, tenantID string) ([]TenantSettingProfile, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, name, provider_type, base_url, api_key, model_id,
		       temperature, is_active, created_at, updated_at
		FROM public.ai_tenant_setting_profiles
		WHERE tenant_id = $1
		ORDER BY is_active DESC, name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant setting profiles: %w", err)
	}
	defer rows.Close()

	profiles := make([]TenantSettingProfile, 0)
	for rows.Next() {
		var profile TenantSettingProfile
		if err := rows.Scan(
			&profile.ID, &profile.TenantID, &profile.Name, &profile.ProviderType,
			&profile.BaseURL, &profile.APIKey, &profile.ModelID,
			&profile.Temperature, &profile.IsActive, &profile.CreatedAt, &profile.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan tenant setting profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant setting profiles: %w", err)
	}
	return profiles, nil
}

func (s *SQLRunStore) CreateProfile(ctx context.Context, profile TenantSettingProfile) (*TenantSettingProfile, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	profile.Name = strings.TrimSpace(profile.Name)
	profile.BaseURL = strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/")
	profile.ModelID = strings.TrimSpace(profile.ModelID)
	profile.ProviderType = strings.TrimSpace(profile.ProviderType)
	if profile.ProviderType == "" {
		profile.ProviderType = "openai"
	}
	if profile.Name == "" || profile.BaseURL == "" || profile.ModelID == "" {
		return nil, fmt.Errorf("name, base_url and model_id are required")
	}
	if profile.Temperature <= 0 || profile.Temperature > 2 {
		profile.Temperature = 0.2
	}

	apiKeyToSave := strings.TrimSpace(profile.APIKey)
	if apiKeyToSave == "" {
		return nil, fmt.Errorf("api_key is required")
	}
	if !strings.HasPrefix(apiKeyToSave, "enc:v1:") && s.encryptionSecret != "" {
		if encrypted, err := ardacrypto.Encrypt(apiKeyToSave, s.encryptionSecret); err == nil {
			apiKeyToSave = encrypted
		}
	}

	var created TenantSettingProfile
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO public.ai_tenant_setting_profiles
			(tenant_id, name, provider_type, base_url, api_key, model_id, temperature, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, created_at, updated_at
	`, profile.TenantID, profile.Name, profile.ProviderType, profile.BaseURL,
		apiKeyToSave, profile.ModelID, profile.Temperature, profile.IsActive,
	).Scan(&created.ID, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrProfileNameExists
		}
		return nil, fmt.Errorf("create tenant setting profile: %w", err)
	}
	created.TenantID = profile.TenantID
	created.Name = profile.Name
	created.ProviderType = profile.ProviderType
	created.BaseURL = profile.BaseURL
	created.APIKey = apiKeyToSave
	created.ModelID = profile.ModelID
	created.Temperature = profile.Temperature
	created.IsActive = profile.IsActive
	return &created, nil
}

func (s *SQLRunStore) DeleteProfile(ctx context.Context, tenantID, profileID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("database not available")
	}
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM public.ai_tenant_setting_profiles
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, profileID)
	if err != nil {
		return fmt.Errorf("delete tenant setting profile: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return ErrProfileNotFound
	}
	return nil
}

// ActivateProfile switches the tenant's active profile in one transaction and
// mirrors the profile into ai_tenant_settings so the running agent picks it
// up on the next request.
func (s *SQLRunStore) ActivateProfile(ctx context.Context, tenantID, profileID string) (*TenantSettingProfile, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin profile activation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE public.ai_tenant_setting_profiles SET is_active = false, updated_at = now()
		WHERE tenant_id = $1 AND is_active
	`, tenantID); err != nil {
		return nil, fmt.Errorf("deactivate profiles: %w", err)
	}

	var profile TenantSettingProfile
	err = tx.QueryRowContext(ctx, `
		UPDATE public.ai_tenant_setting_profiles SET is_active = true, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING name, provider_type, base_url, api_key, model_id, temperature
	`, tenantID, profileID).Scan(
		&profile.Name, &profile.ProviderType, &profile.BaseURL,
		&profile.APIKey, &profile.ModelID, &profile.Temperature,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrProfileNotFound
		}
		return nil, fmt.Errorf("activate tenant setting profile: %w", err)
	}

	// Mirror into the live settings table the agent loop reads. A masked key
	// ("sk-1...xyz9") keeps the previously stored key.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.ai_tenant_settings
			(tenant_id, provider_type, base_url, api_key, model_id, temperature, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, true)
		ON CONFLICT (tenant_id) DO UPDATE SET
			provider_type = EXCLUDED.provider_type,
			base_url = EXCLUDED.base_url,
			api_key = EXCLUDED.api_key,
			model_id = EXCLUDED.model_id,
			temperature = EXCLUDED.temperature,
			is_active = true,
			updated_at = now()
	`, tenantID, profile.ProviderType, profile.BaseURL, profile.APIKey,
		profile.ModelID, profile.Temperature); err != nil {
		return nil, fmt.Errorf("sync active profile into settings: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit profile activation: %w", err)
	}

	profile.ID = profileID
	profile.TenantID = tenantID
	profile.IsActive = true
	profile.APIKey = s.decryptKey(profile.APIKey)
	return &profile, nil
}

func (s *SQLRunStore) decryptKey(raw string) string {
	if s.encryptionSecret != "" && strings.HasPrefix(raw, "enc:v1:") {
		if decrypted, err := ardacrypto.Decrypt(raw, s.encryptionSecret); err == nil {
			return decrypted
		}
	}
	return raw
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "SQLSTATE 23505") ||
		strings.Contains(err.Error(), "duplicate key value")
}
