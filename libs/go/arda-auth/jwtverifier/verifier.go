package jwtverifier

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// MinSecretLength is the minimum HS256 HMAC secret length accepted by New.
// RFC 7518 requires an HMAC key at least as long as the hash output (256 bits).
const MinSecretLength = 32

// verificationLeeway tolerates a small clock skew between the token issuer and
// this verifier. It never lengthens a token's life beyond its `exp` claim.
const verificationLeeway = 30 * time.Second

// Verifier verifies JWT access tokens.
type Verifier struct {
	issuer   string
	audience string
	key      []byte
}

// New creates a verifier using a static HMAC secret. It fails fast when the
// secret is missing or weaker than MinSecretLength so a misconfigured
// deployment cannot silently accept tokens.
func New(issuer, audience, secret string) (*Verifier, error) {
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("jwtverifier: HS256 secret must be at least %d characters", MinSecretLength)
	}
	return &Verifier{
		issuer:   issuer,
		audience: audience,
		key:      []byte(secret),
	}, nil
}

// Claims holds the standard claims extracted from a token.
type Claims struct {
	Subject  string
	Issuer   string
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
		// Every token must carry an expiry; without this a token with no
		// `exp` claim would be accepted forever.
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(verificationLeeway),
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
