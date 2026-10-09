package model

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPoolMaxEntries = 128
	defaultPoolTTL        = 15 * time.Minute
)

// Config is the raw, user-supplied part of a profile that selects the client:
// strings as stored, normalized by Resolve.
type Config struct {
	ProviderType    string
	APIFormat       string
	ReasoningEffort string
	ReasoningBudget int
}

// Resolve validates the raw values and returns the client options.
func (c Config) Resolve() (Options, bool) {
	kind, ok := NormalizeProviderType(c.ProviderType)
	if !ok {
		return Options{}, false
	}
	format, ok := NormalizeAPIFormat(c.APIFormat)
	if !ok {
		return Options{}, false
	}
	effort, ok := NormalizeReasoningEffort(c.ReasoningEffort)
	if !ok {
		return Options{}, false
	}
	if !ValidReasoningBudget(c.ReasoningBudget) {
		return Options{}, false
	}
	return Options{ProviderType: kind, APIFormat: format, ReasoningEffort: effort, ReasoningBudget: c.ReasoningBudget}, true
}

type poolEntry struct {
	client    Backend
	lastUsed  time.Time
	configKey string
}

// ClientPool manages cached, thread-safe model.Client instances per tenant,
// keeping persistent HTTP connections warm and avoiding client reallocation per chat turn.
type ClientPool struct {
	mu           sync.RWMutex
	entries      map[string]*poolEntry
	httpClient   *http.Client
	maxEntries   int
	ttl          time.Duration
	gatewayToken string
	guards       map[string]Provider
}

func NewClientPool(httpClient *http.Client) *ClientPool {
	if httpClient == nil {
		httpClient = newDefaultHTTPClient()
	}

	return &ClientPool{
		entries:    make(map[string]*poolEntry),
		httpClient: httpClient,
		maxEntries: defaultPoolMaxEntries,
		ttl:        defaultPoolTTL,
		guards:     make(map[string]Provider),
	}
}

// GetProvider returns a pooled client wrapped in a circuit breaker whose
// state follows the tenant/configuration key across requests.
func (p *ClientPool) GetProvider(tenantID string, cfg Config, baseURL, apiKey, modelID string) Provider {
	opts, ok := cfg.Resolve()
	if !ok {
		return nil
	}
	if p == nil {
		return NewCircuitBreakerProvider(NewBackend(opts, baseURL, apiKey, modelID, nil), 3, 30*time.Second)
	}
	client := p.GetClient(tenantID, cfg, baseURL, apiKey, modelID)
	key := tenantID + "\x00" + hashConfig(opts, baseURL, apiKey, modelID)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.evictExpiredGuardsLocked(time.Now())
	if guarded, ok := p.guards[key]; ok {
		return guarded
	}
	guarded := NewCircuitBreakerProvider(client, 3, 30*time.Second)
	p.guards[key] = guarded
	return guarded
}

// SetGatewayToken applies the AI Gateway credential (cf-aig-authorization
// header) to every client the pool creates.
func (p *ClientPool) SetGatewayToken(token string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gatewayToken = strings.TrimSpace(token)
}

func (p *ClientPool) GetClient(tenantID string, cfg Config, baseURL, apiKey, modelID string) Backend {
	opts, ok := cfg.Resolve()
	if !ok {
		return nil
	}
	if p == nil {
		return NewBackend(opts, baseURL, apiKey, modelID, nil)
	}

	configHash := hashConfig(opts, baseURL, apiKey, modelID)

	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.evictExpiredGuardsLocked(now)

	// Double-check under write lock
	if entry, ok := p.entries[tenantID]; ok && entry.configKey == configHash && now.Sub(entry.lastUsed) < p.ttl {
		entry.lastUsed = now
		return entry.client
	}
	// A configuration change must not retain the previous circuit state.
	for key := range p.guards {
		if strings.HasPrefix(key, tenantID+"\x00") {
			delete(p.guards, key)
		}
	}

	// Evict old entries if pool is full
	if len(p.entries) >= p.maxEntries {
		p.evictOldestLocked()
	}

	client := NewBackend(opts, baseURL, apiKey, modelID, p.httpClient)
	if p.gatewayToken != "" {
		client = client.WithGatewayToken(p.gatewayToken)
	}
	p.entries[tenantID] = &poolEntry{
		client:    client,
		lastUsed:  now,
		configKey: configHash,
	}

	return client
}

func (p *ClientPool) Invalidate(tenantID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.entries, tenantID)
	for key := range p.guards {
		if strings.HasPrefix(key, tenantID+"\x00") {
			delete(p.guards, key)
		}
	}
}

func (p *ClientPool) evictOldestLocked() {
	var oldestTenant string
	var oldestTime time.Time

	for tenant, entry := range p.entries {
		if oldestTenant == "" || entry.lastUsed.Before(oldestTime) {
			oldestTenant = tenant
			oldestTime = entry.lastUsed
		}
	}

	if oldestTenant != "" {
		delete(p.entries, oldestTenant)
		for key := range p.guards {
			if strings.HasPrefix(key, oldestTenant+"\x00") {
				delete(p.guards, key)
			}
		}
	}
}

// evictExpiredGuardsLocked bounds circuit-breaker state even when a tenant's
// client entry is replaced by a new model configuration. Guards are cache
// state, so removing them only affects the next request's warm-up period.
func (p *ClientPool) evictExpiredGuardsLocked(now time.Time) {
	for key := range p.guards {
		separator := strings.IndexByte(key, 0)
		if separator < 0 {
			delete(p.guards, key)
			continue
		}
		tenantID := key[:separator]
		entry, ok := p.entries[tenantID]
		if !ok || now.Sub(entry.lastUsed) >= p.ttl {
			delete(p.guards, key)
		}
	}
}

func hashConfig(opts Options, baseURL, apiKey, modelID string) string {
	h := sha256.New()
	for _, part := range []string{string(opts.ProviderType), string(opts.APIFormat), string(opts.ReasoningEffort), strconv.Itoa(opts.ReasoningBudget), baseURL, apiKey, modelID} {
		h.Write([]byte(part))
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
