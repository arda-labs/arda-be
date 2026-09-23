package decision

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Classification budgets for the routing state. They mirror the production
// behavior: a latest user message over maxLatestBytes is never classified
// (truncated intent must not be routed), the previous user message only
// disambiguates references and is dropped when oversized.
const (
	maxLatestBytes    = 4096
	maxPreviousBytes  = 2048
	maxSanitizedBytes = 16 * 1024
	maxStateBytes     = 8192
)

// Message is the minimal conversation shape BuildState needs.
type Message struct {
	Role    string
	Content string
}

var transcriptSecretPattern = regexp.MustCompile(`(?i)(bearer\s+[^\s,;]+|(?:authorization|arda_sid|arda_did)\s*[:=]\s*(?:bearer\s+)?[^\s,;]+)`)

// SanitizeTranscript trims, redacts credential-shaped tokens and bounds the
// result. It is the single implementation used by both the production routing
// path and the evaluation runners.
func SanitizeTranscript(value string) string {
	value = strings.TrimSpace(value)
	value = transcriptSecretPattern.ReplaceAllString(value, "[REDACTED]")
	return TruncateRunes(value, maxSanitizedBytes)
}

// TruncateRunes cuts a string at maxBytes without splitting a multi-byte
// character: a byte-slice cut through Vietnamese text produces invalid UTF-8,
// which model providers reject and Postgres refuses to store.
func TruncateRunes(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

// BuildState builds the System One state string for one run, mirroring
// production exactly: only user messages are considered, the latest message
// over maxLatestBytes is never classified, and at most one previous user
// message is attached as context. ok=false means the caller must skip
// classification and keep the normal conversation path.
func BuildState(messages []Message) (string, bool) {
	var userMessages []string
	for _, message := range messages {
		if message.Role == "user" {
			userMessages = append(userMessages, message.Content)
		}
	}
	if len(userMessages) == 0 {
		return "", false
	}
	latest := userMessages[len(userMessages)-1]
	if len(latest) > maxLatestBytes {
		return "", false
	}
	state := "Latest user request: " + SanitizeTranscript(latest)
	if len(userMessages) > 1 {
		previous := userMessages[len(userMessages)-2]
		if len(previous) <= maxPreviousBytes {
			state = "Previous user request (context only): " + SanitizeTranscript(previous) + "\n" + state
		}
	}
	if len(state) > maxStateBytes {
		return "", false
	}
	return state, true
}
