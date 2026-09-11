package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	ardacrypto "github.com/arda-labs/arda/libs/go/arda-crypto"
)

var (
	ErrModelProfileNotFound  = errors.New("ai model profile not found")
	ErrModelProfileNameTaken = errors.New("ai model profile name already exists")
	ErrModelNotFound         = errors.New("ai profile model not found")
	ErrModelDuplicate        = errors.New("ai profile model already exists")
)

// AIModelProfile is a named provider endpoint (base URL + API key) owned by a
// tenant. Each profile holds many model IDs; exactly one profile and one of
// its models are applied (is_active) and used by the runtime.
type AIModelProfile struct {
	ID        string                `json:"id"`
	TenantID  string                `json:"tenantId"`
	Name      string                `json:"name"`
	BaseURL   string                `json:"baseUrl"`
	APIKey    string                `json:"apiKey"`
	IsActive  bool                  `json:"isActive"`
	Models    []AIModelProfileModel `json:"models"`
	CreatedAt time.Time             `json:"createdAt"`
	UpdatedAt time.Time             `json:"updatedAt"`
}

type AIModelProfileModel struct {
	ID        string `json:"id"`
	ModelID   string `json:"modelId"`
	Label     string `json:"label,omitempty"`
	IsActive  bool   `json:"isActive"`
	SortOrder int    `json:"sortOrder"`
}

// ModelProfileStore manages multi-profile model configuration. It is a
// superset of TenantSettingsStore: GetTenantSettings resolves the active
// profile + active model.
type ModelProfileStore interface {
	ListProfiles(ctx context.Context, tenantID string) ([]AIModelProfile, error)
	CreateProfile(ctx context.Context, tenantID, name, baseURL, apiKey string, models []string) (*AIModelProfile, error)
	UpdateProfile(ctx context.Context, tenantID, profileID, name, baseURL, apiKey string) (*AIModelProfile, error)
	DeleteProfile(ctx context.Context, tenantID, profileID string) error
	AddProfileModels(ctx context.Context, tenantID, profileID string, models []string) (*AIModelProfile, error)
	DeleteProfileModel(ctx context.Context, tenantID, profileID, modelID string) error
	ApplyProfileModel(ctx context.Context, tenantID, profileID, modelID string) (*AIModelProfile, error)
}

func newAIID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

// encryptSecret encrypts a plaintext secret (idempotent: already-encrypted or
// empty values pass through).
func (s *SQLRunStore) encryptSecret(plain string) (string, error) {
	value := strings.TrimSpace(plain)
	if value == "" || strings.HasPrefix(value, "enc:v1:") || s.encryptionSecret == "" {
		return value, nil
	}
	encrypted, err := ardacrypto.Encrypt(value, s.encryptionSecret)
	if err != nil {
		return "", fmt.Errorf("encrypt api key: %w", err)
	}
	return encrypted, nil
}

func (s *SQLRunStore) profileByID(ctx context.Context, tenantID, profileID string) (*AIModelProfile, error) {
	var p AIModelProfile
	var rawAPIKey string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, base_url, api_key, is_active, created_at, updated_at
		FROM public.ai_model_profiles
		WHERE id = $1 AND tenant_id = $2
	`, profileID, tenantID).Scan(&p.ID, &p.TenantID, &p.Name, &p.BaseURL, &rawAPIKey, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModelProfileNotFound
		}
		return nil, fmt.Errorf("query model profile: %w", err)
	}
	p.APIKey = s.decryptSecret(rawAPIKey)

	models, err := s.profileModels(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	p.Models = models
	return &p, nil
}

func (s *SQLRunStore) profileModels(ctx context.Context, profileID string) ([]AIModelProfileModel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, model_id, COALESCE(label, ''), is_active, sort_order
		FROM public.ai_profile_models
		WHERE profile_id = $1
		ORDER BY sort_order, model_id
	`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query profile models: %w", err)
	}
	defer rows.Close()

	models := []AIModelProfileModel{}
	for rows.Next() {
		var m AIModelProfileModel
		if err := rows.Scan(&m.ID, &m.ModelID, &m.Label, &m.IsActive, &m.SortOrder); err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

func (s *SQLRunStore) ListProfiles(ctx context.Context, tenantID string) ([]AIModelProfile, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("database not available")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, base_url, api_key, is_active, created_at, updated_at
		FROM public.ai_model_profiles
		WHERE tenant_id = $1
		ORDER BY is_active DESC, name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query model profiles: %w", err)
	}
	defer rows.Close()

	profiles := []AIModelProfile{}
	for rows.Next() {
		var p AIModelProfile
		var rawAPIKey string
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.BaseURL, &rawAPIKey, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.APIKey = s.decryptSecret(rawAPIKey)
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range profiles {
		models, err := s.profileModels(ctx, profiles[i].ID)
		if err != nil {
			return nil, err
		}
		profiles[i].Models = models
	}
	return profiles, nil
}

func (s *SQLRunStore) CreateProfile(ctx context.Context, tenantID, name, baseURL, apiKey string, models []string) (*AIModelProfile, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("database not available")
	}
	name = strings.TrimSpace(name)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	apiKey = strings.TrimSpace(apiKey)
	if name == "" || baseURL == "" || apiKey == "" {
		return nil, errors.New("name, base url and api key are required")
	}

	encrypted, err := s.encryptSecret(apiKey)
	if err != nil {
		return nil, err
	}

	id := newAIID("aip")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin profile tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.ai_model_profiles (id, tenant_id, name, base_url, api_key, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, false, now(), now())
	`, id, tenantID, name, baseURL, encrypted); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrModelProfileNameTaken
		}
		return nil, fmt.Errorf("insert model profile: %w", err)
	}

	if err := insertProfileModels(ctx, tx, id, models); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit profile: %w", err)
	}
	return s.profileByID(ctx, tenantID, id)
}

func (s *SQLRunStore) UpdateProfile(ctx context.Context, tenantID, profileID, name, baseURL, apiKey string) (*AIModelProfile, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("database not available")
	}
	existing, err := s.profileByID(ctx, tenantID, profileID)
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = existing.Name
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = existing.BaseURL
	}
	encrypted, err := s.encryptSecret(apiKey)
	if err != nil {
		return nil, err
	}
	if encrypted == "" {
		encrypted = existing.APIKey
		if plain, err := s.encryptSecret(encrypted); err == nil {
			encrypted = plain
		}
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE public.ai_model_profiles
		SET name = $3, base_url = $4, api_key = $5, updated_at = now()
		WHERE id = $1 AND tenant_id = $2
	`, profileID, tenantID, name, baseURL, encrypted); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrModelProfileNameTaken
		}
		return nil, fmt.Errorf("update model profile: %w", err)
	}
	return s.profileByID(ctx, tenantID, profileID)
}

