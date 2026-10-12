package device

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want Info
	}{
		{
			name: "empty",
			ua:   "",
			want: Info{Type: "browser", Name: "Web Browser", OS: "Unknown", Browser: "Unknown"},
		},
		{
			name: "chrome on windows",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
			want: Info{Type: "browser", Name: "Chrome on Windows", OS: "Windows", Browser: "Chrome"},
		},
		{
			name: "edge on windows",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36 Edg/128.0.0.0",
			want: Info{Type: "browser", Name: "Edge on Windows", OS: "Windows", Browser: "Edge"},
		},
		{
			name: "safari on macos",
			ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
			want: Info{Type: "browser", Name: "Safari on macOS", OS: "macOS", Browser: "Safari"},
		},
		{
			name: "firefox on linux",
			ua:   "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0",
			want: Info{Type: "browser", Name: "Firefox on Linux", OS: "Linux", Browser: "Firefox"},
		},
		{
			name: "iphone safari",
			ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
			want: Info{Type: "mobile", Name: "iPhone", OS: "iOS", Browser: "Safari"},
		},
		{
			name: "iphone chrome",
			ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/128.0.0.0 Mobile/15E148 Safari/604.1",
			want: Info{Type: "mobile", Name: "iPhone", OS: "iOS", Browser: "Chrome"},
		},
		{
			name: "ipad",
			ua:   "Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
			want: Info{Type: "tablet", Name: "iPad", OS: "iOS", Browser: "Safari"},
		},
		{
			name: "android phone with model",
			ua:   "Mozilla/5.0 (Linux; Android 14; Pixel 8 Build/AP2A.240805.005; wv) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Mobile Safari/537.36",
			want: Info{Type: "mobile", Name: "Pixel 8", OS: "Android", Browser: "Chrome"},
		},
		{
			name: "android samsung browser",
			ua:   "Mozilla/5.0 (Linux; Android 13; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/24.0 Chrome/117.0.0.0 Mobile Safari/537.36",
			want: Info{Type: "mobile", Name: "SM-S918B", OS: "Android", Browser: "Samsung Internet"},
		},
		{
			// Chrome's reduced User-Agent replaces the model with "K".
			name: "android reduced user agent",
			ua:   "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Mobile Safari/537.36",
			want: Info{Type: "mobile", Name: "Android Phone", OS: "Android", Browser: "Chrome"},
		},
		{
			// Every segment is a platform token, so no model can be extracted
			// and the generic label applies.
			name: "android tablet without model",
			ua:   "Linux; Android 12 Tablet",
			want: Info{Type: "tablet", Name: "Android Tablet", OS: "Android", Browser: "Unknown"},
		},
		{
			name: "android phone without model",
			ua:   "Linux; Android 12 Mobile",
			want: Info{Type: "mobile", Name: "Android Phone", OS: "Android", Browser: "Unknown"},
		},
		{
			name: "opera on windows",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/128.0.0.0 Safari/537.36 OPR/114.0.0.0",
			want: Info{Type: "browser", Name: "Opera on Windows", OS: "Windows", Browser: "Opera"},
		},
		{
			name: "known os unknown browser",
			ua:   "curl-like (Windows NT 10.0)",
			want: Info{Type: "browser", Name: "Windows Device", OS: "Windows", Browser: "Unknown"},
		},
		{
			name: "mobile hint with known os unknown browser",
			ua:   "Something (Windows NT 10.0) Mobile",
			want: Info{Type: "mobile", Name: "Windows Phone", OS: "Windows", Browser: "Unknown"},
		},
		{
			name: "tablet hint with known os unknown browser",
			ua:   "Something (Windows NT 10.0) Tablet",
			want: Info{Type: "tablet", Name: "Windows Tablet", OS: "Windows", Browser: "Unknown"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse(tt.ua); got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.ua, got, tt.want)
			}
		})
	}
}

func TestFingerprint(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		ip   string
		want string
	}{
		{name: "ipv4 last octet masked", ua: "ua", ip: "203.0.113.42", want: "ua|203.0.113.0"},
		{name: "ipv6 last group masked", ua: "ua", ip: "2001:db8::1", want: "ua|2001:db8:.0"},
		{name: "no separator kept verbatim", ua: "ua", ip: "localhost", want: "ua|localhost"},
		{name: "empty ip", ua: "ua", ip: "", want: "ua|"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Fingerprint(tt.ua, tt.ip); got != tt.want {
				t.Fatalf("Fingerprint(%q, %q) = %q, want %q", tt.ua, tt.ip, got, tt.want)
			}
		})
	}
	longUA := strings.Repeat("a", 200)
	if got := Fingerprint(longUA, "10.0.0.1"); got != strings.Repeat("a", 128)+"|10.0.0.0" {
		t.Fatalf("long user agent not truncated to 128 bytes: %q", got)
	}
}
