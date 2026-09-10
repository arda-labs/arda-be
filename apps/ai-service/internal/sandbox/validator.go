package sandbox

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// MaxScriptSizeBytes is the maximum allowed size for a script (16 KiB).
	MaxScriptSizeBytes = 16 * 1024
	// MaxOutputSizeBytes is the maximum allowed size for script return output (64 KiB).
	MaxOutputSizeBytes = 64 * 1024
)

var (
	ErrScriptTooLarge      = errors.New("ai.sandbox_script_too_large: script exceeds 16 KiB limit")
	ErrInvalidEncoding     = errors.New("ai.sandbox_invalid_encoding: script must be valid UTF-8 without null bytes")
	ErrForbiddenIdentifier = errors.New("ai.sandbox_forbidden_identifier: script contains restricted identifier")
)

// forbiddenTokens contains JavaScript identifiers and keywords that pose sandbox escape or pollution risks.
var forbiddenTokens = []string{
	"eval(",
	"eval ",
	"new Function",
	"Function(",
	"Function ",
	"__proto__",
	"constructor[",
	"Object.defineProperty",
	"Object.setPrototypeOf",
	"Object.getPrototypeOf",
	"Proxy(",
	"Proxy ",
	"new Proxy",
	"Reflect.",
	"globalThis",
	"process.",
	"require(",
	"require ",
	"import(",
	"import ",
	"window.",
	"document.",
	"setTimeout",
	"setInterval",
	"XMLHttpRequest",
	"WebSocket",
}

// forbiddenIdentifierPattern catches escapes that survive the literal token
// list, most importantly prototype-chain access like
// ({}).constructor.constructor("return process")() and bracket access such as
// ({}).__proto__ or Object["defineProperty"]. Identifiers are case-sensitive
// so the `function` keyword keeps working.
var forbiddenIdentifierPattern = regexp.MustCompile(
	`\b(__proto__|constructor|prototype|globalThis|process|require|module|exports|Reflect|Proxy|XMLHttpRequest|WebSocket|setTimeout|setInterval|setImmediate|clearTimeout|clearInterval|window|document|fetch|defineProperty|defineProperties|__defineGetter__|__defineSetter__|__lookupGetter__|__lookupSetter__)\b`,
)

// ValidateScript performs static pre-execution checks on the input code.
func ValidateScript(code string) error {
	trimmed := strings.TrimSpace(code)
	if len(trimmed) == 0 {
		return errors.New("ai.sandbox_empty_script: code cannot be empty")
	}

	if len(code) > MaxScriptSizeBytes {
		return ErrScriptTooLarge
	}

	if !utf8.ValidString(code) || strings.ContainsRune(code, 0) {
		return ErrInvalidEncoding
	}

	// Normalize spaces for token matching
	normalized := strings.ReplaceAll(code, "\t", " ")

	for _, token := range forbiddenTokens {
		if strings.Contains(normalized, token) {
			return fmt.Errorf("%w: '%s'", ErrForbiddenIdentifier, strings.TrimSpace(token))
		}
	}

	if match := forbiddenIdentifierPattern.FindString(code); match != "" {
		return fmt.Errorf("%w: '%s'", ErrForbiddenIdentifier, match)
	}

	return nil
}
