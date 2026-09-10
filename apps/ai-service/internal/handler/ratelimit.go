package handler

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	tokens    float64
	lastFill  time.Time
	perMinute float64
}

// RateLimitStore decides whether a request key is within its per-minute budget.
// Implementations must be safe for concurrent use.
type RateLimitStore interface {
	Allow(ctx context.Context, key string, perMinute int) bool
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	limit   int
}

func newRateLimiter(perMinute int) *rateLimiter {
	if perMinute <= 0 {
		perMinute = 30
	}
	return &rateLimiter{buckets: map[string]*rateBucket{}, limit: perMinute}
}

// Allow implements a token bucket refilled at perMinute tokens/minute.
func (l *rateLimiter) Allow(_ context.Context, key string, perMinute int) bool {
	if key == "" {
		return true
	}
	limit := perMinute
	if limit <= 0 {
		limit = l.limit
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) > 10_000 {
			l.buckets = map[string]*rateBucket{}
		}
		bucket = &rateBucket{tokens: float64(limit), lastFill: now, perMinute: float64(limit)}
		l.buckets[key] = bucket
		return true
	}
	elapsed := now.Sub(bucket.lastFill).Minutes()
	bucket.tokens += elapsed * bucket.perMinute
	if bucket.tokens > bucket.perMinute {
		bucket.tokens = bucket.perMinute
	}
	bucket.lastFill = now
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

// RateLimitMiddleware rejects requests over the per-tenant/user budget. A nil
// store uses the in-process token bucket (single-replica behavior); a Redis
// store shares the window across replicas.
func RateLimitMiddleware(next http.Handler, perMinute int, store RateLimitStore) http.Handler {
	if store == nil {
		store = newRateLimiter(perMinute)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			next.ServeHTTP(w, r)
			return
		}
		key := strings.TrimSpace(r.Header.Get("X-Tenant-Id")) + "|" + strings.TrimSpace(r.Header.Get("X-User-Id"))
		ctx, cancel := context.WithTimeout(r.Context(), 250*time.Millisecond)
		defer cancel()
		if !store.Allow(ctx, key, perMinute) {
			problem(w, http.StatusTooManyRequests, "ai.rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}
