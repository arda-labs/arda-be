// Package resultid owns the grammar of sandbox result ids. It is a leaf
// package (no imports from the service) so the stores that mint ids, the
// renderWidget tool and the results endpoint can all share one definition:
// internal/sandbox imports internal/tools, so the tools package cannot import
// sandbox itself.
//
// An id is "<namespace>:<seq>" where the namespace is the conversation scope
// "tenant|actor|thread" and seq is the store's monotonically increasing
// counter in lowercase hex (see Mint). Valid accepts exactly that shape over a
// restricted ASCII charset, so an id can be embedded in a URL path segment
// without escaping surprises and can never carry a path separator, query,
// fragment, quote or control character.
package resultid

import "strconv"

// maxLen bounds an id: two UUIDs (72) + separators + a thread id + a 16-digit
// hex sequence fit comfortably; anything longer is not something a store mints.
const maxLen = 512

// maxSeqDigits is the width of a uint64 in hex.
const maxSeqDigits = 16

// Mint builds the id for the seq-th result stored under namespace.
func Mint(namespace string, seq uint64) string {
	return namespace + ":" + strconv.FormatUint(seq, 16)
}

// Valid reports whether id has the shape Mint produces over the allowed
// charset: ASCII letters, digits, '_', '-', '|' and ':', ending in
// ":<1-16 lowercase hex digits>", at most 512 bytes.
func Valid(id string) bool {
	if len(id) < 3 || len(id) > maxLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_', c == '-', c == '|', c == ':':
		default:
			return false
		}
	}
	colon := -1
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == ':' {
			colon = i
			break
		}
	}
	if colon < 1 { // missing, or an empty namespace
		return false
	}
	seq := id[colon+1:]
	if len(seq) == 0 || len(seq) > maxSeqDigits {
		return false
	}
	for i := 0; i < len(seq); i++ {
		c := seq[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
