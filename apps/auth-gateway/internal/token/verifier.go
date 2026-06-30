package token

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-auth/introspection"
	"github.com/arda-labs/arda/libs/go/arda-auth/jwtverifier"
	"github.com/golang-jwt/jwt/v5"
)

// Verifier abstracts JWT verification and token introspection.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (*jwtverifier.Claims, error)
}

// New creates a verifier based on the strategy.
// strategy can be "jwt", "jwks", or "introspection".
func New(strategy, issuer, audience, secret, jwksURL, introspectionURL, introspectionClientID, introspectionClientSecret string) (Verifier, error) {
	switch strategy {
	case "jwt":
		return jwtverifier.New(issuer, audience, secret), nil
	case "jwks":
		if jwksURL == "" {
			return nil, fmt.Errorf("jwks requires URL")
		}
		return newJWKSVerifier(issuer, audience, jwksURL), nil
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

type jwksVerifier struct {
	issuer   string
	audience string
	jwksURL  string
	client   *http.Client

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
}

func newJWKSVerifier(issuer, audience, jwksURL string) *jwksVerifier {
	return &jwksVerifier{
		issuer:   issuer,
		audience: audience,
		jwksURL:  jwksURL,
		client:   &http.Client{Timeout: 5 * time.Second},
		keys:     make(map[string]*rsa.PublicKey),
	}
}

func (v *jwksVerifier) Verify(ctx context.Context, rawToken string) (*jwtverifier.Claims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)
	token, err := parser.Parse(rawToken, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("missing kid")
		}
		key, err := v.key(ctx, kid)
		if err != nil {
			return nil, err
		}
		return key, nil
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
	return &jwtverifier.Claims{Subject: sub, Issuer: iss, Audience: aud}, nil
}

func (v *jwksVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key := v.keys[kid]
	fresh := time.Now().Before(v.expires)
	v.mu.RUnlock()
	if key != nil && fresh {
		return key, nil
	}
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if key = v.keys[kid]; key != nil {
		return key, nil
	}
	return nil, fmt.Errorf("jwks key not found: %s", kid)
}

func (v *jwksVerifier) refresh(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if time.Now().Before(v.expires) && len(v.keys) > 0 {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch jwks: HTTP %d", resp.StatusCode)
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Use string `json:"use"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, jwk := range set.Keys {
		if jwk.Kty != "RSA" || jwk.Kid == "" || jwk.N == "" || jwk.E == "" {
			continue
		}
		if jwk.Alg != "" && jwk.Alg != "RS256" {
			continue
		}
		key, err := rsaKey(jwk.N, jwk.E)
		if err != nil {
			continue
		}
		keys[jwk.Kid] = key
	}
	if len(keys) == 0 {
		return fmt.Errorf("jwks has no usable RSA keys")
	}
	v.keys = keys
	// ponytail: fixed JWKS TTL; upgrade to Cache-Control/max-stale when key rotation policy is finalized.
	v.expires = time.Now().Add(time.Hour)
	return nil
}

func rsaKey(nRaw, eRaw string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nRaw)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eRaw)
	if err != nil {
		return nil, err
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}
	if e == 0 {
		return nil, fmt.Errorf("invalid exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
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
