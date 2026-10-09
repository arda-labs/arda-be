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
	// APIFormat is this model's override; empty means it inherits the profile.
	APIFormat string `json:"apiFormat,omitempty"`
	// SuggestedAPIFormat is a hint derived from the model ID, never applied
	// automatically.
	SuggestedAPIFormat string `json:"suggestedApiFormat"`
}

type profileDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	BaseURL      string `json:"baseUrl"`
	ProviderType string `json:"providerType"`
	// APIFormat: chat_completions | anthropic_messages | openai_responses.
	// ReasoningEffort: "" (provider default) | low | medium | high.
	APIFormat       string            `json:"apiFormat"`
	ReasoningEffort string            `json:"reasoningEffort"`
	ReasoningBudget int               `json:"reasoningBudgetTokens"`
	APIKey          string            `json:"apiKey"`
	HasAPIKey       bool              `json:"hasApiKey"`
	IsActive        bool              `json:"isActive"`
	Models          []profileModelDTO `json:"models"`
}

type profileUpsertRequest struct {
	Name         string `json:"name"`
	BaseURL      string `json:"baseUrl"`
	ProviderType string `json:"providerType"`
	APIFormat    string `json:"apiFormat"`
	// ReasoningEffort is a pointer so an update can tell "not sent" (keep)
	// from "" (reset to the provider default).
	ReasoningEffort *string `json:"reasoningEffort"`
	// ReasoningBudget is an explicit thinking-token budget (0 = use the effort).
	ReasoningBudget *int     `json:"reasoningBudgetTokens"`
	APIKey          string   `json:"apiKey"`
	Models          []string `json:"models"`
}

type profileModelsRequest struct {
	Models []string `json:"models"`
	// Formats optionally sets a per-model API format (modelId -> format) for
	// the models being added.
	Formats map[string]string `json:"formats"`
}

type modelFormatRequest struct {
	// APIFormat "" clears the override so the model inherits the profile.
	APIFormat string `json:"apiFormat"`
}

type applyModelRequest struct {
	ModelID string `json:"modelId"`
}

func toProfileDTO(p repository.AIModelProfile) profileDTO {
	dto := profileDTO{
		ID:              p.ID,
		Name:            p.Name,
		BaseURL:         p.BaseURL,
		ProviderType:    p.ProviderType,
		APIFormat:       normalizedFormatOrDefault(p.APIFormat),
		ReasoningEffort: p.ReasoningEffort,
		ReasoningBudget: p.ReasoningBudget,
		APIKey:          maskAPIKey(p.APIKey),
		HasAPIKey:       strings.TrimSpace(p.APIKey) != "",
		IsActive:        p.IsActive,
		Models:          []profileModelDTO{},
	}
	for _, m := range p.Models {
		dto.Models = append(dto.Models, profileModelDTO{
			ID:                 m.ID,
			ModelID:            m.ModelID,
			Label:              m.Label,
			IsActive:           m.IsActive,
			APIFormat:          m.APIFormat,
			SuggestedAPIFormat: string(model.SuggestAPIFormat(m.ModelID)),
		})
	}
	return dto
}

func normalizedFormatOrDefault(raw string) string {
	if format, ok := model.NormalizeAPIFormat(raw); ok {
		return string(format)
	}
	return string(model.FormatChatCompletions)
}

