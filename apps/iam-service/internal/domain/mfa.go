package domain

import "time"

// MFASettings stores TOTP/WebAuthn settings for a user.
type MFASettings struct {
	UserID     string
	Method     string // totp, webauthn
	Secret     string // base32 TOTP secret (encrypted in production)
	IsEnrolled bool
	EnrolledAt *time.Time
	LastUsedAt *time.Time
	UpdatedAt  time.Time
}

// MFABackupCode stores a one-time backup code hash.
type MFABackupCode struct {
	ID        string
	UserID    string
	CodeHash  string
	IsUsed    bool
	UsedAt    *time.Time
	CreatedAt time.Time
}
