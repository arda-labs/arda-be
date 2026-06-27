package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/mfa"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
)

// MFAConfig controls MFA enforcement.
type MFAConfig struct {
	Required         bool
	GracePeriod      time.Duration
	Methods          []string
	RememberDevice   bool
	RememberDuration time.Duration
	BackupCodeCount  int
}

var DefaultMFAConfig = MFAConfig{
	Required:         false,
	GracePeriod:      7 * 24 * time.Hour,
	Methods:          []string{"totp"},
	RememberDevice:   true,
	RememberDuration: 30 * 24 * time.Hour,
	BackupCodeCount:  10,
}

// MFAService orchestrates MFA enrollment, verification, and backup codes.
type MFAService struct {
	mfaRepo    *repository.MFARepository
	sessionSvc *SessionService
	totp       *mfa.TOTPService
	cfg        MFAConfig
	logger     *slog.Logger
}

// NewMFAService creates an MFA service.
func NewMFAService(mfaRepo *repository.MFARepository, sessionSvc *SessionService, totp *mfa.TOTPService, cfg MFAConfig) *MFAService {
	return &MFAService{
		mfaRepo:    mfaRepo,
		sessionSvc: sessionSvc,
		totp:       totp,
		cfg:        cfg,
		logger:     slog.Default(),
	}
}

// GenerateSecret generates a TOTP secret for enrollment.
func (s *MFAService) GenerateSecret(ctx context.Context, userID, username, email string) (*mfa.TOTPSecret, error) {
	secret, err := s.totp.GenerateSecret(userID, username, email)
	if err != nil {
		return nil, err
	}

	settings := &domain.MFASettings{
		UserID:     userID,
		Method:     "totp",
		Secret:     secret.Secret,
		IsEnrolled: false,
	}
	if err := s.mfaRepo.UpsertSettings(ctx, settings); err != nil {
		return nil, fmt.Errorf("save settings: %w", err)
	}

	return secret, nil
}

// VerifyAndEnroll confirms the TOTP code and activates MFA.
func (s *MFAService) VerifyAndEnroll(ctx context.Context, userID, code string) ([]string, error) {
	settings, err := s.mfaRepo.GetSettings(ctx, userID)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		return nil, fmt.Errorf("no pending enrollment, generate a secret first")
	}
	if settings.IsEnrolled {
		return nil, fmt.Errorf("already enrolled in MFA")
	}

	ok, err := s.totp.Verify(settings.Secret, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("invalid TOTP code")
	}

	settings.IsEnrolled = true
	if err := s.mfaRepo.UpsertSettings(ctx, settings); err != nil {
		return nil, err
	}

	codes, err := s.generateBackupCodes(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("generate backup codes: %w", err)
	}

	s.logger.Info("mfa enrolled", "user_id", userID)
	return codes, nil
}

// MFAResult is returned during login when MFA is required.
type MFAResult struct {
	RequiresMFA bool     `json:"requiresMfa"`
	MFAToken    string   `json:"mfaToken,omitempty"`
	CanUseMFA   bool     `json:"canUseMfa"`
	Methods     []string `json:"methods"`
}

