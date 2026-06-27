package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
)

const (
	DefaultMaxConcurrentSessions = 5
	DefaultMaxDevices            = 10
	IdleTimeout                  = 30 * time.Minute
	AbsoluteTimeout              = 12 * time.Hour
	DefaultDeviceTrustTTL        = 30 * 24 * time.Hour
)

// SessionConfig controls session limits.
type SessionConfig struct {
	MaxConcurrent int    // <= 0 = no limit
	MaxDevices    int    // <= 0 = no limit
	OnExceed      string // "reject_oldest", "reject_new"
	DefaultTTL    time.Duration
}

var DefaultSessionConfig = SessionConfig{
	MaxConcurrent: DefaultMaxConcurrentSessions,
	MaxDevices:    DefaultMaxDevices,
	OnExceed:      "reject_oldest",
	DefaultTTL:    AbsoluteTimeout,
}

type SessionDetails struct {
	domain.Session
	DeviceName   string     `json:"deviceName,omitempty"`
	DeviceType   string     `json:"deviceType,omitempty"`
	OS           string     `json:"os,omitempty"`
	Browser      string     `json:"browser,omitempty"`
	IsTrusted    bool       `json:"isTrusted"`
	TrustedUntil *time.Time `json:"trustedUntil,omitempty"`
}

// SessionService handles session business logic.
type SessionService struct {
	sessionRepo *repository.SessionRepository
	cfg         SessionConfig
	logger      *slog.Logger
}

// NewSessionService creates a session service.
func NewSessionService(repo *repository.SessionRepository, cfg SessionConfig) *SessionService {
	return &SessionService{
		sessionRepo: repo,
		cfg:         cfg,
		logger:      slog.Default(),
	}
}

// Logger returns the logger for use by handlers.
func (s *SessionService) Logger() *slog.Logger {
	return s.logger
}

// GetDevice returns a device by ID (for MFA trusted device check).
func (s *SessionService) GetDevice(ctx context.Context, deviceID string) (*domain.Device, error) {
	return s.sessionRepo.GetDevice(ctx, deviceID)
}

// GetDeviceByToken returns a known device for a durable token.
func (s *SessionService) GetDeviceByToken(ctx context.Context, userID, deviceToken string) (*domain.Device, error) {
	tokenHash := repository.HashDeviceToken(deviceToken)
	if tokenHash == "" {
		return nil, nil
	}
	return s.sessionRepo.FindDeviceByTokenHash(ctx, userID, tokenHash)
}

// SessionRepo returns the underlying session repository (for MFA service).
func (s *SessionService) SessionRepo() *repository.SessionRepository {
	return s.sessionRepo
}

// ForceRevokeSession revokes a session by ID without ownership check (internal API).
func (s *SessionService) ForceRevokeSession(ctx context.Context, sessionID, reason string) error {
	return s.sessionRepo.RevokeSession(ctx, sessionID, reason)
}

