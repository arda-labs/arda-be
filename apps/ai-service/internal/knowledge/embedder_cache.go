package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// EmbeddingCache stores a vector per cache key. Get must report a miss without
// an error; implementations are best-effort and never fail an embed.
type EmbeddingCache interface {
	Get(ctx context.Context, key string) ([]float32, bool)
	Set(ctx context.Context, key string, vector []float32, ttl time.Duration)
}

// EmbeddingCacheKey is the content address of one embedding call: the model
// plus the exact text, so a model change can never serve a stale vector.
func EmbeddingCacheKey(model, text string) string {
	sum := sha256.Sum256([]byte(model + "\x00" + strings.TrimSpace(text)))
	return "ai:embed:v1:" + hex.EncodeToString(sum[:])
}

// CachedEmbedder wraps an Embedder with a best-effort query cache. Only
// single-text calls (queries) are cached; ingestion batches pass through so
// chunk payloads do not grow the cache.
type CachedEmbedder struct {
	inner Embedder
	cache EmbeddingCache
	ttl   time.Duration
}

func NewCachedEmbedder(inner Embedder, cache EmbeddingCache, ttl time.Duration) *CachedEmbedder {
	if ttl <= 0 {
		ttl = defaultEmbeddingCacheTTL
	}
	return &CachedEmbedder{inner: inner, cache: cache, ttl: ttl}
}

func (e *CachedEmbedder) Model() string   { return e.inner.Model() }
func (e *CachedEmbedder) Dimensions() int { return e.inner.Dimensions() }

func (e *CachedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) != 1 || e.cache == nil {
		return e.inner.Embed(ctx, texts)
	}
	key := EmbeddingCacheKey(e.inner.Model(), texts[0])
	if cached, ok := e.cache.Get(ctx, key); ok && len(cached) == e.inner.Dimensions() {
		return [][]float32{cached}, nil
	}
	vectors, err := e.inner.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) == 1 && len(vectors[0]) == e.inner.Dimensions() {
		e.cache.Set(ctx, key, vectors[0], e.ttl)
	}
	return vectors, nil
}

const defaultEmbeddingCacheTTL = 6 * time.Hour

// --- Redis cache -----------------------------------------------------------

// RedisEmbeddingCache shares query vectors across replicas. Every failure is
// swallowed: the cache must never break retrieval.
type RedisEmbeddingCache struct {
	client *redis.Client
	logger *slog.Logger
}

func NewRedisEmbeddingCache(client *redis.Client, logger *slog.Logger) *RedisEmbeddingCache {
	if logger == nil {
		logger = slog.Default()
	}
	return &RedisEmbeddingCache{client: client, logger: logger}
}

func (c *RedisEmbeddingCache) Get(ctx context.Context, key string) ([]float32, bool) {
	if c == nil || c.client == nil {
		return nil, false
	}
	raw, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		c.logger.Warn("embedding cache get failed", "err", err)
		return nil, false
	}
	var vector []float32
	if err := json.Unmarshal(raw, &vector); err != nil {
		c.logger.Warn("embedding cache decode failed", "err", err)
		return nil, false
	}
	return vector, true
}

func (c *RedisEmbeddingCache) Set(ctx context.Context, key string, vector []float32, ttl time.Duration) {
	if c == nil || c.client == nil {
		return
	}
	raw, err := json.Marshal(vector)
	if err != nil {
		return
	}
	if err := c.client.Set(ctx, key, raw, ttl).Err(); err != nil {
		c.logger.Warn("embedding cache set failed", "err", err)
	}
}

// --- In-process fallback ---------------------------------------------------

const defaultMemoryCacheEntries = 256

type memoryCacheEntry struct {
	vector    []float32
	expiresAt time.Time
}

// MemoryEmbeddingCache keeps query vectors in-process. It is the fallback when
// Redis is not configured and the cache used in tests.
type MemoryEmbeddingCache struct {
	mu      sync.Mutex
	entries map[string]memoryCacheEntry
	max     int
}

func NewMemoryEmbeddingCache(max int) *MemoryEmbeddingCache {
	if max <= 0 {
		max = defaultMemoryCacheEntries
	}
	return &MemoryEmbeddingCache{entries: make(map[string]memoryCacheEntry), max: max}
}

func (c *MemoryEmbeddingCache) Get(_ context.Context, key string) ([]float32, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	return entry.vector, true
}

func (c *MemoryEmbeddingCache) Set(_ context.Context, key string, vector []float32, ttl time.Duration) {
	if c == nil || ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		for k, entry := range c.entries {
			if time.Now().After(entry.expiresAt) {
				delete(c.entries, k)
			}
		}
	}
	if len(c.entries) >= c.max {
		// Still full: drop an arbitrary entry. Query caching is best-effort.
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
	c.entries[key] = memoryCacheEntry{vector: vector, expiresAt: time.Now().Add(ttl)}
}
