package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

// SessionRepository provides persistence for sessions and devices.
type SessionRepository struct {
	db *sql.DB
}

// NewSessionRepository creates a new session repository.
func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// ── Devices ──

type DeviceFingerprint struct {
	UserAgent   string
	IP          string
	AcceptLang  string
}

func (f DeviceFingerprint) Hash() string {
	// simplified — in production use sha256
	return fmt.Sprintf("%s|%s|%s", truncate(f.UserAgent, 128), maskIP(f.IP), f.AcceptLang)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func maskIP(ip string) string {
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == '.' {
			return ip[:i] + ".0"
		}
	}
	return ip
}

// FindDeviceByFingerprint looks up a device by fingerprint hash.
func (r *SessionRepository) FindDeviceByFingerprint(ctx context.Context, userID, fingerprint string) (*domain.Device, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, device_name, device_type, os, browser,
		       fingerprint_hash, is_trusted, first_seen_at, last_seen_at
		FROM iam_devices
		WHERE user_id = $1 AND fingerprint_hash = $2
	`, userID, fingerprint)

	return scanDevice(row)
}

// UpsertDevice creates or updates a device record.
func (r *SessionRepository) UpsertDevice(ctx context.Context, d *domain.Device) (*domain.Device, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO iam_devices (user_id, device_name, device_type, os, browser, fingerprint_hash, is_trusted, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT DO NOTHING
		RETURNING id, first_seen_at, last_seen_at
	`, d.UserID, d.DeviceName, d.DeviceType, d.OS, d.Browser, d.Fingerprint, d.IsTrusted)

	err := row.Scan(&d.ID, &d.FirstSeenAt, &d.LastSeenAt)
	if err == nil {
		return d, nil
	}

	// Already exists — update last_seen
	_, err = r.db.ExecContext(ctx, `
		UPDATE iam_devices SET last_seen_at = now()
		WHERE user_id = $1 AND fingerprint_hash = $2
	`, d.UserID, d.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("update device: %w", err)
	}

	return r.FindDeviceByFingerprint(ctx, d.UserID, d.Fingerprint)
}

// ListDevicesByUser returns all devices for a user.
func (r *SessionRepository) ListDevicesByUser(ctx context.Context, userID string) ([]domain.Device, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, device_name, device_type, os, browser,
		       fingerprint_hash, is_trusted, first_seen_at, last_seen_at
		FROM iam_devices
		WHERE user_id = $1
		ORDER BY last_seen_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []domain.Device
	for rows.Next() {
		var d domain.Device
		if err := rows.Scan(&d.ID, &d.UserID, &d.DeviceName, &d.DeviceType,
			&d.OS, &d.Browser, &d.Fingerprint, &d.IsTrusted,
			&d.FirstSeenAt, &d.LastSeenAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// GetDevice returns a single device by ID.
func (r *SessionRepository) GetDevice(ctx context.Context, deviceID string) (*domain.Device, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, device_name, device_type, os, browser,
		       fingerprint_hash, is_trusted, first_seen_at, last_seen_at
		FROM iam_devices
		WHERE id = $1
	`, deviceID)
	return scanDevice(row)
}

// DeleteDevice removes a device record.
func (r *SessionRepository) DeleteDevice(ctx context.Context, deviceID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM iam_devices WHERE id = $1`, deviceID)
	return err
}

// TrustDevice marks a device as trusted (skip MFA).
func (r *SessionRepository) TrustDevice(ctx context.Context, deviceID string, trusted bool) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE iam_devices SET is_trusted = $1 WHERE id = $2
	`, trusted, deviceID)
	return err
}

// ── Sessions ──

func (r *SessionRepository) CreateSession(ctx context.Context, s *domain.Session) error {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO iam_sessions (user_id, device_id, hydra_session_id, access_token_jti,
		                          refresh_token_jti, ip_address, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at
	`, s.UserID, nullUUID(s.DeviceID), nullStr(s.HydraSessionID),
		nullStr(s.AccessTokenJTI), nullStr(s.RefreshTokenJTI),
		s.IPAddress, s.UserAgent, s.ExpiresAt)

	err := row.Scan(&s.ID, &s.CreatedAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *SessionRepository) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, device_id, hydra_session_id, access_token_jti,
		       refresh_token_jti, ip_address, user_agent, is_active,
		       created_at, expires_at, last_seen_at, revoked_at, revoked_reason
		FROM iam_sessions
		WHERE id = $1
	`, sessionID)
	return scanSession(row)
}

func (r *SessionRepository) ListSessionsByUser(ctx context.Context, userID string) ([]domain.Session, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, device_id, hydra_session_id, access_token_jti,
		       refresh_token_jti, ip_address, user_agent, is_active,
		       created_at, expires_at, last_seen_at, revoked_at, revoked_reason
		FROM iam_sessions
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []domain.Session
	for rows.Next() {
		var s domain.Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.DeviceID, &s.HydraSessionID,
			&s.AccessTokenJTI, &s.RefreshTokenJTI, &s.IPAddress, &s.UserAgent,
			&s.IsActive, &s.CreatedAt, &s.ExpiresAt, &s.LastSeenAt,
			&s.RevokedAt, &s.RevokedReason); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (r *SessionRepository) ListActiveSessionsByUser(ctx context.Context, userID string) ([]domain.Session, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, device_id, hydra_session_id, access_token_jti,
		       refresh_token_jti, ip_address, user_agent, is_active,
		       created_at, expires_at, last_seen_at, revoked_at, revoked_reason
		FROM iam_sessions
		WHERE user_id = $1 AND is_active = true AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []domain.Session
	for rows.Next() {
		var s domain.Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.DeviceID, &s.HydraSessionID,
			&s.AccessTokenJTI, &s.RefreshTokenJTI, &s.IPAddress, &s.UserAgent,
			&s.IsActive, &s.CreatedAt, &s.ExpiresAt, &s.LastSeenAt,
			&s.RevokedAt, &s.RevokedReason); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// CountActiveSessions returns number of active sessions for a user.
func (r *SessionRepository) CountActiveSessions(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM iam_sessions
		WHERE user_id = $1 AND is_active = true AND (expires_at IS NULL OR expires_at > now())
	`, userID).Scan(&count)
	return count, err
}

