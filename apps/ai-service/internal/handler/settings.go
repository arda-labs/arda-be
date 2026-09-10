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
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// settingsDTO is the tenant-owned active model configuration. The deployment
// never sets model credentials; tenants configure them in the AI Settings UI.
type settingsDTO struct {
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey"`
	ModelID   string `json:"modelId"`
	HasAPIKey bool   `json:"hasApiKey"`
}

type testConnectionRequest struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	ModelID string `json:"modelId"`
}

type testConnectionResponse struct {
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latencyMs"`
	ModelID   string `json:"modelId,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

func handleGetSettings(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}

	scope, ok := identityScope(w, r)
	if !ok {
		return
	}

	result := settingsDTO{}
	settingsStore, ok := store.(repository.TenantSettingsStore)
	if !ok {
		writeSettingsEnvelope(w, result)
		return
	}

	settings, err := settingsStore.GetTenantSettings(r.Context(), scope.TenantID)
	switch {
	case err == nil && settings != nil:
		result = settingsDTO{
			BaseURL:   settings.BaseURL,
			APIKey:    maskAPIKey(settings.APIKey),
			ModelID:   settings.ModelID,
			HasAPIKey: strings.TrimSpace(settings.APIKey) != "",
		}
	case errors.Is(err, repository.ErrTenantSettingsNotFound):
		// Not configured yet: the dialog starts empty.
	default:
		problem(w, http.StatusInternalServerError, "ai.settings_fetch_failed")
		return
	}

	writeSettingsEnvelope(w, result)
}

func writeSettingsEnvelope(w http.ResponseWriter, result settingsDTO) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"errors":   []any{},
		"messages": []string{},
		"result":   result,
	})
}

func handleUpdateSettings(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}

	scope, ok := identityScope(w, r)
	if !ok {
		return
	}

	settingsStore, ok := store.(repository.TenantSettingsStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	var req settingsDTO
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}

	req.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	req.ModelID = strings.TrimSpace(req.ModelID)

	if err := validateProviderURL(req.BaseURL, options.AllowLocalModelURLs); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_base_url")
		return
	}
	if !baseURLAllowed(options.ModelBaseURLAllowlist, req.BaseURL) {
		problem(w, http.StatusBadRequest, "ai.base_url_not_allowed")
		return
	}
	if req.ModelID == "" {
		problem(w, http.StatusBadRequest, "ai.missing_required_fields")
		return
	}

	apiKey, ok := resolveAPIKey(r.Context(), settingsStore, scope.TenantID, req.APIKey)
	if !ok {
		problem(w, http.StatusBadRequest, "ai.missing_required_fields")
		return
	}

	err := settingsStore.UpsertTenantSettings(r.Context(), repository.TenantSettings{
		TenantID: scope.TenantID,
		BaseURL:  req.BaseURL,
		APIKey:   apiKey,
		ModelID:  req.ModelID,
	})
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.settings_save_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"errors":   []any{},
		"messages": []string{"AI settings saved successfully"},
		"result":   map[string]any{"saved": true},
	})
}

func handleTestConnection(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}

	scope, ok := identityScope(w, r)
	if !ok {
		return
	}

	var req testConnectionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}

	baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	modelID := strings.TrimSpace(req.ModelID)

	writeTestResult := func(res testConnectionResponse) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"errors":  []any{},
			"result":  res,
		})
	}

	if err := validateProviderURL(baseURL, options.AllowLocalModelURLs); err != nil {
		writeTestResult(testConnectionResponse{Success: false, Error: err.Error()})
		return
	}
	if !baseURLAllowed(options.ModelBaseURLAllowlist, baseURL) {
		writeTestResult(testConnectionResponse{Success: false, Error: "Base URL không nằm trong danh sách được phép của hệ thống"})
		return
	}
	if modelID == "" {
		writeTestResult(testConnectionResponse{Success: false, Error: "Model ID không được để trống"})
		return
	}

	apiKey := strings.TrimSpace(req.APIKey)
	if masked := isMaskedSecret(apiKey); masked || apiKey == "" {
		if settingsStore, hasSettings := store.(repository.TenantSettingsStore); hasSettings {
			if existing, err := settingsStore.GetTenantSettings(r.Context(), scope.TenantID); err == nil && existing != nil {
				apiKey = existing.APIKey
			}
		}
	}

	client := model.NewClient(baseURL, apiKey, modelID, nil)
	if options.ModelGatewayToken != "" {
		client.WithGatewayToken(options.ModelGatewayToken)
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := client.ChatProbe(ctx); err != nil {
		writeTestResult(testConnectionResponse{
			Success:   false,
			LatencyMs: time.Since(start).Milliseconds(),
			Error:     err.Error(),
		})
		return
	}

	latency := time.Since(start).Milliseconds()
	writeTestResult(testConnectionResponse{
		Success:   true,
		LatencyMs: latency,
		ModelID:   modelID,
		Message:   "Kết nối thành công tới " + modelID,
	})
}

// resolveAPIKey returns the key to persist. A masked or empty value means
// "keep the key currently in effect" so the dialog never forces retyping a
// secret it only ever displays masked.
func resolveAPIKey(ctx context.Context, store repository.TenantSettingsStore, tenantID, submitted string) (string, bool) {
	apiKey := strings.TrimSpace(submitted)
	if apiKey != "" && !isMaskedSecret(apiKey) {
		return apiKey, true
	}
	if existing, err := store.GetTenantSettings(ctx, tenantID); err == nil && existing != nil {
		if strings.TrimSpace(existing.APIKey) != "" {
			return existing.APIKey, true
		}
	}
	return "", false
}

func validateProviderURL(rawURL string, allowLocal bool) error {
	return ardahttp.ValidateEgressURL(rawURL, allowLocal)
}

// baseURLAllowed reports whether rawURL matches the gateway allowlist.
// Entries are URL prefixes with path-boundary semantics, so
// "https://gateway.example/v1/acct/gw" permits "/v1/acct/gw/openai" but not
// "/v1/acct/other". An empty allowlist disables enforcement; only
// ValidateEgressURL applies.
func baseURLAllowed(allowlist []string, rawURL string) bool {
	if len(allowlist) == 0 {
		return true
	}
	candidate := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if candidate == "" {
		return false
	}
	for _, entry := range allowlist {
		prefix := strings.TrimRight(strings.TrimSpace(entry), "/")
		if prefix == "" {
			continue
		}
		if candidate == prefix || strings.HasPrefix(candidate, prefix+"/") {
			return true
		}
	}
	return false
}

func isMaskedSecret(value string) bool {
	return strings.Contains(value, "...")
}

func maskAPIKey(key string) string {
	clean := strings.TrimSpace(key)
	if len(clean) <= 8 {
		if clean == "" {
			return ""
		}
		return "••••••••"
	}
	return clean[:4] + "..." + clean[len(clean)-4:]
}