func (s *MFAService) IssueLoginChallenge(ctx context.Context, userID, loginChallenge string) (string, error) {
	settings, err := s.mfaRepo.GetSettings(ctx, userID)
	if err != nil {
		return "", err
	}
	if settings == nil || !settings.IsEnrolled {
		return "", fmt.Errorf("MFA not enrolled")
	}

	exp := time.Now().UTC().Add(5 * time.Minute).Unix()
	payload := strings.Join([]string{userID, loginChallenge, strconv.FormatInt(exp, 10)}, "\n")
	signature := hmacSHA256Hex([]byte(settings.Secret), payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + signature, nil
}

func (s *MFAService) VerifyLoginChallenge(ctx context.Context, userID, loginChallenge, token, code string) error {
	settings, err := s.mfaRepo.GetSettings(ctx, userID)
	if err != nil {
		return err
	}
	if settings == nil || !settings.IsEnrolled {
		return fmt.Errorf("MFA not enrolled")
	}

	payload, signature, ok := strings.Cut(token, ".")
	if !ok || payload == "" || signature == "" {
		return fmt.Errorf("invalid MFA challenge")
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return fmt.Errorf("invalid MFA challenge")
	}
	expectedSignature := hmacSHA256Hex([]byte(settings.Secret), string(rawPayload))
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return fmt.Errorf("invalid MFA challenge")
	}

	parts := strings.Split(string(rawPayload), "\n")
	if len(parts) != 3 || parts[0] != userID || parts[1] != loginChallenge {
		return fmt.Errorf("invalid MFA challenge")
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().UTC().Unix() > exp {
		return fmt.Errorf("MFA challenge expired")
	}

	okCode, err := s.totp.Verify(settings.Secret, code)
	if err != nil {
		return err
	}
	if !okCode {
		return fmt.Errorf("invalid MFA code")
	}
	return nil
}

// CheckMFA checks if user has MFA enrolled and returns the verification requirement.
func (s *MFAService) CheckMFA(ctx context.Context, userID, deviceToken string) (*MFAResult, error) {
	settings, err := s.mfaRepo.GetSettings(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := &MFAResult{
		RequiresMFA: false,
		CanUseMFA:   false,
		Methods:     s.cfg.Methods,
	}

	if settings == nil || !settings.IsEnrolled {
		return result, nil
	}

	if s.cfg.RememberDevice && deviceToken != "" {
		dev, _ := s.sessionSvc.GetDeviceByToken(ctx, userID, deviceToken)
		if s.sessionSvc.IsDeviceTrusted(dev) {
			return result, nil
		}
	}

	result.RequiresMFA = true
	result.CanUseMFA = true
	return result, nil
}

// RememberDevice trusts the current browser profile for the configured remember duration.
func (s *MFAService) RememberDevice(ctx context.Context, userID, deviceToken string) (*domain.Device, error) {
	if !s.cfg.RememberDevice {
		return nil, nil
	}
	return s.sessionSvc.RememberDevice(ctx, userID, deviceToken, time.Now().Add(s.cfg.RememberDuration))
}

// VerifyCode verifies a TOTP code and returns a temporary MFA token.
func (s *MFAService) VerifyCode(ctx context.Context, userID, code string) error {
	settings, err := s.mfaRepo.GetSettings(ctx, userID)
	if err != nil {
		return err
	}
	if settings == nil || !settings.IsEnrolled {
		return fmt.Errorf("MFA not enrolled")
	}

	ok, err := s.totp.Verify(settings.Secret, code)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("invalid MFA code")
	}

	_ = settings
	return nil
}

// VerifyBackupCode verifies and consumes a backup code.
func (s *MFAService) VerifyBackupCode(ctx context.Context, userID, code string) error {
	codes, err := s.mfaRepo.GetUnusedBackupCodes(ctx, userID)
	if err != nil {
		return err
	}

	codeHash := sha256Hex(code)
	for _, c := range codes {
		if c.CodeHash == codeHash {
			if err := s.mfaRepo.MarkBackupCodeUsed(ctx, c.ID); err != nil {
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("invalid or already used backup code")
}

// ResetMFA removes MFA enrollment for a user (admin only).
func (s *MFAService) ResetMFA(ctx context.Context, userID string) error {
	if err := s.mfaRepo.DeleteSettings(ctx, userID); err != nil {
		return err
	}
	if err := s.mfaRepo.DeleteBackupCodes(ctx, userID); err != nil {
		return err
	}
	s.logger.Info("mfa reset by admin", "user_id", userID)
	return nil
}

// IsEnrolled returns whether a user has MFA enrolled.
func (s *MFAService) IsEnrolled(ctx context.Context, userID string) (bool, error) {
	settings, err := s.mfaRepo.GetSettings(ctx, userID)
	if err != nil {
		return false, err
	}
	return settings != nil && settings.IsEnrolled, nil
}

// GetSettings returns MFA settings (for user self-service).
func (s *MFAService) GetSettings(ctx context.Context, userID string) (*domain.MFASettings, error) {
	return s.mfaRepo.GetSettings(ctx, userID)
}

func (s *MFAService) generateBackupCodes(ctx context.Context, userID string) ([]string, error) {
	if err := s.mfaRepo.DeleteBackupCodes(ctx, userID); err != nil {
		return nil, err
	}

	count := s.cfg.BackupCodeCount
	if count <= 0 {
		count = 10
	}

	plainCodes := make([]string, count)
	dbCodes := make([]domain.MFABackupCode, count)

	for i := 0; i < count; i++ {
		code := generateRandomCode(12)
		plainCodes[i] = code
		dbCodes[i] = domain.MFABackupCode{
			UserID:   userID,
			CodeHash: sha256Hex(code),
		}
	}

	if err := s.mfaRepo.InsertBackupCodes(ctx, dbCodes); err != nil {
		return nil, err
	}

	return plainCodes, nil
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func hmacSHA256Hex(key []byte, data string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func generateRandomCode(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return formatCode(base64.RawURLEncoding.EncodeToString(b))
}

func formatCode(code string) string {
	clean := strings.ToUpper(code[:12])
	var parts []string
	for i := 0; i < 12; i += 4 {
		parts = append(parts, clean[i:i+4])
	}
	return strings.Join(parts, "-")
}
