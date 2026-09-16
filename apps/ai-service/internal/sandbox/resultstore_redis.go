package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis result-store keys. The namespace (tenant|actor|thread) is embedded in
// both the value key and the resultId so a lookup can never cross tenants.
const (
	redisResultPrefix    = "ai:sandbox:result:"
	redisResultSeqKey    = "ai:sandbox:result:seq"
	redisResultNamespace = "ai:sandbox:result:ns:"
	redisResultOpTimeout = 2 * time.Second
)

// RedisResultStore implements ResultStore on Redis so sandbox outputs survive
// a pod restart and are readable across replicas (ADR-005 §2). Every operation
// is best-effort: a Redis failure degrades to "result not found", never to a
// failed tool call.
type RedisResultStore struct {
	client *redis.Client
	logger *slog.Logger
	ttl    time.Duration
}

func NewRedisResultStore(client *redis.Client, logger *slog.Logger) *RedisResultStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &RedisResultStore{client: client, logger: logger, ttl: resultStoreTTL}
}

func (s *RedisResultStore) Put(namespace string, data json.RawMessage, logs []string) string {
	if s == nil || s.client == nil || strings.TrimSpace(namespace) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisResultOpTimeout)
	defer cancel()

	seq, err := s.client.Incr(ctx, redisResultSeqKey).Result()
	if err != nil {
		s.logger.Warn("sandbox result store: sequence allocation failed", "err", err)
		return ""
	}
	id := fmt.Sprintf("%s:%x", namespace, seq)
	payload, err := json.Marshal(storedResult{Data: data, Logs: logs, Seq: uint64(seq), CreatedAt: time.Now().UTC()})
	if err != nil {
		s.logger.Warn("sandbox result store: encode failed", "err", err)
		return ""
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, redisResultPrefix+id, payload, s.ttl)
	nsKey := redisResultNamespace + namespace
	pipe.LPush(ctx, nsKey, id)
	pipe.LTrim(ctx, nsKey, 0, int64(resultStorePerNamespaceMax-1))
	pipe.Expire(ctx, nsKey, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		s.logger.Warn("sandbox result store: store failed", "err", err)
		return ""
	}
	return id
}

func (s *RedisResultStore) Get(namespace, resultID string) (json.RawMessage, []string, bool) {
	if s == nil || s.client == nil || resultID == "" || !hasNamespacePrefix(resultID, namespace) {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisResultOpTimeout)
	defer cancel()

	raw, err := s.client.Get(ctx, redisResultPrefix+resultID).Bytes()
	if err == redis.Nil {
		return nil, nil, false
	}
	if err != nil {
		s.logger.Warn("sandbox result store: read failed", "err", err)
		return nil, nil, false
	}
	var item storedResult
	if err := json.Unmarshal(raw, &item); err != nil {
		s.logger.Warn("sandbox result store: decode failed", "err", err)
		return nil, nil, false
	}
	return item.Data, item.Logs, true
}
