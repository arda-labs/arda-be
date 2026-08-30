package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

type settingsDTO struct {
	ProviderType string  `json:"providerType"`
	BaseURL      string  `json:"baseUrl"`
	APIKey       string  `json:"apiKey"`
	ModelID      string  `json:"modelId"`
	Temperature  float32 `json:"temperature"`
	IsActive     bool    `json:"isActive"`
	HasAPIKey    bool    `json:"hasApiKey"`
}

type testConnectionRequest struct {
	ProviderType string `json:"providerType"`
	BaseURL      string `json:"baseUrl"`
	APIKey       string `json:"apiKey"`
	ModelID      string `json:"modelId"`
}

type testConnectionResponse struct {
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latencyMs"`
	ModelID   string `json:"modelId,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

func handleGetSettings(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}

	scope, ok := identityScope(w, r)
	if !ok {
		return
	}

	defaultSettings := settingsDTO{
		ProviderType: "openai",
		BaseURL:      "https://api.openai.com/v1",
		APIKey:       "",
		ModelID:      "gpt-4o-mini",
		Temperature:  0.2,
		IsActive:     true,
		HasAPIKey:    false,
	}

	settingsStore, ok := store.(repository.TenantSettingsStore)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":  true,
			"errors":   []any{},
			"messages": []string{},
			"result":   defaultSettings,
		})
		return
	}

	settings, err := settingsStore.GetTenantSettings(r.Context(), scope.TenantID)
	if err != nil {
		if errors.Is(err, repository.ErrTenantSettingsNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{
				"success":  true,
				"errors":   []any{},
				"messages": []string{},
				"result":   defaultSettings,
			})
			return
		}
		problem(w, http.StatusInternalServerError, "ai.settings_fetch_failed")
		return
	}

	result := settingsDTO{
		ProviderType: settings.ProviderType,
		BaseURL:      settings.BaseURL,
		APIKey:       maskAPIKey(settings.APIKey),
		ModelID:      settings.ModelID,
		Temperature:  settings.Temperature,
		IsActive:     settings.IsActive,
		HasAPIKey:    strings.TrimSpace(settings.APIKey) != "",
	}

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
	req.ProviderType = strings.TrimSpace(req.ProviderType)

	if err := validateProviderURL(req.BaseURL); err != nil {
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
	if strings.TrimSpace(req.APIKey) == "" {
		problem(w, http.StatusBadRequest, "ai.missing_required_fields")
		return
	}
	if req.ProviderType == "" {
		req.ProviderType = "openai"
	}
	if req.Temperature <= 0 || req.Temperature > 2 {
		req.Temperature = 0.2
	}

	err := settingsStore.UpsertTenantSettings(r.Context(), repository.TenantSettings{
		TenantID:     scope.TenantID,
		ProviderType: req.ProviderType,
		BaseURL:      req.BaseURL,
		APIKey:       strings.TrimSpace(req.APIKey),
		ModelID:      req.ModelID,
		Temperature:  req.Temperature,
		IsActive:     true,
	})
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.settings_save_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"errors":   []any{},
		"messages": []string{"AI settings saved successfully"},
		"result": map[string]any{
			"saved": true,
		},
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
	apiKey := strings.TrimSpace(req.APIKey)
	modelID := strings.TrimSpace(req.ModelID)

	if err := validateProviderURL(baseURL); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"errors":  []any{},
			"result": testConnectionResponse{
				Success: false,
				Error:   err.Error(),
			},
		})
		return
	}
	if !baseURLAllowed(options.ModelBaseURLAllowlist, baseURL) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"errors":  []any{},
			"result": testConnectionResponse{
				Success: false,
				Error:   "Base URL không nằm trong danh sách được phép của hệ thống",
			},
		})
		return
	}
	if modelID == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"errors":  []any{},
			"result": testConnectionResponse{
				Success: false,
				Error:   "Model ID không được để trống",
			},
		})
		return
	}

	// If apiKey is masked (e.g. sk-...xxxx), retrieve existing key from DB
	if strings.Contains(apiKey, "...") {
		if settingsStore, ok := store.(repository.TenantSettingsStore); ok {
			if existing, err := settingsStore.GetTenantSettings(r.Context(), scope.TenantID); err == nil && existing != nil {
				apiKey = existing.APIKey
			}
		}
	}

	// Send lightweight probe request (max_tokens: 1)
	start := time.Now()
	testPayload, _ := json.Marshal(map[string]any{
		"model": modelID,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
	})

	testCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(testCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(testPayload))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"errors":  []any{},
			"result": testConnectionResponse{
				Success: false,
				Error:   fmt.Sprintf("Không thể tạo request: %v", err),
			},
		})
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"errors":  []any{},
			"result": testConnectionResponse{
				Success:   false,
				LatencyMs: latency,
				Error:     fmt.Sprintf("Kết nối thất bại: %v", err),
			},
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"errors":  []any{},
			"result": testConnectionResponse{
				Success:   false,
				LatencyMs: latency,
				Error:     fmt.Sprintf("Nhà cung cấp trả về lỗi HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
			},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"errors":  []any{},
		"result": testConnectionResponse{
			Success:   true,
			LatencyMs: latency,
			ModelID:   modelID,
			Message:   fmt.Sprintf("Kết nối thành công tới %s (%dms)", modelID, latency),
		},
	})
}

func validateProviderURL(rawURL string) error {
	return ardahttp.ValidateEgressURL(rawURL, true)
}

// baseURLAllowed reports whether rawURL matches the gateway allowlist.
// Entries are URL prefixes with path-boundary semantics, so
// "https://gateway.example/v1/acct/gw" permits "/v1/acct/gw/openai" but not
// "/v1/acct/other". An empty allowlist disables enforcement (deployment has
// not switched to a gateway yet); only ValidateEgressURL applies.
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
