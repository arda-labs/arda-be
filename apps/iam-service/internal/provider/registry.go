package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry manages registered authentication providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]AuthenticationProvider
	byDomain  map[string]string // email domain → provider ID
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]AuthenticationProvider),
		byDomain:  make(map[string]string),
	}
}

// Register adds a provider. Call during startup, before serving requests.
func (r *Registry) Register(p AuthenticationProvider) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	meta := p.Metadata()
	if meta.ID == "" {
		return fmt.Errorf("provider must have an ID")
	}
	if _, exists := r.providers[meta.ID]; exists {
		return fmt.Errorf("provider %q already registered", meta.ID)
	}

	r.providers[meta.ID] = p
	for _, domain := range meta.Domains {
		r.byDomain[strings.ToLower(domain)] = meta.ID
	}
	return nil
}

// Get returns a provider by ID.
func (r *Registry) Get(id string) (AuthenticationProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", id)
	}
	return p, nil
}

// ResolveByEmail suggests a provider based on the email domain.
func (r *Registry) ResolveByEmail(email string) (AuthenticationProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return nil, false
	}
	domain := strings.ToLower(parts[1])
	id, ok := r.byDomain[domain]
	if !ok {
		return nil, false
	}
	p := r.providers[id]
	return p, true
}

// ListEnabled returns all enabled providers sorted by priority.
func (r *Registry) ListEnabled() []Metadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Metadata
	for _, p := range r.providers {
		meta := p.Metadata()
		if meta.IsEnabled {
			result = append(result, meta)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority < result[j].Priority
	})
	return result
}

// ValidateAll calls Validate on every provider. Use at startup.
func (r *Registry) ValidateAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.providers {
		if err := p.Validate(ctx); err != nil {
			return fmt.Errorf("provider %q validation failed: %w", p.Metadata().ID, err)
		}
	}
	return nil
}
