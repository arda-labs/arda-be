package knowledge

import (
	"bytes"
	"strings"
)

// ParseDocument extracts readable text/markdown from uploaded document bytes.
func ParseDocument(data []byte, filename string) string {
	ext := ""
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		ext = strings.ToLower(filename[idx+1:])
	}

	switch ext {
	case "md", "markdown", "txt", "text", "csv", "json":
		return string(data)
	default:
		// Attempt UTF-8 conversion, replacing invalid bytes
		return string(bytes.ToValidUTF8(data, []byte("")))
	}
}