// CreateSession creates a new session, enforcing concurrent limits.
func (s *SessionService) CreateSession(ctx context.Context, userID, deviceID, hydraID,
	accessJTI, refreshJTI, ip, userAgent string) (*domain.Session, error) {

	if s.cfg.MaxConcurrent > 0 {
		reached, err := s.sessionRepo.MaxConcurrentReached(ctx, userID, s.cfg.MaxConcurrent)
		if err != nil {
			return nil, fmt.Errorf("check limit: %w", err)
		}
		if reached {
			switch s.cfg.OnExceed {
			case "reject_oldest":
				sessions, err := s.sessionRepo.ListActiveSessionsByUser(ctx, userID)
				if err != nil {
					return nil, fmt.Errorf("list sessions: %w", err)
				}
				if len(sessions) > 0 {
					oldest := sessions[len(sessions)-1]
					if err := s.sessionRepo.RevokeSession(ctx, oldest.ID, "replaced_by_newer"); err != nil {
						s.logger.Warn("failed to revoke oldest session", "session_id", oldest.ID, "err", err)
					}
				}
			case "reject_new":
				return nil, fmt.Errorf("maximum concurrent sessions reached (%d)", s.cfg.MaxConcurrent)
			}
		}
	}

	now := time.Now()
	sess := &domain.Session{
		UserID:          userID,
		DeviceID:        deviceID,
		HydraSessionID:  hydraID,
		AccessTokenJTI:  accessJTI,
		RefreshTokenJTI: refreshJTI,
		IPAddress:       ip,
		UserAgent:       userAgent,
		IsActive:        true,
		ExpiresAt:       now.Add(s.cfg.DefaultTTL),
		LastSeenAt:      now,
	}

	if err := s.sessionRepo.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// ListSessions returns all active sessions for a user.
func (s *SessionService) ListSessions(ctx context.Context, userID string) ([]domain.Session, error) {
	return s.sessionRepo.ListActiveSessionsByUser(ctx, userID)
}

// ListSessionDetails returns active sessions joined with device metadata.
func (s *SessionService) ListSessionDetails(ctx context.Context, userID string) ([]SessionDetails, error) {
	sessions, err := s.sessionRepo.ListActiveSessionsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	devices, err := s.sessionRepo.ListDevicesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	deviceMap := make(map[string]domain.Device, len(devices))
	for _, d := range devices {
		deviceMap[d.ID] = d
	}

	details := make([]SessionDetails, 0, len(sessions))
	for _, sess := range sessions {
		detail := SessionDetails{Session: sess}
		if dev, ok := deviceMap[sess.DeviceID]; ok {
			detail.DeviceName = dev.DeviceName
			detail.DeviceType = dev.DeviceType
			detail.OS = dev.OS
			detail.Browser = dev.Browser
			detail.IsTrusted = s.IsDeviceTrusted(&dev)
			detail.TrustedUntil = dev.TrustedUntil
		}
		details = append(details, detail)
	}
	return details, nil
}

// RevokeSession revokes a single session.
func (s *SessionService) RevokeSession(ctx context.Context, sessionID, userID, reason string) error {
	sess, err := s.sessionRepo.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("session not found")
	}
	if sess.UserID != userID {
		return fmt.Errorf("session does not belong to user")
	}
	return s.sessionRepo.RevokeSession(ctx, sessionID, reason)
}

// RevokeAllSessions revokes all sessions for a user.
func (s *SessionService) RevokeAllSessions(ctx context.Context, userID, reason string) (int, error) {
	sessions, err := s.sessionRepo.ListActiveSessionsByUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	for _, sess := range sessions {
		if sess.AccessTokenJTI != "" {
			_ = s.sessionRepo.BlacklistToken(ctx, sess.AccessTokenJTI, userID, sess.ExpiresAt)
		}
		if sess.RefreshTokenJTI != "" {
			_ = s.sessionRepo.BlacklistToken(ctx, sess.RefreshTokenJTI, userID, sess.ExpiresAt)
		}
	}
	return s.sessionRepo.RevokeSessionsByUser(ctx, userID, reason)
}

// RevokeAllExcept revokes all sessions except the current one.
func (s *SessionService) RevokeAllExcept(ctx context.Context, userID, keepSessionID, reason string) (int, error) {
	keep, err := s.sessionRepo.GetSession(ctx, keepSessionID)
	if err != nil {
		return 0, err
	}
	_ = keep

	sessions, err := s.sessionRepo.ListActiveSessionsByUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	for _, sess := range sessions {
		if sess.ID == keepSessionID {
			continue
		}
		if sess.AccessTokenJTI != "" {
			_ = s.sessionRepo.BlacklistToken(ctx, sess.AccessTokenJTI, userID, sess.ExpiresAt)
		}
		if sess.RefreshTokenJTI != "" {
			_ = s.sessionRepo.BlacklistToken(ctx, sess.RefreshTokenJTI, userID, sess.ExpiresAt)
		}
	}
	return s.sessionRepo.RevokeSessionsExcept(ctx, userID, keepSessionID, reason)
}

// GetSession returns a session by ID (scoped to user).
func (s *SessionService) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	return s.sessionRepo.GetSession(ctx, sessionID)
}

// Device methods

