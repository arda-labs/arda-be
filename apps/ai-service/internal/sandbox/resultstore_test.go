package sandbox

import (
	"encoding/json"
	"testing"
	"time"
)

func TestResultStore_PutGetRoundtrip(t *testing.T) {
	store := NewResultStore()
	data := json.RawMessage(`{"customers":[{"id":"c1"}]}`)
	logs := []string{"fetched customer c1"}

	id := store.Put("run-1", data, logs)
	if id == "" {
		t.Fatal("expected non-empty resultId")
	}

	gotData, gotLogs, ok := store.Get("run-1", id)
	if !ok {
		t.Fatal("expected stored result")
	}
	if string(gotData) != string(data) {
		t.Errorf("data = %s, want %s", gotData, data)
	}
	if len(gotLogs) != 1 || gotLogs[0] != logs[0] {
		t.Errorf("logs = %v, want %v", gotLogs, logs)
	}
}

func TestResultStore_NamespaceIsolation(t *testing.T) {
	store := NewResultStore()
	id := store.Put("tenant-a:run-1", json.RawMessage(`{"x":1}`), nil)

	if _, _, ok := store.Get("tenant-b:run-1", id); ok {
		t.Error("cross-namespace read must fail")
	}
	if _, _, ok := store.Get("tenant-a:run-1", id); !ok {
		t.Error("same-namespace read must succeed")
	}
}

func TestResultStore_UnknownID(t *testing.T) {
	store := NewResultStore()
	if _, _, ok := store.Get("run-1", "run-1:999"); ok {
		t.Error("unknown resultId must not resolve")
	}
	if _, _, ok := store.Get("run-1", "run-1"); ok {
		t.Error("bare namespace must not resolve (no colon boundary)")
	}
}

func TestResultStore_TTLExpiry(t *testing.T) {
	store := NewResultStore()
	store.ttl = 50 * time.Millisecond

	id := store.Put("run-1", json.RawMessage(`{"x":1}`), nil)
	time.Sleep(80 * time.Millisecond)
	if _, _, ok := store.Get("run-1", id); ok {
		t.Error("expired result must not resolve")
	}
}

func TestResultStore_MaxEntriesEvictsOldest(t *testing.T) {
	store := NewResultStore()
	store.max = 3

	ids := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		ids = append(ids, store.Put("run-1", json.RawMessage(`{"i":1}`), nil))
	}
	// 4 entries stored with cap 3 → the first one is evicted.
	if _, _, ok := store.Get("run-1", ids[0]); ok {
		t.Error("oldest entry should be evicted")
	}
	for _, id := range ids[1:] {
		if _, _, ok := store.Get("run-1", id); !ok {
			t.Errorf("entry %s should still be present", id)
		}
	}
}

func TestResultStore_NilStore(t *testing.T) {
	var store *ResultStore
	if id := store.Put("run-1", json.RawMessage(`{"x":1}`), nil); id != "" {
		t.Errorf("nil store Put should return empty id, got %q", id)
	}
	if _, _, ok := store.Get("run-1", "run-1:1"); ok {
		t.Error("nil store Get must fail")
	}
}
