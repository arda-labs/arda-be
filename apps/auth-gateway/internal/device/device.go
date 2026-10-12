// Package device derives a coarse, human-readable device description from a
// browser User-Agent so IAM can label sessions ("Chrome on Windows",
// "iPhone"). It is a best-effort classifier for display and device-tracking
// purposes only: nothing security-relevant may depend on its output, and the
// fingerprint it builds is a stable grouping key, not an identity proof.
package device

import (
	"fmt"
	"strings"
)

// Info is the parsed description of the client device.
type Info struct {
	// Type is "browser", "mobile" or "tablet".
	Type string
	// Name is the display label, for example "iPhone" or "Chrome on Windows".
	Name string
	// OS is "Windows", "iOS", "Android", "macOS", "Linux" or "Unknown".
	OS string
	// Browser is "Edge", "Opera", "Samsung Internet", "Chrome", "Firefox",
	// "Safari" or "Unknown".
	Browser string
}

// Parse classifies ua. An empty or unrecognised User-Agent still yields a
// complete Info ("browser", "Web Browser", "Unknown", "Unknown").
func Parse(ua string) Info {
	deviceType := parseType(ua)
	osName := parseOS(ua)
	browserName := parseBrowser(ua)
	return Info{
		Type:    deviceType,
		Name:    parseName(ua, deviceType, osName, browserName),
		OS:      osName,
		Browser: browserName,
	}
}

// Fingerprint builds the grouping key IAM stores on a device record: the
// User-Agent truncated to 128 bytes and the client IP with its last segment
// masked, so one browser on one subnet maps to one device.
func Fingerprint(userAgent, ip string) string {
	if len(userAgent) > 128 {
		userAgent = userAgent[:128]
	}
	return fmt.Sprintf("%s|%s", userAgent, maskIP(ip))
}

func maskIP(ip string) string {
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == '.' || ip[i] == ':' {
			return ip[:i] + ".0"
		}
	}
	return ip
}

func parseType(ua string) string {
	switch {
	case strings.Contains(ua, "iPad"), strings.Contains(ua, "Tablet"):
		return "tablet"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "Android"), strings.Contains(ua, "Mobile"):
		return "mobile"
	default:
		return "browser"
	}
}

func parseName(ua, deviceType, osName, browserName string) string {
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		if model := androidModel(ua); model != "" {
			return model
		}
		if deviceType == "tablet" {
			return "Android Tablet"
		}
		return "Android Phone"
	case browserName != "Unknown" && osName != "Unknown":
		return browserName + " on " + osName
	case osName != "Unknown":
		switch deviceType {
		case "mobile":
			return osName + " Phone"
		case "tablet":
			return osName + " Tablet"
		default:
			return osName + " Device"
		}
	default:
		return "Web Browser"
	}
}

func parseOS(ua string) string {
	switch {
	case strings.Contains(ua, "Windows NT"):
		return "Windows"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		return "iOS"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "Mac OS X"):
		return "macOS"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	default:
		return "Unknown"
	}
}

func parseBrowser(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/"):
		return "Edge"
	case strings.Contains(ua, "OPR/"):
		return "Opera"
	case strings.Contains(ua, "SamsungBrowser/"):
		return "Samsung Internet"
	case strings.Contains(ua, "CriOS/"):
		return "Chrome"
	case strings.Contains(ua, "Chrome/"):
		return "Chrome"
	case strings.Contains(ua, "Firefox/"):
		return "Firefox"
	case strings.Contains(ua, "Version/") && strings.Contains(ua, "Safari/"):
		return "Safari"
	default:
		return "Unknown"
	}
}

// androidModel extracts the device model from the parenthesised platform
// block of an Android User-Agent ("Mozilla/5.0 (Linux; Android 14; Pixel 8
// Build/...; wv) ..." -> "Pixel 8"). Platform tokens, the WebView marker and
// Chrome's reduced-UA placeholder "K" are skipped; without a model the
// result is empty and the caller falls back to a generic label.
func androidModel(ua string) string {
	platform := ua
	if start := strings.Index(ua, "("); start != -1 {
		if end := strings.Index(ua[start:], ")"); end != -1 {
			platform = ua[start+1 : start+end]
		}
	}
	for _, part := range strings.Split(platform, ";") {
		segment := strings.TrimSpace(part)
		switch {
		case segment == "":
			continue
		case strings.HasPrefix(segment, "Linux"):
			continue
		case strings.HasPrefix(segment, "Android"):
			continue
		case segment == "wv", segment == "K":
			continue
		}
		if idx := strings.Index(segment, " Build/"); idx != -1 {
			segment = segment[:idx]
		}
		if segment != "" {
			return segment
		}
	}
	return ""
}
