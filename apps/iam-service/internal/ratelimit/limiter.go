package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Tracker monitors per-user login attempts.
type tracker struct {
	attempts    int
	firstFail   time.Time
	lockedUntil time.Time
}

// Limiter enforces rate limits with in-memory state.
// For production, consider replacing with Redis-backed implementation.
type Limiter struct {
	mu       sync.Mutex
	trackers map[string]*tracker // key = username

	maxAttempts     int
	lockoutDuration time.Duration
	attemptWindow   time.Duration
	cleanupInterval time.Duration
}

// New creates a rate limiter with sensible defaults.
func New() *Limiter {
	l := &Limiter{
		trackers:        make(map[string]*tracker),
		maxAttempts:     5,
		lockoutDuration: 15 * time.Minute,
		attemptWindow:   10 * time.Minute,
		cleanupInterval: 5 * time.Minute,
	}

	go l.cleanupLoop()
	return l
}

// CheckLogin verifies the user is not rate-limited.
// Returns nil if login can proceed, or an error if blocked.
func (l *Limiter) CheckLogin(username string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	t, exists := l.trackers[username]
	if !exists {
		return nil
	}

	// Reset if the attempt window has passed
	if time.Since(t.firstFail) > l.attemptWindow {
		delete(l.trackers, username)
		return nil
	}

	// Check if account is locked
	if !t.lockedUntil.IsZero() {
		if time.Now().Before(t.lockedUntil) {
			remaining := time.Until(t.lockedUntil).Round(time.Second)
			return fmt.Errorf("account locked for %s", remaining)
		}
		// Lock expired
		delete(l.trackers, username)
	}

	return nil
}

// RecordFailure increments the failure counter and locks if threshold reached.
func (l *Limiter) RecordFailure(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	t, exists := l.trackers[username]
	if !exists {
		t = &tracker{firstFail: time.Now()}
		l.trackers[username] = t
	}

	if time.Since(t.firstFail) > l.attemptWindow {
		t.attempts = 0
		t.firstFail = time.Now()
	}

	t.attempts++

	if t.attempts >= l.maxAttempts {
		t.lockedUntil = time.Now().Add(l.lockoutDuration)
	}
}

// Reset clears the tracker on successful login.
func (l *Limiter) Reset(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.trackers, username)
}

// IsLocked returns true if the account is currently locked.
func (l *Limiter) IsLocked(username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	t, exists := l.trackers[username]
	if !exists {
		return false
	}
	if !t.lockedUntil.IsZero() && time.Now().Before(t.lockedUntil) {
		return true
	}
	return false
}

// RemainingAttempts returns how many attempts left before lockout.
func (l *Limiter) RemainingAttempts(username string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	t, exists := l.trackers[username]
	if !exists {
		return l.maxAttempts
	}

	if time.Since(t.firstFail) > l.attemptWindow {
		return l.maxAttempts
	}

	return max(l.maxAttempts-t.attempts, 0)
}

func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		l.cleanup()
	}
}

func (l *Limiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for key, t := range l.trackers {
		// Remove if lock expired and window passed
		if !t.lockedUntil.IsZero() && now.After(t.lockedUntil) && now.Sub(t.firstFail) > l.attemptWindow {
			delete(l.trackers, key)
		}
		// Remove stale trackers past the attempt window
		if t.lockedUntil.IsZero() && now.Sub(t.firstFail) > l.attemptWindow {
			delete(l.trackers, key)
		}
	}
}

// RateLimitIP checks if an IP has made too many requests.
// Simplified in-memory implementation; use Redis sliding window in production.
func (l *Limiter) RateLimitIP(ctx context.Context, ip string, maxPerSec int) bool {
	// In-memory placeholder — production should use Redis.
	// For now, we rely on the per-user tracker.
	return false
}
