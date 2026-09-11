package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type profileModelDTO struct {
	ID       string `json:"id"`
	ModelID  string `json:"modelId"`
	Label    string `json:"label,omitempty"`
	IsActive bool   `json:"isActive"`
}

type profileDTO struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	BaseURL   string            `json:"baseUrl"`
	APIKey    string            `json:"apiKey"`
	HasAPIKey bool              `json:"hasApiKey"`
	IsActive  bool              `json:"isActive"`
	Models    []profileModelDTO `json:"models"`
}

type profileUpsertRequest struct {
	Name    string   `json:"name"`
	BaseURL string   `json:"baseUrl"`
	APIKey  string   `json:"apiKey"`
	Models  []string `json:"models"`
}

type profileModelsRequest struct {
	Models []string `json:"models"`
}

type applyModelRequest struct {
	ModelID string `json:"modelId"`
}

func toProfileDTO(p repository.AIModelProfile) profileDTO {
	dto := profileDTO{
		ID:        p.ID,
		Name:      p.Name,
		BaseURL:   p.BaseURL,
		APIKey:    maskAPIKey(p.APIKey),
		HasAPIKey: strings.TrimSpace(p.APIKey) != "",
		IsActive:  p.IsActive,
		Models:    []profileModelDTO{},
	}
	for _, m := range p.Models {
		dto.Models = append(dto.Models, profileModelDTO{
			ID:       m.ID,
			ModelID:  m.ModelID,
			Label:    m.Label,
			IsActive: m.IsActive,
		})
	}
	return dto
}

func writeResultEnvelope(w http.ResponseWriter, result any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"errors":   []any{},
		"messages": []string{},
		"result":   result,
	})
}

// handleProfiles serves GET/POST /api/ai/settings/profiles.
func handleProfiles(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	profilesStore, ok := store.(repository.ModelProfileStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	switch r.Method {
	case http.MethodGet:
		profiles, err := profilesStore.ListProfiles(r.Context(), scope.TenantID)
		if err != nil {
			problem(w, http.StatusInternalServerError, "ai.profiles_fetch_failed")
			return
		}
		out := make([]profileDTO, 0, len(profiles))
		for _, p := range profiles {
			out = append(out, toProfileDTO(p))
		}
		writeResultEnvelope(w, map[string]any{"profiles": out})
	case http.MethodPost:
		var req profileUpsertRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req); err != nil {
			problem(w, http.StatusBadRequest, "ai.invalid_request_body")
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		req.APIKey = strings.TrimSpace(req.APIKey)
		if req.Name == "" || strings.TrimSpace(req.APIKey) == "" {
			problem(w, http.StatusBadRequest, "ai.missing_required_fields")
			return
		}
		if !validateProfileURL(w, req.BaseURL, options) {
			return
		}
		profile, err := profilesStore.CreateProfile(r.Context(), scope.TenantID, req.Name, req.BaseURL, req.APIKey, req.Models)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		writeResultEnvelope(w, map[string]any{"profile": toProfileDTO(*profile)})
	default:
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
	}
}

// handleProfileByID serves /api/ai/settings/profiles/{id}[/...].
func handleProfileByID(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	profilesStore, ok := store.(repository.ModelProfileStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/settings/profiles/"), "/")
	parts := strings.Split(rest, "/")
	profileID := parts[0]
	if profileID == "" {
		problem(w, http.StatusNotFound, "ai.profile_not_found")
		return
	}

	// /profiles/{id}/models
	if len(parts) == 2 && parts[1] == "models" {
		handleProfileModels(w, r, profilesStore, scope.TenantID, profileID)
		return
	}
	// /profiles/{id}/models/{modelId}
	if len(parts) == 3 && parts[1] == "models" {
		if r.Method != http.MethodDelete {
			problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
			return
		}
		modelID := parts[2]
		if err := profilesStore.DeleteProfileModel(r.Context(), scope.TenantID, profileID, modelID); err != nil {
			writeProfileError(w, err)
			return
		}
		writeResultEnvelope(w, map[string]any{"deleted": true})
		return
	}
	// /profiles/{id}/apply
	if len(parts) == 2 && parts[1] == "apply" {
		if r.Method != http.MethodPost {
			problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
			return
		}
		var req applyModelRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
			problem(w, http.StatusBadRequest, "ai.invalid_request_body")
			return
		}
		profile, err := profilesStore.ApplyProfileModel(r.Context(), scope.TenantID, profileID, req.ModelID)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		writeResultEnvelope(w, map[string]any{"profile": toProfileDTO(*profile)})
		return
	}
	// /profiles/{id}/test
	if len(parts) == 2 && parts[1] == "test" {
		handleProfileTest(w, r, profilesStore, scope, options, profileID)
		return
	}
	if len(parts) != 1 {
		problem(w, http.StatusNotFound, "ai.profile_endpoint_not_found")
		return
	}

	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		var req profileUpsertRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req); err != nil {
			problem(w, http.StatusBadRequest, "ai.invalid_request_body")
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		if req.BaseURL != "" && !validateProfileURL(w, req.BaseURL, options) {
			return
		}
		if isMaskedSecret(req.APIKey) {
			req.APIKey = ""
		}
		profile, err := profilesStore.UpdateProfile(r.Context(), scope.TenantID, profileID, req.Name, req.BaseURL, req.APIKey)
		if err != nil {
			writeProfileError(w, err)
			return
		}
		writeResultEnvelope(w, map[string]any{"profile": toProfileDTO(*profile)})
	case http.MethodDelete:
		if err := profilesStore.DeleteProfile(r.Context(), scope.TenantID, profileID); err != nil {
			writeProfileError(w, err)
			return
		}
		writeResultEnvelope(w, map[string]any{"deleted": true})
	default:
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
	}
}

