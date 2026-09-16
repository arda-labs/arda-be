package push

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/netguard"
)

const (
	// MaxEndpointLength bounds a stored push subscription endpoint.
	MaxEndpointLength = 2048
	// EndpointScheme is the only scheme accepted for web push endpoints. Plain
	// http is rejected: browsers never use it for push and accepting it would
	// turn the worker into an unauthenticated SSRF/plaintext channel.
	EndpointScheme = "https"
)

var (
	ErrEndpointRequired = errors.New("push endpoint is required")
	ErrEndpointTooLong  = errors.New("push endpoint is too long")
	ErrEndpointScheme   = errors.New("push endpoint must use https")
	ErrEndpointHost     = errors.New("push endpoint host is not allowed")
)

// ValidateEndpoint rejects web push endpoints that would let an authenticated
// user turn the notification service into an SSRF proxy. It requires https and
// blocks cloud metadata, loopback, link-local, private and CGNAT addresses as
// well as internal DNS suffixes (shared rules live in internal/netguard).
func ValidateEndpoint(endpoint string) error {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return ErrEndpointRequired
	}
	if len(trimmed) > MaxEndpointLength {
		return ErrEndpointTooLong
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("push endpoint is not a valid URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, EndpointScheme) {
		return ErrEndpointScheme
	}
	host := parsed.Hostname()
	if host == "" {
		return ErrEndpointHost
	}
	if err := netguard.ValidateHost(host); err != nil {
		return fmt.Errorf("%w: %v", ErrEndpointHost, err)
	}
	return nil
}
