package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Session holds the BFF session state — access/refresh tokens + user context.
type Session struct {
	ID           string    `json:"id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	User         *UserInfo `json:"user"`
	CreatedAt    time.Time `json:"created_at"`
}

// UserInfo is the safe user subset returned to the SPA (no tokens).
type UserInfo struct {
	UserID      string   `json:"userId"`
	Subject     string   `json:"subject"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

// Store defines the session persistence interface.
type Store interface {
	Create(ctx context.Context, session *Session, ttl time.Duration) error
	Get(ctx context.Context, sessionID string) (*Session, error)
	Delete(ctx context.Context, sessionID string) error
	Refresh(ctx context.Context, sessionID string, newSession *Session, ttl time.Duration) error
}

// NewID generates a UUID v7 session ID.
func NewID() string {
	return uuid.NewString()
}

// ── In-memory store (for dev / single-instance) ──

// MemoryStore holds sessions in a sync.Map.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*sessionEntry
}

type sessionEntry struct {
	session *Session
	expires time.Time
}

// NewMemoryStore creates an in-memory session store.
func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{
		sessions: make(map[string]*sessionEntry),
	}
	go s.cleanupLoop()
	return s
}

func (s *MemoryStore) Create(_ context.Context, session *Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session.ID = NewID()
	session.CreatedAt = time.Now()
	s.sessions[session.ID] = &sessionEntry{
		session: session,
		expires: time.Now().Add(ttl),
	}
	return nil
}

func (s *MemoryStore) Get(_ context.Context, sessionID string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.sessions[sessionID]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(s.sessions, sessionID)
		}
		return nil, nil
	}
	return entry.session, nil
}

func (s *MemoryStore) Delete(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return nil
}

func (s *MemoryStore) Refresh(_ context.Context, oldID string, newSession *Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, oldID)
	newSession.ID = NewID()
	newSession.CreatedAt = time.Now()
	s.sessions[newSession.ID] = &sessionEntry{
		session: newSession,
		expires: time.Now().Add(ttl),
	}
	return nil
}

func (s *MemoryStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, entry := range s.sessions {
			if now.After(entry.expires) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}

// ── Helpers ──

// SessionCookieName returns the default cookie name.
const DefaultCookieName = "arda_sid"

// MarshalJSON marshals user info.
func (u *UserInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"userId":      u.UserID,
		"subject":     u.Subject,
		"username":    u.Username,
		"email":       u.Email,
		"roles":       u.Roles,
		"permissions": u.Permissions,
	})
}

// Ensure fmt is used for error formatting
var _ = fmt.Sprintf
