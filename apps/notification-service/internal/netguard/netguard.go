// Package netguard validates tenant-configured outbound destinations (web push
// endpoints, SMTP hosts) before notification-service dials them, so a tenant
// config cannot turn the service into an SSRF proxy into the cluster or the
// cloud metadata endpoint.
//
// Only the literal host is checked. A public hostname that resolves to an
// internal address (DNS rebinding) is not detected here, so network-level
// egress restrictions must remain the second line of defence.
package netguard

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

var (
	ErrHostRequired = errors.New("host is required")
	ErrHostBlocked  = errors.New("host is not a public destination")
)

var (
	// cgnatPrefix blocks carrier-grade NAT (100.64.0.0/10). net.IP.IsPrivate
	// does not cover it and it is a common path into internal infrastructure.
	cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

	// blockedHosts are cloud metadata endpoints and wildcard addresses that
	// never host a legitimate tenant-configured SMTP relay or push service.
	blockedHosts = []string{
		"169.254.169.254",
		"metadata.google.internal",
		"metadata.internal",
		"instance-data",
		"0.0.0.0",
	}

	// blockedHostSuffixes are internal DNS suffixes that never host a public
	// SMTP relay or push service.
	blockedHostSuffixes = []string{
		".local", ".localhost", ".internal", ".lan", ".svc", ".cluster.local", ".home.arpa",
	}
)

// ValidateHost rejects a bare hostname (no scheme, port or path) that points at
// loopback, link-local, private, CGNAT or unspecified addresses, intranet DNS
// names, metadata endpoints or numeric IPv4 shorthands ("127.1",
// "2130706433") that some resolvers still expand.
func ValidateHost(rawHost string) error {
	host := normalizeHost(rawHost)
	if host == "" {
		return ErrHostRequired
	}

	for _, blocked := range blockedHosts {
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return fmt.Errorf("%w: blocked host", ErrHostBlocked)
		}
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.WithZone("").Unmap()
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified() || ip.IsPrivate() || cgnatPrefix.Contains(ip) {
			return fmt.Errorf("%w: non-public address", ErrHostBlocked)
		}
		return nil
	}

	// Not an IP literal: reject intranet names and numeric IPv4 shorthands.
	if !strings.Contains(host, ".") {
		return fmt.Errorf("%w: intranet hostname", ErrHostBlocked)
	}
	for _, suffix := range blockedHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return fmt.Errorf("%w: intranet hostname", ErrHostBlocked)
		}
	}
	if isNumericIPv4Shorthand(host) {
		return fmt.Errorf("%w: numeric host shorthand", ErrHostBlocked)
	}
	return nil
}

// normalizeHost lowercases the input and drops an optional root-label dot, so
// "127.0.0.1." cannot dodge the literal-address checks.
func normalizeHost(rawHost string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(rawHost)), ".")
}

// isNumericIPv4Shorthand reports whether every dot-separated label is numeric
// (decimal or 0x-prefixed hex). Those hosts are never legitimate public
// services and some resolvers still interpret them as IPv4 addresses.
func isNumericIPv4Shorthand(host string) bool {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" {
			return false
		}
		base := 10
		digits := label
		if strings.HasPrefix(label, "0x") {
			base, digits = 16, label[2:]
		}
		if digits == "" {
			return false
		}
		if _, err := strconv.ParseUint(digits, base, 32); err != nil {
			return false
		}
	}
	return true
}
