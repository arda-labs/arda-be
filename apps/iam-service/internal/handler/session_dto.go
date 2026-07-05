package handler

import (
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/service"
)

type sessionItemJSON struct {
	ID           string     `json:"id"`
	DeviceID     string     `json:"device_id,omitempty"`
	DeviceName   string     `json:"device_name,omitempty"`
	DeviceType   string     `json:"device_type,omitempty"`
	OS           string     `json:"os,omitempty"`
	Browser      string     `json:"browser,omitempty"`
	IsTrusted    bool       `json:"is_trusted"`
	TrustedUntil *time.Time `json:"trusted_until,omitempty"`
	IPAddress    string     `json:"ip_address,omitempty"`
	UserAgent    string     `json:"user_agent,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	IsActive     bool       `json:"is_active"`
	IsCurrent    bool       `json:"is_current,omitempty"`
}

func toSessionItemJSON(s service.SessionDetails, isCurrent bool) sessionItemJSON {
	return sessionItemJSON{
		ID:           s.ID,
		DeviceID:     s.DeviceID,
		DeviceName:   s.DeviceName,
		DeviceType:   s.DeviceType,
		OS:           s.OS,
		Browser:      s.Browser,
		IsTrusted:    s.IsTrusted,
		TrustedUntil: s.TrustedUntil,
		IPAddress:    s.IPAddress,
		UserAgent:    s.UserAgent,
		CreatedAt:    s.CreatedAt,
		LastSeenAt:   s.LastSeenAt,
		ExpiresAt:    s.ExpiresAt,
		IsActive:     s.IsActive,
		IsCurrent:    isCurrent,
	}
}

type deviceItemJSON struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	DeviceName   string     `json:"device_name,omitempty"`
	DeviceType   string     `json:"device_type,omitempty"`
	OS           string     `json:"os,omitempty"`
	Browser      string     `json:"browser,omitempty"`
	Fingerprint  string     `json:"fingerprint,omitempty"`
	IsTrusted    bool       `json:"is_trusted"`
	TrustedUntil *time.Time `json:"trusted_until,omitempty"`
	FirstSeenAt  time.Time  `json:"first_seen_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
}
