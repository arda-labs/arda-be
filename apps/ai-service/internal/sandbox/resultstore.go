package sandbox

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	resultStoreTTL        = 15 * time.Minute
	resultStoreMaxEntries = 64
)

type storedResult struct {
	Data      json.RawMessage `json:"data"`
	Logs      []string        `json:"logs,omitempty"`
	Seq       uint64          `json:"seq"`
	CreatedAt time.Time       `json:"createdAt"`
}

// ResultStore retains raw sandbox execution outputs for a bounded window so
// the model only receives a summary + resultId in the conversation and can
// fetch the full data on demand via readResult — the Anthropic/Cloudflare
// "filesystem as context" pattern instead of truncating data into tool
// feedback.
type ResultStore struct {
	mu    sync.Mutex
	items map[string]storedResult
	seq   atomic.Uint64
	ttl   time.Duration
	max   int
}

// NewResultStore returns a bounded, TTL-backed result store.
func NewResultStore() *ResultStore {
	return &ResultStore{
		items: make(map[string]storedResult),
		ttl:   resultStoreTTL,
		max:   resultStoreMaxEntries,
	}
}

// Put stores data under namespace (scope.RequestID per run) and returns a
// short resultId the model can pass to readResult. Evicts expired entries and
// enforces the entry cap.
func (s *ResultStore) Put(namespace string, data json.RawMessage, logs []string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.evictLocked()

	seq := s.seq.Add(1)
	id := fmt.Sprintf("%s:%x", namespace, seq)
	s.items[id] = storedResult{
		Data:      data,
		Logs:      logs,
		Seq:       seq,
		CreatedAt: time.Now().UTC(),
	}

	// Bound: drop the lowest-sequence entries (insertion order) when over the
	// cap. Seq is monotonic, so eviction is deterministic even for entries
	// created in the same clock tick.
	for len(s.items) > s.max {
		var oldest string
		var oldestSeq uint64
		for key, item := range s.items {
			if oldest == "" || item.Seq < oldestSeq {
				oldest, oldestSeq = key, item.Seq
			}
		}
		delete(s.items, oldest)
	}
	return id
}

// Get returns the stored result for a resultId produced by Put. The resultId
// embeds the namespace, so lookups only succeed for the same run/request that
// created the result — cross-tenant reads are impossible.
func (s *ResultStore) Get(namespace, resultID string) (json.RawMessage, []string, bool) {
	if s == nil || resultID == "" || !hasNamespacePrefix(resultID, namespace) {
		return nil, nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[resultID]
	if !ok {
		return nil, nil, false
	}
	if time.Since(item.CreatedAt) > s.ttl {
		delete(s.items, resultID)
		return nil, nil, false
	}
	return item.Data, item.Logs, true
}

func (s *ResultStore) evictLocked() {
	for key, item := range s.items {
		if time.Since(item.CreatedAt) > s.ttl {
			delete(s.items, key)
		}
	}
}

// hasNamespacePrefix reports whether value starts with prefix followed by a
// colon boundary, so "ns-a:1" never matches namespace "ns".
func hasNamespacePrefix(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	if len(value) == len(prefix) {
		return false
	}
	return value[len(prefix)] == ':'
}
