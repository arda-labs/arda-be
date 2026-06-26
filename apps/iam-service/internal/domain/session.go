package domain

import "time"

// Session represents an authoritative login session in IAM.
type Session struct {
	ID              string
	UserID          string
	DeviceID        string
	HydraSessionID  string
	AccessTokenJTI  string
	RefreshTokenJTI string
	IPAddress       string
	UserAgent       string
	IsActive        bool
	CreatedAt       time.Time
	ExpiresAt       time.Time
	LastSeenAt      time.Time
	RevokedAt       *time.Time
	RevokedReason   string
}

// Device represents a known device used for authentication.
type Device struct {
	ID          string
	UserID      string
	DeviceName  string    // "Chrome on Windows", "iPhone 15"
	DeviceType  string    // browser, mobile_app, api_key
	OS          string
	Browser     string
	Fingerprint string    // hashed device fingerprint
	IsTrusted   bool
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}
