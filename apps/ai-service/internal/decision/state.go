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
	maxAssistantBytes = 1024
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
// production: user messages define intent; when available in a multi-turn
// conversation, the previous assistant message provides disambiguating context;
// the latest message over maxLatestBytes is never classified.
// ok=false means the caller must skip classification and keep the normal conversation path.
func BuildState(messages []Message) (string, bool) {
	var userIndices []int
	for i, message := range messages {
		if message.Role == "user" {
			userIndices = append(userIndices, i)
		}
	}
	if len(userIndices) == 0 {
		return "", false
	}
	latestIdx := userIndices[len(userIndices)-1]
	latest := messages[latestIdx].Content
	if len(latest) > maxLatestBytes {
		return "", false
	}
	state := "Latest user request: " + SanitizeTranscript(latest)

	if len(userIndices) > 1 {
		// In a multi-turn conversation, attach the previous assistant message if present
		// between the previous and latest user message to disambiguate follow-ups.
		for j := latestIdx - 1; j > userIndices[len(userIndices)-2]; j-- {
			if messages[j].Role == "assistant" && len(messages[j].Content) > 0 && len(messages[j].Content) <= maxAssistantBytes {
				state = "Previous assistant context: " + SanitizeTranscript(messages[j].Content) + "\n" + state
				break
			}
		}

		prevUserIdx := userIndices[len(userIndices)-2]
		previous := messages[prevUserIdx].Content
		if len(previous) <= maxPreviousBytes {
			state = "Previous user request (context only): " + SanitizeTranscript(previous) + "\n" + state
		}
	}
	if len(state) > maxStateBytes {
		return "", false
	}
	return state, true
}
