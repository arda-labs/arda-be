package ardahttp

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var (
	ErrEgressURLRequired = errors.New("ardahttp: egress URL is required")
	ErrInvalidScheme     = errors.New("ardahttp: only http and https protocols are allowed")
	ErrBlockedHost       = errors.New("ardahttp: destination host is blocked (SSRF protection)")
)

// ValidateEgressURL checks whether an outbound URL is safe to call from the backend,
// blocking access to cloud metadata services (e.g. 169.254.169.254) and private networks
// unless allowLocal is explicitly enabled (for development/testing).
func ValidateEgressURL(rawURL string, allowLocal bool) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ErrEgressURLRequired
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("ardahttp: invalid URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ErrInvalidScheme
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ErrEgressURLRequired
	}

	// 1. Explicit cloud metadata and internal domains blocklist
	blockedDomains := []string{
		"169.254.169.254",
		"metadata.google.internal",
		"metadata.internal",
		"instance-data",
		"0.0.0.0",
	}
	for _, blocked := range blockedDomains {
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return ErrBlockedHost
		}
	}

	// 2. If host is an IP address, check against private/loopback/link-local ranges
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return ErrBlockedHost
		}
		if ip.IsLoopback() {
			if !allowLocal {
				return ErrBlockedHost
			}
			return nil
		}
		if ip.IsPrivate() {
			if !allowLocal {
				return ErrBlockedHost
			}
			return nil
		}
	}

	// 3. If localhost domain
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		if !allowLocal {
			return ErrBlockedHost
		}
	}

	return nil
}
