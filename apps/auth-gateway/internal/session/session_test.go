package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestUserInfoMarshalIncludesAuthVersion(t *testing.T) {
	data, err := json.Marshal(&UserInfo{UserID: "u1", Subject: "s1", AuthVersion: 18})
	if err != nil {
		t.Fatalf("marshal user info: %v", err)
	}
	if !strings.Contains(string(data), `"authVersion":18`) {
		t.Fatalf("json = %s, want authVersion", data)
	}
}

func TestMemoryStoreGetReturnsNilForExpiredEntryWithoutDeletingUnderReadLock(t *testing.T) {
	store := NewMemoryStore()
	sess := &Session{User: &UserInfo{UserID: "u1"}}
	// A negative TTL produces an already expired entry without waiting.
	if err := store.Create(context.Background(), sess, -time.Second); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(context.Background(), sess.ID)
	if err != nil || got != nil {
		t.Fatalf("expired session = (%v, %v), want (nil, nil)", got, err)
	}

	// Deletion belongs to cleanupLoop: Get only holds the read lock, so it must
	// not mutate the sessions map.
	store.mu.RLock()
	_, present := store.sessions[sess.ID]
	store.mu.RUnlock()
	if !present {
		t.Fatal("Get deleted an expired entry while holding only the read lock")
	}
}

func TestSessionLastAuthCheckRoundTripsJSON(t *testing.T) {
	checkedAt := time.Date(2026, 9, 16, 10, 30, 0, 0, time.UTC)
	data, err := json.Marshal(&Session{ID: "s1", LastAuthCheck: checkedAt})
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if !strings.Contains(string(data), `"last_auth_check"`) {
		t.Fatalf("json = %s, want last_auth_check", data)
	}
	var decoded Session
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	if !decoded.LastAuthCheck.Equal(checkedAt) {
		t.Fatalf("last auth check = %v, want %v", decoded.LastAuthCheck, checkedAt)
	}
}
