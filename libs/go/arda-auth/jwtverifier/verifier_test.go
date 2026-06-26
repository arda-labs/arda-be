package jwtverifier

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "super-secret-dev-key-change-in-production"

func TestVerifyValidToken(t *testing.T) {
	v := New("http://hydra.local", "arda-api", testSecret)
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

func TestVerifyExpiredToken(t *testing.T) {
	v := New("http://hydra.local", "arda-api", testSecret)
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

func TestVerifyWrongIssuer(t *testing.T) {
	v := New("http://hydra.local", "arda-api", testSecret)
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
