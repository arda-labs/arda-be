package token

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/libs/go/arda-auth/introspection"
	"github.com/arda-labs/arda/libs/go/arda-auth/jwtverifier"
)

// Verifier abstracts JWT verification and token introspection.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (*jwtverifier.Claims, error)
}

// New creates a verifier based on the strategy.
// strategy can be "jwt" or "introspection".
func New(strategy, issuer, audience, secret, introspectionURL, introspectionClientID, introspectionClientSecret string) (Verifier, error) {
	switch strategy {
	case "jwt":
		return jwtverifier.New(issuer, audience, secret), nil
	case "introspection":
		if introspectionURL == "" || introspectionClientID == "" || introspectionClientSecret == "" {
			return nil, fmt.Errorf("introspection requires URL, client ID and client secret")
		}
		return &introspectionVerifier{
			client: introspection.New(introspectionURL, introspectionClientID, introspectionClientSecret),
		}, nil
	default:
		return nil, fmt.Errorf("unknown token strategy: %s", strategy)
	}
}

type introspectionVerifier struct {
	client *introspection.Client
}

func (v *introspectionVerifier) Verify(ctx context.Context, rawToken string) (*jwtverifier.Claims, error) {
	result, err := v.client.Introspect(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	return &jwtverifier.Claims{
		Subject:  result.Subject,
		Issuer:   result.Issuer,
		Audience: result.Audience,
	}, nil
}
