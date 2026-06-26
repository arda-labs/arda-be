package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

// MFARepository persists MFA settings and backup codes.
type MFARepository struct {
	db *sql.DB
}

// NewMFARepository creates an MFA repository.
func NewMFARepository(db *sql.DB) *MFARepository {
	return &MFARepository{db: db}
}

// GetSettings returns MFA settings for a user.
func (r *MFARepository) GetSettings(ctx context.Context, userID string) (*domain.MFASettings, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT user_id, method, secret, is_enrolled, enrolled_at, last_used_at, updated_at
		FROM iam_mfa_settings WHERE user_id = $1
	`, userID)

	s := &domain.MFASettings{}
	err := row.Scan(&s.UserID, &s.Method, &s.Secret, &s.IsEnrolled, &s.EnrolledAt, &s.LastUsedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get mfa settings: %w", err)
	}
	return s, nil
}

// UpsertSettings creates or updates MFA settings.
func (r *MFARepository) UpsertSettings(ctx context.Context, s *domain.MFASettings) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO iam_mfa_settings (user_id, method, secret, is_enrolled, enrolled_at)
		VALUES ($1, $2, $3, $4,
			CASE WHEN $4 THEN now() ELSE NULL END)
		ON CONFLICT (user_id) DO UPDATE SET
			method = EXCLUDED.method,
			secret = EXCLUDED.secret,
			is_enrolled = EXCLUDED.is_enrolled,
			enrolled_at = CASE WHEN EXCLUDED.is_enrolled AND iam_mfa_settings.enrolled_at IS NULL THEN now() ELSE iam_mfa_settings.enrolled_at END,
			updated_at = now()
	`, s.UserID, s.Method, s.Secret, s.IsEnrolled)
	return err
}

// DeleteSettings removes MFA settings.
func (r *MFARepository) DeleteSettings(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM iam_mfa_settings WHERE user_id = $1`, userID)
	return err
}

// ── Backup codes ──

// InsertBackupCodes bulk inserts backup codes.
func (r *MFARepository) InsertBackupCodes(ctx context.Context, codes []domain.MFABackupCode) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO iam_mfa_backup_codes (user_id, code_hash) VALUES ($1, $2)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range codes {
		if _, err := stmt.ExecContext(ctx, c.UserID, c.CodeHash); err != nil {
			return fmt.Errorf("insert backup code: %w", err)
		}
	}

	return tx.Commit()
}

// GetUnusedBackupCodes returns all unused backup codes for a user.
func (r *MFARepository) GetUnusedBackupCodes(ctx context.Context, userID string) ([]domain.MFABackupCode, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, code_hash, is_used, used_at, created_at
		FROM iam_mfa_backup_codes
		WHERE user_id = $1 AND is_used = false
		ORDER BY created_at
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []domain.MFABackupCode
	for rows.Next() {
		var c domain.MFABackupCode
		if err := rows.Scan(&c.ID, &c.UserID, &c.CodeHash, &c.IsUsed, &c.UsedAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// MarkBackupCodeUsed marks a backup code as used.
func (r *MFARepository) MarkBackupCodeUsed(ctx context.Context, codeID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE iam_mfa_backup_codes SET is_used = true, used_at = now() WHERE id = $1
	`, codeID)
	return err
}

// DeleteBackupCodes removes all backup codes for a user (e.g., on regenerate).
func (r *MFARepository) DeleteBackupCodes(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM iam_mfa_backup_codes WHERE user_id = $1`, userID)
	return err
}

// GetEnrolledUsersCount returns count of enrolled MFA users.
func (r *MFARepository) GetEnrolledUsersCount(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM iam_mfa_settings WHERE is_enrolled = true`).Scan(&count)
	return count, err
}

// GetUserIDS returns all user IDs that have MFA enrolled.
func (r *MFARepository) GetEnrolledUserIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT user_id FROM iam_mfa_settings WHERE is_enrolled = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
