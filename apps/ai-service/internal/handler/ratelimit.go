package handler

import (
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

func (l *rateLimiter) allow(key string) bool {
	if key == "" {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) > 10_000 {
			l.buckets = map[string]*rateBucket{}
		}
		bucket = &rateBucket{tokens: float64(l.limit), lastFill: now, perMinute: float64(l.limit)}
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

func RateLimitMiddleware(next http.Handler, perMinute int) http.Handler {
	limiter := newRateLimiter(perMinute)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			next.ServeHTTP(w, r)
			return
		}
		key := strings.TrimSpace(r.Header.Get("X-Tenant-Id")) + "|" + strings.TrimSpace(r.Header.Get("X-User-Id"))
		if !limiter.allow(key) {
			problem(w, http.StatusTooManyRequests, "ai.rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}
