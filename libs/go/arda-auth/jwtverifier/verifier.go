package jwtverifier

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Verifier verifies JWT access tokens.
type Verifier struct {
	issuer   string
	audience string
	key      []byte
}

// New creates a verifier using a static HMAC secret.
func New(issuer, audience, secret string) *Verifier {
	return &Verifier{
		issuer:   issuer,
		audience: audience,
		key:      []byte(secret),
	}
}

// Claims holds the standard claims extracted from a token.
type Claims struct {
	Subject string
	Issuer  string
	Audience []string
}

// Verify validates a raw JWT and returns its subject.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	if rawToken == "" {
		return nil, fmt.Errorf("token is empty")
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)

	token, err := parser.Parse(rawToken, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return v.key, nil
	})
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}

	mc, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims format")
	}

	sub, err := mc.GetSubject()
	if err != nil {
		return nil, fmt.Errorf("missing subject: %w", err)
	}

	iss, _ := mc.GetIssuer()

	aud, err := mc.GetAudience()
	if err != nil {
		aud = []string{}
	}

	return &Claims{
		Subject:  sub,
		Issuer:   iss,
		Audience: aud,
	}, nil
}

// ExtractBearer pulls the token from an Authorization: Bearer <token> header.
func ExtractBearer(header string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(header, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(header, prefix))
	}
	return ""
}
