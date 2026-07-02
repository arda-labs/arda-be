package handler

import (
	"testing"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
)

func TestRedirectURIFromRequestURL(t *testing.T) {
	got, err := redirectURIFromRequestURL("https://auth.arda.io.vn/oauth2/auth?client_id=arda-shell&redirect_uri=http%3A%2F%2Flocalhost%3A5000%2Fcallback")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://localhost:5000/callback" {
		t.Fatalf("redirect URI = %q", got)
	}
}

func TestOriginFromURL(t *testing.T) {
	got, err := originFromURL("https://arda.io.vn/callback")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://arda.io.vn" {
		t.Fatalf("origin = %q", got)
	}
}

func TestAllowedOAuthRedirectURI(t *testing.T) {
	h := &BFFHandler{cfg: config.Config{
		OAuthRedirectURI:  "https://arda.io.vn/callback",
		OAuthRedirectURIs: "https://arda.io.vn/callback, http://localhost:5000/callback",
	}}
	if !h.isAllowedOAuthRedirectURI("http://localhost:5000/callback") {
		t.Fatal("local redirect URI should be allowed")
	}
	if h.isAllowedOAuthRedirectURI("https://evil.example/callback") {
		t.Fatal("unknown redirect URI should be denied")
	}
}

func TestPKCEChallenge(t *testing.T) {
	got := pkceChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
}

func TestSafeReturnTo(t *testing.T) {
	tests := map[string]string{
		"/finance?tab=1":             "/finance?tab=1",
		"":                           "/",
		"https://evil.example/steal": "/",
		"//evil.example/steal":       "/",
		"settings/appearance":        "/",
	}
	for input, want := range tests {
		if got := safeReturnTo(input); got != want {
			t.Fatalf("safeReturnTo(%q) = %q, want %q", input, got, want)
		}
	}
}