// parseProfileOptions validates the API format and reasoning effort of a
// create/update request. An empty format means "default" on create and "keep"
// on update; the store resolves which.
func parseProfileOptions(w http.ResponseWriter, req profileUpsertRequest) (repository.ProfileOptions, bool) {
	opts := repository.ProfileOptions{}
	if strings.TrimSpace(req.APIFormat) != "" {
		format, ok := model.NormalizeAPIFormat(req.APIFormat)
		if !ok {
			problem(w, http.StatusBadRequest, "ai.unsupported_api_format")
			return opts, false
		}
		opts.APIFormat = string(format)
	}
	if req.ReasoningEffort != nil {
		effort, ok := model.NormalizeReasoningEffort(*req.ReasoningEffort)
		if !ok {
			problem(w, http.StatusBadRequest, "ai.unsupported_reasoning_effort")
			return opts, false
		}
		value := string(effort)
		opts.ReasoningEffort = &value
	}
	if req.ReasoningBudget != nil {
		if !model.ValidReasoningBudget(*req.ReasoningBudget) {
			problem(w, http.StatusBadRequest, "ai.unsupported_reasoning_budget")
			return opts, false
		}
		budget := *req.ReasoningBudget
		opts.ReasoningBudget = &budget
	}
	return opts, true
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
		providerType, ok := model.NormalizeProviderType(req.ProviderType)
		if !ok {
			problem(w, http.StatusBadRequest, "ai.unsupported_provider_type")
			return
		}
		if req.Name == "" || strings.TrimSpace(req.APIKey) == "" {
			problem(w, http.StatusBadRequest, "ai.missing_required_fields")
			return
		}
		if !validateProfileURL(w, req.BaseURL, options) {
			return
		}
		if !validateChatModels(w, req.Models) {
			return
		}
		profileOptions, ok := parseProfileOptions(w, req)
		if !ok {
			return
		}
		profile, err := profilesStore.CreateProfile(r.Context(), scope.TenantID, req.Name, string(providerType), req.BaseURL, req.APIKey, req.Models, profileOptions)
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
	// /profiles/{id}/available-models
	if len(parts) == 2 && parts[1] == "available-models" {
		handleProfileAvailableModels(w, r, profilesStore, scope, options, profileID)
		return
	}
	// /profiles/{id}/models/{modelId}
	if len(parts) == 3 && parts[1] == "models" {
		if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			handleProfileModelFormat(w, r, profilesStore, scope.TenantID, profileID, parts[2])
			return
		}
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
		if !validateChatModels(w, []string{req.ModelID}) {
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
		providerType := model.ProviderType("")
		if req.ProviderType != "" {
			var ok bool
			providerType, ok = model.NormalizeProviderType(req.ProviderType)
			if !ok {
				problem(w, http.StatusBadRequest, "ai.unsupported_provider_type")
				return
			}
		}
		if req.ProviderType != "" && providerType == "" {
			problem(w, http.StatusBadRequest, "ai.unsupported_provider_type")
			return
		}
		if req.BaseURL != "" && !validateProfileURL(w, req.BaseURL, options) {
			return
		}
		if isMaskedSecret(req.APIKey) {
			req.APIKey = ""
		}
		profileOptions, ok := parseProfileOptions(w, req)
		if !ok {
			return
		}
		profile, err := profilesStore.UpdateProfile(r.Context(), scope.TenantID, profileID, req.Name, string(providerType), req.BaseURL, req.APIKey, profileOptions)
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
	if !validateChatModels(w, req.Models) {
		return
	}
	formats := map[string]model.APIFormat{}
	for modelID, raw := range req.Formats {
		format, ok := model.NormalizeAPIFormat(raw)
		if !ok {
			problem(w, http.StatusBadRequest, "ai.unsupported_api_format")
			return
		}
		formats[strings.TrimSpace(modelID)] = format
	}
	profile, err := store.AddProfileModels(r.Context(), tenantID, profileID, req.Models)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	for modelID, format := range formats {
		if profile, err = store.SetProfileModelFormat(r.Context(), tenantID, profileID, modelID, string(format)); err != nil {
			writeProfileError(w, err)
			return
		}
	}
	writeResultEnvelope(w, map[string]any{"profile": toProfileDTO(*profile)})
}

// handleProfileModelFormat serves PATCH /profiles/{id}/models/{modelId}.
func handleProfileModelFormat(w http.ResponseWriter, r *http.Request, store repository.ModelProfileStore, tenantID, profileID, modelID string) {
	var req modelFormatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}
	format := ""
	if strings.TrimSpace(req.APIFormat) != "" {
		normalized, ok := model.NormalizeAPIFormat(req.APIFormat)
		if !ok {
			problem(w, http.StatusBadRequest, "ai.unsupported_api_format")
			return
		}
		format = string(normalized)
	}
	profile, err := store.SetProfileModelFormat(r.Context(), tenantID, profileID, modelID, format)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeResultEnvelope(w, map[string]any{"profile": toProfileDTO(*profile)})
}

