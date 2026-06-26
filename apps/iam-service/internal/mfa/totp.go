package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// TOTPService generates and verifies TOTP codes.
type TOTPService struct {
	issuer string
}

// TOTPSecret holds a generated secret for enrollment.
type TOTPSecret struct {
	Secret   string `json:"secret"`
	OTPAuth  string `json:"otpauth_url"`
	QRImage  string `json:"qr_image_url,omitempty"`
}

// New creates a TOTP service.
func New(issuer string) *TOTPService {
	return &TOTPService{issuer: issuer}
}

// GenerateSecret creates a new TOTP secret for a user.
func (s *TOTPService) GenerateSecret(userID, username, email string) (*TOTPSecret, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}

	secretBase32 := strings.TrimRight(base32.StdEncoding.EncodeToString(secret), "=")

	// OTP Auth URL for QR code generation
	label := email
	if label == "" {
		label = username
	}
	otpauth := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		s.issuer, label, secretBase32, s.issuer)

	return &TOTPSecret{
		Secret:  secretBase32,
		OTPAuth: otpauth,
	}, nil
}

// Verify checks a TOTP code against a secret.
// Allows a 1-step time skew (30s before and after).
func (s *TOTPService) Verify(secretBase32, code string) (bool, error) {
	secret, err := base32.StdEncoding.DecodeString(padSecret(secretBase32))
	if err != nil {
		return false, fmt.Errorf("decode secret: %w", err)
	}

	now := time.Now().Unix() / 30

	// Check current, previous, and next window
	for i := -1; i <= 1; i++ {
		expected := computeOTP(secret, now+int64(i))
		if hmac.Equal([]byte(expected), []byte(code)) {
			return true, nil
		}
	}

	return false, nil
}

// IsEnrolled checks if a user has a TOTP secret configured.
// This is delegated to the caller — the service just generates/verifies.
func (s *TOTPService) IsEnrolled(secret string) bool {
	return secret != ""
}

func computeOTP(secret []byte, counter int64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	hash := mac.Sum(nil)

	offset := hash[len(hash)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7fffffff

	code := truncated % 1000000
	return fmt.Sprintf("%06d", code)
}

func padSecret(s string) string {
	// Add padding to make base32 decode happy
	switch len(s) % 8 {
	case 2:
		s += "======"
	case 4:
		s += "===="
	case 5:
		s += "==="
	case 7:
		s += "="
	}
	return s
}