func (s *SQLRunStore) DeleteProfile(ctx context.Context, tenantID, profileID string) error {
	if s == nil || s.db == nil {
		return errors.New("database not available")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM public.ai_model_profiles WHERE id = $1 AND tenant_id = $2
	`, profileID, tenantID)
	if err != nil {
		return fmt.Errorf("delete model profile: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrModelProfileNotFound
	}
	return nil
}

func (s *SQLRunStore) AddProfileModels(ctx context.Context, tenantID, profileID string, models []string) (*AIModelProfile, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("database not available")
	}
	if _, err := s.profileByID(ctx, tenantID, profileID); err != nil {
		return nil, err
	}
	cleaned := cleanModels(models)
	if len(cleaned) == 0 {
		return nil, errors.New("at least one model id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin model tx: %w", err)
	}
	defer tx.Rollback()
	if err := insertProfileModels(ctx, tx, profileID, cleaned); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.ai_model_profiles SET updated_at = now() WHERE id = $1
	`, profileID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit models: %w", err)
	}
	return s.profileByID(ctx, tenantID, profileID)
}

func (s *SQLRunStore) DeleteProfileModel(ctx context.Context, tenantID, profileID, modelID string) error {
	if s == nil || s.db == nil {
		return errors.New("database not available")
	}
	if _, err := s.profileByID(ctx, tenantID, profileID); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM public.ai_profile_models WHERE profile_id = $1 AND model_id = $2
	`, profileID, modelID)
	if err != nil {
		return fmt.Errorf("delete profile model: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrModelNotFound
	}
	return nil
}

// ApplyProfileModel atomically makes profileID + modelID the applied config:
// the profile becomes the tenant's only active profile and modelID the
// profile's only active model.
func (s *SQLRunStore) ApplyProfileModel(ctx context.Context, tenantID, profileID, modelID string) (*AIModelProfile, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("database not available")
	}
	if _, err := s.profileByID(ctx, tenantID, profileID); err != nil {
		return nil, err
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil, errors.New("model id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin apply tx: %w", err)
	}
	defer tx.Rollback()

	var found string
	if err := tx.QueryRowContext(ctx, `
		SELECT model_id FROM public.ai_profile_models
		WHERE profile_id = $1 AND model_id = $2
	`, profileID, modelID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModelNotFound
		}
		return nil, fmt.Errorf("resolve model: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE public.ai_model_profiles SET is_active = false WHERE tenant_id = $1 AND is_active = true
	`, tenantID); err != nil {
		return nil, fmt.Errorf("clear active profile: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.ai_model_profiles SET is_active = true, updated_at = now() WHERE id = $1 AND tenant_id = $2
	`, profileID, tenantID); err != nil {
		return nil, fmt.Errorf("set active profile: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.ai_profile_models SET is_active = false WHERE profile_id = $1 AND is_active = true
	`, profileID); err != nil {
		return nil, fmt.Errorf("clear active model: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.ai_profile_models SET is_active = true WHERE profile_id = $1 AND model_id = $2
	`, profileID, modelID); err != nil {
		return nil, fmt.Errorf("set active model: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit apply: %w", err)
	}
	return s.profileByID(ctx, tenantID, profileID)
}

// insertProfileModels adds model IDs to a profile, ignoring duplicates and
// the first model becoming the default sort order.
func insertProfileModels(ctx context.Context, tx *sql.Tx, profileID string, models []string) error {
	var nextOrder int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sort_order) + 1, 0) FROM public.ai_profile_models WHERE profile_id = $1
	`, profileID).Scan(&nextOrder); err != nil {
		return fmt.Errorf("resolve model sort order: %w", err)
	}
	for _, modelID := range models {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO public.ai_profile_models (id, profile_id, model_id, is_active, sort_order, created_at)
			VALUES ($1, $2, $3, false, $4, now())
			ON CONFLICT (profile_id, model_id) DO NOTHING
		`, newAIID("aim"), profileID, modelID, nextOrder); err != nil {
			return fmt.Errorf("insert profile model: %w", err)
		}
		nextOrder++
	}
	return nil
}

func cleanModels(models []string) []string {
	seen := map[string]struct{}{}
	cleaned := []string{}
	for _, m := range models {
		v := strings.TrimSpace(m)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		cleaned = append(cleaned, v)
	}
	return cleaned
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate key") ||
		strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