// GetOrCreateDevice finds a device by durable token or fingerprint or creates one.
func (s *SessionService) GetOrCreateDevice(ctx context.Context, userID, deviceName, deviceType, os, browser, fingerprint, deviceToken string) (*domain.Device, error) {
	tokenHash := repository.HashDeviceToken(deviceToken)

	if s.cfg.MaxDevices > 0 {
		existingByToken, err := s.sessionRepo.FindDeviceByTokenHash(ctx, userID, tokenHash)
		if err != nil {
			return nil, err
		}
		existingByFP := (*domain.Device)(nil)
		if existingByToken == nil && fingerprint != "" {
			existingByFP, err = s.sessionRepo.FindDeviceByFingerprint(ctx, userID, fingerprint)
			if err != nil {
				return nil, err
			}
		}
		if existingByToken == nil && existingByFP == nil {
			devices, err := s.sessionRepo.ListDevicesByUser(ctx, userID)
			if err != nil {
				return nil, err
			}
			if len(devices) >= s.cfg.MaxDevices {
				return nil, fmt.Errorf("maximum devices reached (%d)", s.cfg.MaxDevices)
			}
		}
	}

	d := &domain.Device{
		UserID:          userID,
		DeviceName:      deviceName,
		DeviceType:      deviceType,
		OS:              os,
		Browser:         browser,
		Fingerprint:     fingerprint,
		DeviceTokenHash: tokenHash,
	}
	return s.sessionRepo.UpsertDevice(ctx, d)
}

// ListDevices returns all devices for a user.
func (s *SessionService) ListDevices(ctx context.Context, userID string) ([]domain.Device, error) {
	devices, err := s.sessionRepo.ListDevicesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range devices {
		if !s.IsDeviceTrusted(&devices[i]) {
			devices[i].IsTrusted = false
		}
	}
	return devices, nil
}

// DeleteDevice removes a device (and its sessions).
func (s *SessionService) DeleteDevice(ctx context.Context, deviceID, userID string) error {
	d, err := s.sessionRepo.GetDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("device not found")
	}
	if d.UserID != userID {
		return fmt.Errorf("device does not belong to user")
	}

	sessions, err := s.sessionRepo.ListActiveSessionsByUser(ctx, userID)
	if err == nil {
		for _, sess := range sessions {
			if sess.DeviceID == deviceID {
				_ = s.sessionRepo.RevokeSession(ctx, sess.ID, "device_deleted")
			}
		}
	}

	return s.sessionRepo.DeleteDevice(ctx, deviceID)
}

// TrustDevice sets device trust level for MFA skip.
func (s *SessionService) TrustDevice(ctx context.Context, deviceID, userID string, trusted bool) error {
	if trusted {
		return s.TrustDeviceUntil(ctx, deviceID, userID, time.Now().Add(DefaultDeviceTrustTTL))
	}

	d, err := s.sessionRepo.GetDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("device not found")
	}
	if d.UserID != userID {
		return fmt.Errorf("device does not belong to user")
	}
	return s.sessionRepo.TrustDevice(ctx, deviceID, false, nil)
}

// TrustDeviceUntil marks a device trusted until a specific time.
func (s *SessionService) TrustDeviceUntil(ctx context.Context, deviceID, userID string, until time.Time) error {
	d, err := s.sessionRepo.GetDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("device not found")
	}
	if d.UserID != userID {
		return fmt.Errorf("device does not belong to user")
	}
	return s.sessionRepo.TrustDevice(ctx, deviceID, true, &until)
}

// RememberDevice ensures the current browser has a durable device record and trusts it for MFA.
func (s *SessionService) RememberDevice(ctx context.Context, userID, deviceToken string, until time.Time) (*domain.Device, error) {
	if deviceToken == "" {
		return nil, fmt.Errorf("device token required")
	}
	device, err := s.GetOrCreateDevice(ctx, userID, "", "browser", "", "", "", deviceToken)
	if err != nil {
		return nil, err
	}
	if err := s.sessionRepo.TrustDevice(ctx, device.ID, true, &until); err != nil {
		return nil, err
	}
	return s.sessionRepo.GetDevice(ctx, device.ID)
}

func (s *SessionService) IsDeviceTrusted(device *domain.Device) bool {
	if device == nil || !device.IsTrusted {
		return false
	}
	if device.TrustedUntil == nil {
		return true
	}
	return device.TrustedUntil.After(time.Now())
}

// Session config for auth-gateway

type SessionConfigDTO struct {
	MaxConcurrent int    `json:"maxConcurrent"`
	MaxDevices    int    `json:"maxDevices"`
	SessionTTL    string `json:"sessionTtl"`
}

func (s *SessionService) GetConfig() SessionConfigDTO {
	return SessionConfigDTO{
		MaxConcurrent: s.cfg.MaxConcurrent,
		MaxDevices:    s.cfg.MaxDevices,
		SessionTTL:    s.cfg.DefaultTTL.String(),
	}
}
