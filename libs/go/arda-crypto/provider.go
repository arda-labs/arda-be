package ardacrypto

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var (
	ErrKeyNotFound = errors.New("ardacrypto: encryption key not found")
)

// KeyProvider abstracts access to root/master encryption keys (e.g. AWS KMS, HashiCorp Vault, Static Secret).
type KeyProvider interface {
	GetSecretKey(ctx context.Context, keyID string) (string, error)
}

// StaticKeyProvider implements KeyProvider backed by an in-memory secret string.
type StaticKeyProvider struct {
	mu      sync.RWMutex
	secrets map[string]string
	defaultKey string
}

func NewStaticKeyProvider(defaultSecret string) *StaticKeyProvider {
	p := &StaticKeyProvider{
		secrets: make(map[string]string),
		defaultKey: strings.TrimSpace(defaultSecret),
	}
	if p.defaultKey != "" {
		p.secrets["default"] = p.defaultKey
	}
	return p
}

func (p *StaticKeyProvider) SetKey(keyID, secret string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.secrets[keyID] = strings.TrimSpace(secret)
}

func (p *StaticKeyProvider) GetSecretKey(_ context.Context, keyID string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if keyID == "" || keyID == "default" {
		if p.defaultKey != "" {
			return p.defaultKey, nil
		}
	}

	secret, ok := p.secrets[keyID]
	if !ok || secret == "" {
		return "", ErrKeyNotFound
	}
	return secret, nil
}
