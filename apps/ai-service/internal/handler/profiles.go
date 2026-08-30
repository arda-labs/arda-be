package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type profileDTO struct {
	ID           string  `json:"id,omitempty"`
	Name         string  `json:"name"`
	ProviderType string  `json:"providerType"`
	BaseURL      string  `json:"baseUrl"`
	APIKey       string  `json:"apiKey"`
	ModelID      string  `json:"modelId"`
	Temperature  float32 `json:"temperature"`
	IsActive     bool    `json:"isActive"`
}

func profileDTOResponse(profiles []repository.TenantSettingProfile) map[string]any {
	items := make([]profileDTO, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, profileDTO{
			ID:           profile.ID,
			Name:         profile.Name,
			ProviderType: profile.ProviderType,
			BaseURL:      profile.BaseURL,
			APIKey:       maskAPIKey(profile.APIKey),
			ModelID:      profile.ModelID,
			Temperature:  profile.Temperature,
			IsActive:     profile.IsActive,
		})
	}
	return map[string]any{
		"success":  true,
		"errors":   []any{},
		"messages": []string{},
		"result":   items,
	}
}

func handleListProfiles(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	profileStore, hasProfiles := store.(repository.ProfileStore)
	if !hasProfiles {
		writeJSON(w, http.StatusOK, profileDTOResponse(nil))
		return
	}
	profiles, err := profileStore.ListProfiles(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.profiles_fetch_failed")
		return
	}
	writeJSON(w, http.StatusOK, profileDTOResponse(profiles))
}

func handleCreateProfile(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	profileStore, hasProfiles := store.(repository.ProfileStore)
	if !hasProfiles {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	var req profileDTO
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}
	req.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if err := validateProviderURL(req.BaseURL); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_base_url")
		return
	}
	if !baseURLAllowed(options.ModelBaseURLAllowlist, req.BaseURL) {
		problem(w, http.StatusBadRequest, "ai.base_url_not_allowed")
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.ModelID) == "" {
		problem(w, http.StatusBadRequest, "ai.missing_required_fields")
		return
	}
	// A masked or empty key means "reuse the key currently in effect" — the
	// dialog only shows masked values and should not force retyping secrets.
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" || strings.Contains(apiKey, "...") {
		if settingsStore, hasSettings := store.(repository.TenantSettingsStore); hasSettings {
			if existing, err := settingsStore.GetTenantSettings(r.Context(), scope.TenantID); err == nil && existing != nil {
				apiKey = existing.APIKey
			}
		}
	}
	if apiKey == "" {
		problem(w, http.StatusBadRequest, "ai.missing_required_fields")
		return
	}

	profile, err := profileStore.CreateProfile(r.Context(), repository.TenantSettingProfile{
		TenantID:     scope.TenantID,
		Name:         req.Name,
		ProviderType: strings.TrimSpace(req.ProviderType),
		BaseURL:      req.BaseURL,
		APIKey:       apiKey,
		ModelID:      strings.TrimSpace(req.ModelID),
		Temperature:  req.Temperature,
		IsActive:     req.IsActive,
	})
	if err != nil {
		if errors.Is(err, repository.ErrProfileNameExists) {
			problem(w, http.StatusConflict, "ai.profile_name_exists")
			return
		}
		problem(w, http.StatusInternalServerError, "ai.profile_save_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"errors":   []any{},
		"messages": []string{"Profile saved"},
		"result": map[string]any{
			"id": profile.ID,
		},
	})
}

func handleProfileAction(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	profileStore, hasProfiles := store.(repository.ProfileStore)
	if !hasProfiles {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	// Path: /api/ai/settings/profiles/{id}[/activate]
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/settings/profiles/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" || len(parts[0]) > 128 {
		problem(w, http.StatusNotFound, "ai.profile_not_found")
		return
	}
	profileID := parts[0]

	switch {
	case r.Method == http.MethodDelete && len(parts) == 1:
		if err := profileStore.DeleteProfile(r.Context(), scope.TenantID, profileID); err != nil {
			if errors.Is(err, repository.ErrProfileNotFound) {
				problem(w, http.StatusNotFound, "ai.profile_not_found")
				return
			}
			problem(w, http.StatusInternalServerError, "ai.profile_delete_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":  true,
			"errors":   []any{},
			"messages": []string{"Profile deleted"},
			"result":   map[string]any{"deleted": true},
		})
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "activate":
		profile, err := profileStore.ActivateProfile(r.Context(), scope.TenantID, profileID)
		if err != nil {
			if errors.Is(err, repository.ErrProfileNotFound) {
				problem(w, http.StatusNotFound, "ai.profile_not_found")
				return
			}
			problem(w, http.StatusInternalServerError, "ai.profile_activate_failed")
			return
		}
		_ = options // allowlist enforced at create-time; kept for symmetry
		writeJSON(w, http.StatusOK, map[string]any{
			"success":  true,
			"errors":   []any{},
			"messages": []string{"Profile activated"},
			"result": map[string]any{
				"id":       profile.ID,
				"name":     profile.Name,
				"baseUrl":  profile.BaseURL,
				"modelId":  profile.ModelID,
				"isActive": profile.IsActive,
			},
		})
	default:
		problem(w, http.StatusNotFound, "ai.profile_not_found")
	}
}
