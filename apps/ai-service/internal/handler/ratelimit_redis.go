package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisRateLimitStore shares a fixed one-minute window across replicas. A key
// is tenant|user, so every replica counts into the same bucket. Redis errors
// fall back to the in-process limiter instead of failing closed on a cache
// outage.
type redisRateLimitStore struct {
	client   *redis.Client
	fallback *rateLimiter
}

func NewRedisRateLimitStore(client *redis.Client, perMinute int) RateLimitStore {
	return &redisRateLimitStore{client: client, fallback: newRateLimiter(perMinute)}
}

func (s *redisRateLimitStore) Allow(ctx context.Context, key string, perMinute int) bool {
	if s == nil || s.client == nil || key == "" {
		return true
	}
	if perMinute <= 0 {
		perMinute = 30
	}
	window := time.Now().Unix() / 60
	redisKey := fmt.Sprintf("ai:ratelimit:%s:%d", key, window)
	count, err := s.client.Incr(ctx, redisKey).Result()
	if err != nil {
		return s.fallback.Allow(ctx, key, perMinute)
	}
	if count == 1 {
		s.client.Expire(ctx, redisKey, 2*time.Minute)
	}
	return count <= int64(perMinute)
}
