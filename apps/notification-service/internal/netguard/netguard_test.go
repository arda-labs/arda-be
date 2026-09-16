package netguard

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateHostRejectsUnsafeTargets(t *testing.T) {
	tests := []struct {
		name string
		host string
	}{
		{name: "loopback ipv4", host: "127.0.0.1"},
		{name: "loopback ipv6", host: "::1"},
		{name: "loopback trailing dot", host: "127.0.0.1."},
		{name: "private 10/8", host: "10.0.0.1"},
		{name: "private 172.16/12", host: "172.16.0.5"},
		{name: "private 192.168/16", host: "192.168.1.10"},
		{name: "link local ipv4", host: "169.254.169.254"},
		{name: "link local ipv6", host: "fe80::1"},
		{name: "link local ipv6 with zone", host: "fe80::1%eth0"},
		{name: "cgnat", host: "100.64.0.1"},
		{name: "unspecified", host: "0.0.0.0"},
		{name: "metadata host", host: "metadata.google.internal"},
		{name: "metadata host suffix", host: "x.metadata.internal"},
		{name: "instance data", host: "instance-data"},
		{name: "localhost", host: "localhost"},
		{name: "localhost suffix", host: "api.localhost"},
		{name: "internal tld", host: "smtp.internal"},
		{name: "cluster dns", host: "redis.default.svc"},
		{name: "single label", host: "smtp-gateway"},
		{name: "decimal shorthand", host: "2130706433"},
		{name: "dotted shorthand", host: "127.1"},
		{name: "hex shorthand", host: "0x7f000001"},
		{name: "bracketed loopback", host: "[::1]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHost(tt.host)
			if err == nil {
				t.Fatalf("ValidateHost(%q) = nil, want error", tt.host)
			}
			if !errors.Is(err, ErrHostBlocked) {
				t.Fatalf("ValidateHost(%q) = %v, want ErrHostBlocked", tt.host, err)
			}
		})
	}
}

func TestValidateHostRequiresHost(t *testing.T) {
	for _, host := range []string{"", "   ", "."} {
		if err := ValidateHost(host); !errors.Is(err, ErrHostRequired) {
			t.Fatalf("ValidateHost(%q) = %v, want ErrHostRequired", host, err)
		}
	}
}

func TestValidateHostAcceptsPublicHosts(t *testing.T) {
	allowed := []string{
		"smtp.gmail.com",
		"email-smtp.us-east-1.amazonaws.com",
		"SMTP.EXAMPLE.COM",
		"smtp.example.com.",
		"93.184.216.34",
	}
	for _, host := range allowed {
		if err := ValidateHost(host); err != nil {
			t.Errorf("ValidateHost(%q) = %v, want nil", host, err)
		}
	}
}

func TestValidateHostTrimsWhitespace(t *testing.T) {
	trimmed := "smtp.example.com"
	if err := ValidateHost("  " + trimmed + "  "); err != nil {
		t.Fatalf("ValidateHost(%q) = %v, want nil", "  "+trimmed+"  ", err)
	}
	if got := normalizeHost("\tSMTP.Example.COM.\n"); got != trimmed {
		t.Fatalf("normalizeHost = %q, want %q", got, trimmed)
	}
	if !strings.HasSuffix(normalizeHost("smtp.example.com."), "example.com") {
		t.Fatalf("normalizeHost kept the root-label dot: %q", normalizeHost("smtp.example.com."))
	}
}
