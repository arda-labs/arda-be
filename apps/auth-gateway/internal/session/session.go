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
	IAMSessionID string    `json:"iam_session_id,omitempty"`

	// Device info (tracked on login)
	DeviceID   string `json:"device_id,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	DeviceType string `json:"device_type,omitempty"`
	IPAddress  string `json:"ip_address,omitempty"`
}

// UserInfo is the safe user subset returned to the SPA (no tokens).
type UserInfo struct {
	UserID       string   `json:"userId"`
	Subject      string   `json:"subject"`
	Username     string   `json:"username"`
	Email        string   `json:"email"`
	DisplayName  string   `json:"displayName,omitempty"`
	Nickname     string   `json:"nickname,omitempty"`
	FirstName    string   `json:"firstName,omitempty"`
	LastName     string   `json:"lastName,omitempty"`
	PhoneNumber  string   `json:"phoneNumber,omitempty"`
	Birthdate    string   `json:"birthdate,omitempty"`
	Gender       string   `json:"gender,omitempty"`
	Address      string   `json:"address,omitempty"`
	Country      string   `json:"country,omitempty"`
	Picture      string   `json:"picture,omitempty"`
	AvatarFileID string   `json:"avatarFileId,omitempty"`
	CoverImage   string   `json:"coverImage,omitempty"`
	CoverFileID  string   `json:"coverFileId,omitempty"`
	TenantID     string   `json:"tenantId,omitempty"`
	OrgIDs       []string `json:"orgIds,omitempty"`
	Roles        []string `json:"roles"`
	Permissions  []string `json:"permissions"`
}

// Store defines the session persistence interface.
type Store interface {
	Create(ctx context.Context, session *Session, ttl time.Duration) error
	Get(ctx context.Context, sessionID string) (*Session, error)
	Delete(ctx context.Context, sessionID string) error
	Refresh(ctx context.Context, sessionID string, newSession *Session, ttl time.Duration) error

	// Extended operations
	ListByUser(ctx context.Context, userID string) ([]*Session, error)
	RevokeByUser(ctx context.Context, userID string, reason string) (int, error)
	RevokeAllExcept(ctx context.Context, userID, currentSessionID string) (int, error)
	CountActive(ctx context.Context, userID string) (int, error)
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
	userIdx  map[string]map[string]bool // userID → set<sessionID>
}

type sessionEntry struct {
	session *Session
	expires time.Time
}

// NewMemoryStore creates an in-memory session store.
func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{
		sessions: make(map[string]*sessionEntry),
		userIdx:  make(map[string]map[string]bool),
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
	// Index by user
	if session.User != nil && session.User.UserID != "" {
		if s.userIdx[session.User.UserID] == nil {
			s.userIdx[session.User.UserID] = make(map[string]bool)
		}
		s.userIdx[session.User.UserID][session.ID] = true
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
	entry := s.sessions[sessionID]
	if entry != nil && entry.session.User != nil {
		delete(s.userIdx[entry.session.User.UserID], sessionID)
	}
	delete(s.sessions, sessionID)
	return nil
}

func (s *MemoryStore) Refresh(_ context.Context, oldID string, newSession *Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Remove old from index
	if oldEntry := s.sessions[oldID]; oldEntry != nil && oldEntry.session.User != nil {
		delete(s.userIdx[oldEntry.session.User.UserID], oldID)
	}
	delete(s.sessions, oldID)

	newSession.ID = NewID()
	newSession.CreatedAt = time.Now()
	s.sessions[newSession.ID] = &sessionEntry{
		session: newSession,
		expires: time.Now().Add(ttl),
	}
	if newSession.User != nil && newSession.User.UserID != "" {
		if s.userIdx[newSession.User.UserID] == nil {
			s.userIdx[newSession.User.UserID] = make(map[string]bool)
		}
		s.userIdx[newSession.User.UserID][newSession.ID] = true
	}
	return nil
}

func (s *MemoryStore) ListByUser(_ context.Context, userID string) ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.userIdx[userID]
	var result []*Session
	for id := range ids {
		if entry := s.sessions[id]; entry != nil && time.Now().Before(entry.expires) {
			result = append(result, entry.session)
		}
	}
	return result, nil
}

func (s *MemoryStore) RevokeByUser(_ context.Context, userID string, reason string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.userIdx[userID]
	count := 0
	for id := range ids {
		if entry := s.sessions[id]; entry != nil {
			delete(s.sessions, id)
			count++
		}
	}
	delete(s.userIdx, userID)
	return count, nil
}

func (s *MemoryStore) RevokeAllExcept(_ context.Context, userID, currentSessionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.userIdx[userID]
	count := 0
	for id := range ids {
		if id == currentSessionID {
			continue
		}
		if entry := s.sessions[id]; entry != nil {
			delete(s.sessions, id)
			delete(s.userIdx[userID], id)
			count++
		}
	}
	return count, nil
}

func (s *MemoryStore) CountActive(_ context.Context, userID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.userIdx[userID]
	count := 0
	for id := range ids {
		if entry := s.sessions[id]; entry != nil && time.Now().Before(entry.expires) {
			count++
		}
	}
	return count, nil
}

func (s *MemoryStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, entry := range s.sessions {
			if now.After(entry.expires) {
				if entry.session.User != nil {
					delete(s.userIdx[entry.session.User.UserID], id)
				}
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}

// ── Helpers ──

// DefaultCookieName returns the default cookie name.
const DefaultCookieName = "arda_sid"

// MarshalJSON marshals user info.
func (u *UserInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"userId":       u.UserID,
		"subject":      u.Subject,
		"username":     u.Username,
		"email":        u.Email,
		"displayName":  u.DisplayName,
		"nickname":     u.Nickname,
		"firstName":    u.FirstName,
		"lastName":     u.LastName,
		"phoneNumber":  u.PhoneNumber,
		"birthdate":    u.Birthdate,
		"gender":       u.Gender,
		"address":      u.Address,
		"country":      u.Country,
		"picture":      u.Picture,
		"avatarFileId": u.AvatarFileID,
		"coverImage":   u.CoverImage,
		"coverFileId":  u.CoverFileID,
		"tenantId":     u.TenantID,
		"orgIds":       u.OrgIDs,
		"roles":        u.Roles,
		"permissions":  u.Permissions,
	})
}

// Ensure fmt is used for error formatting
var _ = fmt.Sprintf
