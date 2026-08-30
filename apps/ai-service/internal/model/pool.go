package model

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultPoolMaxEntries = 128
	defaultPoolTTL        = 15 * time.Minute
)

type poolEntry struct {
	client    *Client
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
}

func NewClientPool(httpClient *http.Client) *ClientPool {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	}

	return &ClientPool{
		entries:    make(map[string]*poolEntry),
		httpClient: httpClient,
		maxEntries: defaultPoolMaxEntries,
		ttl:        defaultPoolTTL,
	}
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

func (p *ClientPool) GetClient(tenantID, baseURL, apiKey, modelID string) *Client {
	if p == nil {
		return NewClient(baseURL, apiKey, modelID, nil)
	}

	configHash := hashConfig(baseURL, apiKey, modelID)

	p.mu.RLock()
	entry, ok := p.entries[tenantID]
	if ok && entry.configKey == configHash && time.Since(entry.lastUsed) < p.ttl {
		entry.lastUsed = time.Now()
		p.mu.RUnlock()
		return entry.client
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check under write lock
	if entry, ok := p.entries[tenantID]; ok && entry.configKey == configHash && time.Since(entry.lastUsed) < p.ttl {
		entry.lastUsed = time.Now()
		return entry.client
	}

	// Evict old entries if pool is full
	if len(p.entries) >= p.maxEntries {
		p.evictOldestLocked()
	}

	client := NewClient(baseURL, apiKey, modelID, p.httpClient)
	if p.gatewayToken != "" {
		client.WithGatewayToken(p.gatewayToken)
	}
	p.entries[tenantID] = &poolEntry{
		client:    client,
		lastUsed:  time.Now(),
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
	}
}

func hashConfig(baseURL, apiKey, modelID string) string {
	h := sha256.New()
	h.Write([]byte(baseURL))
	h.Write([]byte("|"))
	h.Write([]byte(apiKey))
	h.Write([]byte("|"))
	h.Write([]byte(modelID))
	return hex.EncodeToString(h.Sum(nil))
}