func handleProfileModels(w http.ResponseWriter, r *http.Request, store repository.ModelProfileStore, tenantID, profileID string) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	var req profileModelsRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}
	profile, err := store.AddProfileModels(r.Context(), tenantID, profileID, req.Models)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeResultEnvelope(w, map[string]any{"profile": toProfileDTO(*profile)})
}

// handleProfileTest probes a stored profile + model using the saved API key,
// so the UI can verify a model without retyping the secret.
func handleProfileTest(w http.ResponseWriter, r *http.Request, store repository.ModelProfileStore, scope tools.Context, options RouterOptions, profileID string) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	var req applyModelRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}
	profile, err := profileByID(r.Context(), store, scope.TenantID, profileID)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	if err := validateProviderURL(profile.BaseURL, options.AllowLocalModelURLs); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "result": testConnectionResponse{Success: false, Error: err.Error()}})
		return
	}
	if !baseURLAllowed(options.ModelBaseURLAllowlist, profile.BaseURL) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "result": testConnectionResponse{Success: false, Error: "Base URL không nằm trong danh sách được phép của hệ thống"}})
		return
	}
	modelID := strings.TrimSpace(req.ModelID)
	if modelID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "result": testConnectionResponse{Success: false, Error: "Model ID không được để trống"}})
		return
	}

	client := model.NewClient(profile.BaseURL, profile.APIKey, modelID, nil)
	if options.ModelGatewayToken != "" {
		client.WithGatewayToken(options.ModelGatewayToken)
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := client.ChatProbe(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "result": testConnectionResponse{Success: false, LatencyMs: time.Since(start).Milliseconds(), Error: err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "result": testConnectionResponse{
		Success: true, LatencyMs: time.Since(start).Milliseconds(), ModelID: modelID, Message: "Kết nối thành công tới " + modelID,
	}})
}

// profileByID fetches a single profile via the list store (repository exposes
// no single-get on the interface) and returns ErrModelProfileNotFound if absent.
func profileByID(ctx context.Context, store repository.ModelProfileStore, tenantID, profileID string) (*repository.AIModelProfile, error) {
	profiles, err := store.ListProfiles(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for i := range profiles {
		if profiles[i].ID == profileID {
			return &profiles[i], nil
		}
	}
	return nil, repository.ErrModelProfileNotFound
}

func validateProfileURL(w http.ResponseWriter, rawURL string, options RouterOptions) bool {
	if err := validateProviderURL(rawURL, options.AllowLocalModelURLs); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_base_url")
		return false
	}
	if !baseURLAllowed(options.ModelBaseURLAllowlist, rawURL) {
		problem(w, http.StatusBadRequest, "ai.base_url_not_allowed")
		return false
	}
	return true
}

func writeProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrModelProfileNotFound):
		problem(w, http.StatusNotFound, "ai.profile_not_found")
	case errors.Is(err, repository.ErrModelProfileNameTaken):
		problem(w, http.StatusConflict, "ai.profile_name_taken")
	case errors.Is(err, repository.ErrModelNotFound):
		problem(w, http.StatusNotFound, "ai.model_not_found")
	default:
		problem(w, http.StatusBadRequest, "ai.profile_operation_failed")
	}
}
