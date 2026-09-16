package jwtverifier

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testSecret must satisfy New's MinSecretLength fail-fast check.
const testSecret = "unit-test-secret-only-0123456789abcdef"

func newTestVerifier(t *testing.T) *Verifier {
	t.Helper()
	v, err := New("http://hydra.local", "arda-api", testSecret)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	return v
}

func TestNewRejectsWeakSecret(t *testing.T) {
	if _, err := New("http://hydra.local", "arda-api", ""); err == nil {
		t.Fatal("expected error for empty secret")
	}
	if _, err := New("http://hydra.local", "arda-api", "too-short"); err == nil {
		t.Fatal("expected error for secret shorter than MinSecretLength")
	}
	if _, err := New("http://hydra.local", "arda-api", "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("expected 32-character secret to be accepted: %v", err)
	}
}

func TestVerifyValidToken(t *testing.T) {
	v := newTestVerifier(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   "dev-admin-sub",
		Issuer:    "http://hydra.local",
		Audience:  jwt.ClaimStrings{"arda-api"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	raw, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	claims, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("verify valid token: %v", err)
	}
	if claims.Subject != "dev-admin-sub" {
		t.Fatalf("unexpected subject: %s", claims.Subject)
	}
}

func TestVerifyRejectsTokenWithoutExpiration(t *testing.T) {
	v := newTestVerifier(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:  "dev-admin-sub",
		Issuer:   "http://hydra.local",
		Audience: jwt.ClaimStrings{"arda-api"},
	})
	raw, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := v.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected error for token without exp claim")
	}
}

func TestVerifyExpiredToken(t *testing.T) {
	v := newTestVerifier(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   "dev-admin-sub",
		Issuer:    "http://hydra.local",
		Audience:  jwt.ClaimStrings{"arda-api"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	})
	raw, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := v.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestVerifyAcceptsTokenWithinLeeway(t *testing.T) {
	v := newTestVerifier(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   "dev-admin-sub",
		Issuer:    "http://hydra.local",
		Audience:  jwt.ClaimStrings{"arda-api"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-10 * time.Second)),
	})
	raw, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := v.Verify(context.Background(), raw); err != nil {
		t.Fatalf("expected token within leeway to verify: %v", err)
	}
}

func TestVerifyWrongIssuer(t *testing.T) {
	v := newTestVerifier(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   "dev-admin-sub",
		Issuer:    "http://evil.local",
		Audience:  jwt.ClaimStrings{"arda-api"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	raw, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := v.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestExtractBearer(t *testing.T) {
	if got := ExtractBearer("Bearer abc123"); got != "abc123" {
		t.Fatalf("unexpected bearer: %s", got)
	}
	if got := ExtractBearer("Basic abc123"); got != "" {
		t.Fatalf("expected empty, got: %s", got)
	}
}