// RevokeSession soft-deletes a single session.
func (r *SessionRepository) RevokeSession(ctx context.Context, sessionID, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE iam_sessions
		SET is_active = false, revoked_at = now(), revoked_reason = $2
		WHERE id = $1 AND is_active = true
	`, sessionID, reason)
	return err
}

// RevokeSessionsByUser revokes all sessions for a user (logout all devices).
func (r *SessionRepository) RevokeSessionsByUser(ctx context.Context, userID, reason string) (int, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE iam_sessions
		SET is_active = false, revoked_at = now(), revoked_reason = $2
		WHERE user_id = $1 AND is_active = true
	`, userID, reason)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RevokeSessionsExcept revokes all sessions except the specified one.
func (r *SessionRepository) RevokeSessionsExcept(ctx context.Context, userID, keepSessionID, reason string) (int, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE iam_sessions
		SET is_active = false, revoked_at = now(), revoked_reason = $3
		WHERE user_id = $1 AND id != $2 AND is_active = true
	`, userID, keepSessionID, reason)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// TouchSession updates last_seen_at.
func (r *SessionRepository) TouchSession(ctx context.Context, sessionID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE iam_sessions SET last_seen_at = now() WHERE id = $1
	`, sessionID)
	return err
}

// ── Token Blacklist (for revoked tokens) ──

func (r *SessionRepository) BlacklistToken(ctx context.Context, jti, userID string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO iam_session_blacklist (jti, user_id, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (jti) DO NOTHING
	`, jti, userID, expiresAt)
	return err
}

func (r *SessionRepository) IsTokenBlacklisted(ctx context.Context, jti string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM iam_session_blacklist WHERE jti = $1 AND expires_at > now())
	`, jti).Scan(&exists)
	return exists, err
}

// CleanExpiredBlacklist removes expired entries.
func (r *SessionRepository) CleanExpiredBlacklist(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM iam_session_blacklist WHERE expires_at <= now()`)
	return err
}

// ── Max concurrent sessions ──

// MaxConcurrentReached checks if the user has reached the session limit.
// If max <= 0, no limit is enforced.
func (r *SessionRepository) MaxConcurrentReached(ctx context.Context, userID string, max int) (bool, error) {
	if max <= 0 {
		return false, nil
	}
	count, err := r.CountActiveSessions(ctx, userID)
	if err != nil {
		return false, err
	}
	return count >= max, nil
}

// ── Scanners ──

func scanDevice(row *sql.Row) (*domain.Device, error) {
	d := &domain.Device{}
	err := row.Scan(&d.ID, &d.UserID, &d.DeviceName, &d.DeviceType,
		&d.OS, &d.Browser, &d.Fingerprint, &d.IsTrusted,
		&d.FirstSeenAt, &d.LastSeenAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan device: %w", err)
	}
	return d, nil
}

func scanSession(row *sql.Row) (*domain.Session, error) {
	s := &domain.Session{}
	err := row.Scan(&s.ID, &s.UserID, &s.DeviceID, &s.HydraSessionID,
		&s.AccessTokenJTI, &s.RefreshTokenJTI, &s.IPAddress, &s.UserAgent,
		&s.IsActive, &s.CreatedAt, &s.ExpiresAt, &s.LastSeenAt,
		&s.RevokedAt, &s.RevokedReason)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	return s, nil
}

func nullUUID(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
