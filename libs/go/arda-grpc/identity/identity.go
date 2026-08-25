// Package identity implements the authenticated workload identity carried by
// Arda's internal gRPC calls. User/session metadata is separate and remains
// delegated context; this package authenticates the calling service itself.
package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	MetadataKey  = "x-service-auth"
	version      = "v1"
	maxClockSkew = 30 * time.Second
)

var (
	errInvalidToken  = errors.New("invalid service identity token")
	errMissingSecret = errors.New("service identity secret is required")
)

type claims struct {
	Version   string `json:"v"`
	Source    string `json:"src"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Nonce     string `json:"nonce"`
}

// Claims is the verified identity of an internal caller.
type Claims struct {
	Source    string
	Audience  string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// SecretFromEnv reads the one shared workload secret from the environment.
// Production manifests must provide it through a secret reference; silently
// disabling service authentication is intentionally not supported.
func SecretFromEnv() (string, error) {
	secret := strings.TrimSpace(os.Getenv("ARDA_SERVICE_AUTH_SECRET"))
	if secret == "" {
		return "", errMissingSecret
	}
	if len(secret) < 32 {
		return "", errors.New("service identity secret must be at least 32 characters")
	}
	return secret, nil
}

// Issue creates a short-lived signed identity assertion for source -> audience.
func Issue(secret, source, audience string, now time.Time, ttl time.Duration) (string, error) {
	secret = strings.TrimSpace(secret)
	source = strings.TrimSpace(source)
	audience = strings.TrimSpace(audience)
	if len(secret) < 32 {
		return "", errMissingSecret
	}
	if source == "" || audience == "" {
		return "", errors.New("service identity source and audience are required")
	}
	if ttl <= 0 || ttl > 5*time.Minute {
		return "", errors.New("service identity ttl must be between 0 and 5 minutes")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("generate service identity nonce: %w", err)
	}
	issuedAt := now.UTC()
	c := claims{
		Version:   version,
		Source:    source,
		Audience:  audience,
		IssuedAt:  issuedAt.Unix(),
		ExpiresAt: issuedAt.Add(ttl).Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(nonceBytes),
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal service identity claims: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := sign(secret, version+"."+encodedPayload)
	return version + "." + encodedPayload + "." + signature, nil
}

// Verify validates signature, audience and bounded lifetime of an assertion.
func Verify(token, secret, expectedAudience string, now time.Time) (Claims, error) {
	var zero Claims
	secret = strings.TrimSpace(secret)
	if len(secret) < 32 {
		return zero, errMissingSecret
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != version || parts[1] == "" || parts[2] == "" {
		return zero, errInvalidToken
	}
	expectedSignature := sign(secret, parts[0]+"."+parts[1])
	if subtle.ConstantTimeCompare([]byte(expectedSignature), []byte(parts[2])) != 1 {
		return zero, errInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return zero, errInvalidToken
	}
	var c claims
	if json.Unmarshal(payload, &c) != nil || c.Version != version || c.Source == "" || c.Audience == "" || c.Nonce == "" {
		return zero, errInvalidToken
	}
	if c.Audience != strings.TrimSpace(expectedAudience) {
		return zero, errInvalidToken
	}
	issuedAt := time.Unix(c.IssuedAt, 0)
	expiresAt := time.Unix(c.ExpiresAt, 0)
	current := now.UTC()
	if c.IssuedAt <= 0 || c.ExpiresAt <= c.IssuedAt || current.Before(issuedAt.Add(-maxClockSkew)) || !current.Before(expiresAt) {
		return zero, errInvalidToken
	}
	return Claims{Source: c.Source, Audience: c.Audience, IssuedAt: issuedAt, ExpiresAt: expiresAt}, nil
}

func sign(secret, value string) string {
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