type availableModelDTO struct {
	ModelID            string `json:"modelId"`
	SuggestedAPIFormat string `json:"suggestedApiFormat"`
	// Added reports whether the profile already holds the model.
	Added bool `json:"added"`
}

// handleProfileAvailableModels lists the models the saved profile's endpoint
// advertises (GET {base}/models) so the UI can add them without retyping IDs.
// It applies the same egress and allowlist checks as the connection test.
func handleProfileAvailableModels(w http.ResponseWriter, r *http.Request, store repository.ModelProfileStore, scope tools.Context, options RouterOptions, profileID string) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	profile, err := profileByID(r.Context(), store, scope.TenantID, profileID)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	respond := func(models []availableModelDTO, message string) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "result": map[string]any{
			"models": models, "error": message,
		}})
	}
	if err := validateProviderURL(profile.BaseURL, options.AllowLocalModelURLs); err != nil {
		respond(nil, err.Error())
		return
	}
	if !baseURLAllowed(options.ModelBaseURLAllowlist, profile.BaseURL) {
		respond(nil, "Base URL không nằm trong danh sách được phép của hệ thống")
		return
	}
	clientOptions, ok := model.Config{
		ProviderType: profile.ProviderType, APIFormat: profile.APIFormat,
	}.Resolve()
	if !ok {
		respond(nil, "Provider type hoặc API format không được hỗ trợ")
		return
	}
	// ListModels does not call a model, so any non-decision placeholder ID works.
	client := model.NewBackend(clientOptions, profile.BaseURL, profile.APIKey, "model-list", nil)
	if options.ModelGatewayToken != "" {
		client = client.WithGatewayToken(options.ModelGatewayToken)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ids, err := client.ListModels(ctx)
	if err != nil {
		respond(nil, err.Error())
		return
	}
	held := map[string]bool{}
	for _, m := range profile.Models {
		held[m.ModelID] = true
	}
	models := make([]availableModelDTO, 0, len(ids))
	for _, id := range ids {
		models = append(models, availableModelDTO{ModelID: id, SuggestedAPIFormat: string(model.SuggestAPIFormat(id)), Added: held[id]})
	}
	respond(models, "")
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
	if !validateChatModels(w, []string{modelID}) {
		return
	}
	if modelID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "result": testConnectionResponse{Success: false, Error: "Model ID không được để trống"}})
		return
	}

	apiFormat := profile.APIFormat
	for _, m := range profile.Models {
		if m.ModelID == modelID && m.APIFormat != "" {
			apiFormat = m.APIFormat // per-model override
		}
	}
	clientOptions, ok := model.Config{
		ProviderType: profile.ProviderType, APIFormat: apiFormat,
		ReasoningEffort: profile.ReasoningEffort, ReasoningBudget: profile.ReasoningBudget,
	}.Resolve()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "result": testConnectionResponse{Success: false, Error: "Provider type hoặc API format không được hỗ trợ"}})
		return
	}
	client := model.NewBackend(clientOptions, profile.BaseURL, profile.APIKey, modelID, nil)
	if options.ModelGatewayToken != "" {
		client = client.WithGatewayToken(options.ModelGatewayToken)
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(model.WithSessionID(r.Context(), model.StableSessionID(options.ModelSessionSecret, scope.TenantID, "profile-test:"+profile.ID+":"+modelID)), 10*time.Second)
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
