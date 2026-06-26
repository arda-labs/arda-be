package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "bff:session:"

// RedisStore implements Store backed by Redis.
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore creates a Redis-backed session store.
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) Create(ctx context.Context, session *Session, ttl time.Duration) error {
	session.ID = NewID()
	session.CreatedAt = time.Now()
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return s.client.Set(ctx, keyPrefix+session.ID, data, ttl).Err()
}

func (s *RedisStore) Get(ctx context.Context, sessionID string) (*Session, error) {
	data, err := s.client.Get(ctx, keyPrefix+sessionID).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &sess, nil
}

func (s *RedisStore) Delete(ctx context.Context, sessionID string) error {
	return s.client.Del(ctx, keyPrefix+sessionID).Err()
}

func (s *RedisStore) Refresh(ctx context.Context, oldID string, newSession *Session, ttl time.Duration) error {
	newSession.ID = NewID()
	newSession.CreatedAt = time.Now()
	data, err := json.Marshal(newSession)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	pipe := s.client.TxPipeline()
	pipe.Del(ctx, keyPrefix+oldID)
	pipe.Set(ctx, keyPrefix+newSession.ID, data, ttl)
	_, err = pipe.Exec(ctx)
	return err
}
