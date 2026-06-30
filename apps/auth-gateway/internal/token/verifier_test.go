package token

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWKSVerifierValidToken(t *testing.T) {
	key, jwksURL := testJWKS(t)
	verifier, err := New("jwks", "https://auth.local", "arda-api", "", jwksURL, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Subject:   "user-1",
		Issuer:    "https://auth.local",
		Audience:  jwt.ClaimStrings{"arda-api"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	})
	token.Header["kid"] = "test-key"
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := verifier.Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" {
		t.Fatalf("subject = %q", claims.Subject)
	}
}

func TestJWKSVerifierRejectsWrongAudience(t *testing.T) {
	key, jwksURL := testJWKS(t)
	verifier, err := New("jwks", "https://auth.local", "arda-api", "", jwksURL, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Subject:   "user-1",
		Issuer:    "https://auth.local",
		Audience:  jwt.ClaimStrings{"other-api"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	})
	token.Header["kid"] = "test-key"
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := verifier.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected wrong audience error")
	}
}

func testJWKS(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"use": "sig",
			"kid": "test-key",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(server.Close)
	return key, server.URL
}
