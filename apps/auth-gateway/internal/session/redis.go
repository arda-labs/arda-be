package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix      = "bff:session:"
	userIdxPrefix  = "bff:user_sessions:"
)

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

	pipe := s.client.Pipeline()
	setCmd := pipe.Set(ctx, keyPrefix+session.ID, data, ttl)
	var saddCmd *redis.IntCmd
	var expireCmd *redis.BoolCmd
	if session.User != nil && session.User.UserID != "" {
		saddCmd = pipe.SAdd(ctx, userIdxPrefix+session.User.UserID, session.ID)
		expireCmd = pipe.Expire(ctx, userIdxPrefix+session.User.UserID, ttl)
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		var saddErr, expireErr error
		if saddCmd != nil {
			saddErr = saddCmd.Err()
		}
		if expireCmd != nil {
			expireErr = expireCmd.Err()
		}
		return fmt.Errorf("tx exec failed: %v (set_err: %v, sadd_err: %v, expire_err: %v)", err, setCmd.Err(), saddErr, expireErr)
	}
	return nil
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
	sess, err := s.Get(ctx, sessionID)
	if err == nil && sess != nil && sess.User != nil {
		pipe := s.client.Pipeline()
		pipe.Del(ctx, keyPrefix+sessionID)
		pipe.SRem(ctx, userIdxPrefix+sess.User.UserID, sessionID)
		_, err = pipe.Exec(ctx)
		return err
	}
	return s.client.Del(ctx, keyPrefix+sessionID).Err()
}

func (s *RedisStore) Refresh(ctx context.Context, oldID string, newSession *Session, ttl time.Duration) error {
	newSession.ID = NewID()
	newSession.CreatedAt = time.Now()
	data, err := json.Marshal(newSession)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	pipe := s.client.Pipeline()

	// Remove old from index and delete
	if newSession.User != nil && newSession.User.UserID != "" {
		pipe.SRem(ctx, userIdxPrefix+newSession.User.UserID, oldID)
		pipe.SAdd(ctx, userIdxPrefix+newSession.User.UserID, newSession.ID)
	}
	pipe.Del(ctx, keyPrefix+oldID)
	pipe.Set(ctx, keyPrefix+newSession.ID, data, ttl)

	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) ListByUser(ctx context.Context, userID string) ([]*Session, error) {
	ids, err := s.client.SMembers(ctx, userIdxPrefix+userID).Result()
	if err != nil {
		return nil, err
	}

	var result []*Session
	for _, id := range ids {
		sess, err := s.Get(ctx, id)
		if err == nil && sess != nil {
			result = append(result, sess)
		} else if err == nil {
			// Clean up stale index entry
			s.client.SRem(ctx, userIdxPrefix+userID, id)
		}
	}
	return result, nil
}

func (s *RedisStore) RevokeByUser(ctx context.Context, userID string, _ string) (int, error) {
	ids, err := s.client.SMembers(ctx, userIdxPrefix+userID).Result()
	if err != nil {
		return 0, err
	}

	pipe := s.client.Pipeline()
	for _, id := range ids {
		pipe.Del(ctx, keyPrefix+id)
	}
	pipe.Del(ctx, userIdxPrefix+userID)
	_, err = pipe.Exec(ctx)
	return len(ids), err
}

func (s *RedisStore) RevokeAllExcept(ctx context.Context, userID, currentSessionID string) (int, error) {
	ids, err := s.client.SMembers(ctx, userIdxPrefix+userID).Result()
	if err != nil {
		return 0, err
	}

	pipe := s.client.Pipeline()
	count := 0
	for _, id := range ids {
		if id == currentSessionID {
			continue
		}
		pipe.Del(ctx, keyPrefix+id)
		pipe.SRem(ctx, userIdxPrefix+userID, id)
		count++
	}
	_, err = pipe.Exec(ctx)
	return count, err
}

func (s *RedisStore) CountActive(ctx context.Context, userID string) (int, error) {
	count, err := s.client.SCard(ctx, userIdxPrefix+userID).Result()
	return int(count), err
}
