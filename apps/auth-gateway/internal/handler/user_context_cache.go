package handler

import (
	"sync"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
)

type userContextCache struct {
	ttl   time.Duration
	mu    sync.RWMutex
	items map[string]userContextCacheEntry
}

type userContextCacheEntry struct {
	ctx     *iamclient.UserContext
	expires time.Time
}

func newUserContextCache(ttl time.Duration) *userContextCache {
	if ttl <= 0 {
		return nil
	}
	return &userContextCache{
		ttl:   ttl,
		items: make(map[string]userContextCacheEntry),
	}
}

func (c *userContextCache) get(key string) (*iamclient.UserContext, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expires) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}
	return cloneUserContext(entry.ctx), true
}

func (c *userContextCache) set(key string, ctx *iamclient.UserContext) {
	if c == nil || key == "" || ctx == nil {
		return
	}
	c.mu.Lock()
	c.items[key] = userContextCacheEntry{
		ctx:     cloneUserContext(ctx),
		expires: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

func cloneUserContext(ctx *iamclient.UserContext) *iamclient.UserContext {
	if ctx == nil {
		return nil
	}
	cloned := *ctx
	cloned.OrgIDs = append([]string(nil), ctx.OrgIDs...)
	cloned.GroupIDs = append([]string(nil), ctx.GroupIDs...)
	cloned.Roles = append([]string(nil), ctx.Roles...)
	cloned.Permissions = append([]string(nil), ctx.Permissions...)
	return &cloned
}
