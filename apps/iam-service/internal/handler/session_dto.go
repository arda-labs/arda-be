package handler

import (
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
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

type internalCreateSessionRequest struct {
	UserID          string `json:"user_id"`
	HydraSessionID  string `json:"hydra_session_id"`
	AccessTokenJTI  string `json:"jti"`
	RefreshTokenJTI string `json:"refresh_jti"`
	IPAddress       string `json:"ip"`
	UserAgent       string `json:"user_agent"`
	DeviceName      string `json:"device_name"`
	DeviceType      string `json:"device_type"`
	OS              string `json:"os"`
	Browser         string `json:"browser"`
	Fingerprint     string `json:"fingerprint"`
	DeviceToken     string `json:"device_token"`
	TrustForMFA     bool   `json:"trust_for_mfa"`
	DeviceID        string `json:"device_id,omitempty"`
}

type internalCreateSessionResponse struct {
	SessionID string    `json:"session_id"`
	DeviceID  string    `json:"device_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type internalSessionItemJSON struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	DeviceID       string    `json:"device_id,omitempty"`
	HydraSessionID string    `json:"hydra_session_id,omitempty"`
	IPAddress      string    `json:"ip_address,omitempty"`
	UserAgent      string    `json:"user_agent,omitempty"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	LastSeenAt     time.Time `json:"last_seen_at"`
}

func toInternalSessionItemJSON(s domain.Session) internalSessionItemJSON {
	return internalSessionItemJSON{
		ID: s.ID, UserID: s.UserID, DeviceID: s.DeviceID, HydraSessionID: s.HydraSessionID,
		IPAddress: s.IPAddress, UserAgent: s.UserAgent, IsActive: s.IsActive,
		CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt, LastSeenAt: s.LastSeenAt,
	}
}
