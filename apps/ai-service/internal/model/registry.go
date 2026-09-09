package model

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type ProviderID string

const (
	ProviderOpenAI    ProviderID = "openai"
	ProviderAnthropic ProviderID = "anthropic"
	ProviderGemini    ProviderID = "gemini"
	ProviderOllama    ProviderID = "ollama"
)

type ProviderMatch struct {
	TenantPlan   []string `yaml:"tenant_plan" json:"tenantPlan,omitempty"`
	RiskLevel    []string `yaml:"risk_level" json:"riskLevel,omitempty"`
	FeatureFlags []string `yaml:"feature_flags" json:"featureFlags,omitempty"`
}

type ProviderConfig struct {
	ID        ProviderID    `yaml:"id" json:"id"`
	Provider  string        `yaml:"provider" json:"provider"`
	ModelID   string        `yaml:"model_id" json:"modelId"`
	BaseURL   string        `yaml:"base_url" json:"baseUrl"`
	APIKeyEnv string        `yaml:"api_key_env" json:"apiKeyEnv"`
	MaxTokens int           `yaml:"max_tokens" json:"maxTokens"`
	TimeoutMs int           `yaml:"timeout_ms" json:"timeoutMs"`
	Priority  int           `yaml:"priority" json:"priority"`
	Match     ProviderMatch `yaml:"match" json:"match"`
}

type RoutingContext struct {
	TenantPlan   string   `json:"tenantPlan,omitempty"`
	RiskLevel    string   `json:"riskLevel,omitempty"`
	FeatureFlags []string `json:"featureFlags,omitempty"`
	RunID        string   `json:"runId,omitempty"`
}

type ProvidersFile struct {
	Providers []ProviderConfig `yaml:"providers"`
}

type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[ProviderID]Provider
	configs   []ProviderConfig
	stopCh    chan struct{}
}

func NewProviderRegistry() *ProviderRegistry {
	r := &ProviderRegistry{
		providers: make(map[ProviderID]Provider),
		configs:   make([]ProviderConfig, 0),
		stopCh:    make(chan struct{}),
	}
	return r
}

func (r *ProviderRegistry) Register(cfg ProviderConfig, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[cfg.ID] = p
	// Replace existing config if ID matches, else append
	found := false
	for i, c := range r.configs {
		if c.ID == cfg.ID {
			r.configs[i] = cfg
			found = true
			break
		}
	}
	if !found {
		r.configs = append(r.configs, cfg)
	}
	// Sort by Priority ascending (lower = higher priority)
	sort.SliceStable(r.configs, func(i, j int) bool {
		return r.configs[i].Priority < r.configs[j].Priority
	})
}

func (r *ProviderRegistry) Select(ctx RoutingContext) Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Only a provider that explicitly matches the routing context and is
	// healthy may be selected. There is deliberately no last-resort
	// fallback: callers keep their own tenant-configured primary when no
	// entry matches, and returning a known-unhealthy provider (or one
	// without a base URL) would break every run routed through the
	// registry.
	for _, cfg := range r.configs {
		if r.matchesContext(cfg, ctx) && r.isHealthy(cfg.ID) {
			if p, ok := r.providers[cfg.ID]; ok {
				return p
			}
		}
	}
	return nil
}

func (r *ProviderRegistry) matchesContext(cfg ProviderConfig, ctx RoutingContext) bool {
	if len(cfg.Match.TenantPlan) > 0 {
		matched := false
		for _, plan := range cfg.Match.TenantPlan {
			if strings.EqualFold(plan, ctx.TenantPlan) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(cfg.Match.RiskLevel) > 0 {
		matched := false
		for _, risk := range cfg.Match.RiskLevel {
			if strings.EqualFold(risk, ctx.RiskLevel) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(cfg.Match.FeatureFlags) > 0 {
		matched := false
		for _, requiredFlag := range cfg.Match.FeatureFlags {
			for _, activeFlag := range ctx.FeatureFlags {
				if strings.EqualFold(requiredFlag, activeFlag) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func (r *ProviderRegistry) isHealthy(id ProviderID) bool {
	p, ok := r.providers[id]
	if !ok || p == nil {
		return false
	}
	if cb, ok := p.(*CircuitBreakerProvider); ok {
		cb.mu.Lock()
		open := time.Now().Before(cb.openUntil)
		cb.mu.Unlock()
		return !open
	}
	return true
}

// StartActiveHealthProbing runs background probes every 30 seconds for providers
// with open circuit breakers to detect recovery.
func (r *ProviderRegistry) StartActiveHealthProbing(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.probeOpenProviders(ctx)
		}
	}
}

func (r *ProviderRegistry) Close() {
	select {
	case <-r.stopCh:
	default:
		close(r.stopCh)
	}
}

func (r *ProviderRegistry) probeOpenProviders(ctx context.Context) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.providers {
		cb, ok := p.(*CircuitBreakerProvider)
		if !ok {
			continue
		}
		cb.mu.Lock()
		isOpen := time.Now().Before(cb.openUntil)
		cb.mu.Unlock()

		if isOpen {
			probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := cb.Probe(probeCtx)
			cancel()
			if err == nil {
				cb.mu.Lock()
				cb.failures = 0
				cb.openUntil = time.Time{}
				cb.mu.Unlock()
			}
		}
	}
}

// LoadProvidersFromYAML parses a providers configuration YAML file and populates
// the registry with CircuitBreakerProvider wrappers around Client instances.
func LoadProvidersFromYAML(filePath string) (*ProviderRegistry, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read providers file %q: %w", filePath, err)
	}

	var parsed ProvidersFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse providers yaml: %w", err)
	}

	registry := NewProviderRegistry()
	for _, cfg := range parsed.Providers {
		// Entries without a base URL can never serve traffic: NewClient("")
		// fails Validate on every call. Skip them instead of letting routing
		// pick a dead provider.
		if strings.TrimSpace(cfg.BaseURL) == "" {
			continue
		}
		apiKey := ""
		if cfg.APIKeyEnv != "" {
			apiKey = os.Getenv(cfg.APIKeyEnv)
		}
		timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		client := NewClient(cfg.BaseURL, apiKey, cfg.ModelID, nil)
		provider := NewCircuitBreakerProvider(client, 5, 60*time.Second)
		registry.Register(cfg, provider)
	}
	return registry, nil
}
