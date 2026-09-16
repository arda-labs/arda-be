package push

import (
	"strings"
	"testing"
)

func TestValidateEndpointRejectsUnsafeTargets(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{name: "empty", endpoint: ""},
		{name: "plain http", endpoint: "http://example.com/push"},
		{name: "file scheme", endpoint: "file:///etc/passwd"},
		{name: "cloud metadata", endpoint: "https://169.254.169.254/latest/meta-data/"},
		{name: "gce metadata host", endpoint: "https://metadata.google.internal/computeMetadata/v1/"},
		{name: "loopback ipv4", endpoint: "https://127.0.0.1/push"},
		{name: "loopback ipv6", endpoint: "https://[::1]/push"},
		{name: "private 10/8", endpoint: "https://10.0.0.1/push"},
		{name: "private 172.16/12", endpoint: "https://172.16.0.5/push"},
		{name: "private 192.168/16", endpoint: "https://192.168.1.10/push"},
		{name: "link local ipv6", endpoint: "https://[fe80::1]/push"},
		{name: "link local ipv6 with zone", endpoint: "https://[fe80::1%25eth0]/push"},
		{name: "cgnat start", endpoint: "https://100.64.0.1/push"},
		{name: "cgnat end", endpoint: "https://100.127.255.254/push"},
		{name: "localhost", endpoint: "https://localhost/push"},
		{name: "internal tld", endpoint: "https://push.internal/sub"},
		{name: "cluster dns", endpoint: "https://redis.default.svc/push"},
		{name: "single label", endpoint: "https://push-gateway/push"},
		{name: "decimal shorthand", endpoint: "https://2130706433/push"},
		{name: "dotted shorthand", endpoint: "https://127.1/push"},
		{name: "octal-looking shorthand", endpoint: "https://0177.0.0.1/push"},
		{name: "hex shorthand", endpoint: "https://0x7f000001/push"},
		{name: "too long", endpoint: "https://" + strings.Repeat("a", MaxEndpointLength) + ".example.com/push"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateEndpoint(tt.endpoint); err == nil {
				t.Fatalf("ValidateEndpoint(%q) = nil, want error", tt.endpoint)
			}
		})
	}
}

func TestValidateEndpointAcceptsPublicHTTPSProviders(t *testing.T) {
	allowed := []string{
		"https://fcm.googleapis.com/fcm/send/abc123",
		"https://updates.push.services.mozilla.com/wpush/v2/xyz",
		"https://wns2-par02p.notify.windows.com/w/?token=abc",
		"https://web.push.apple.com/QG9uZw",
		" https://fcm.googleapis.com/fcm/send/trimmed ",
	}
	for _, endpoint := range allowed {
		if err := ValidateEndpoint(endpoint); err != nil {
			t.Errorf("ValidateEndpoint(%q) = %v, want nil", endpoint, err)
		}
	}
}
