package ardacrypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const BlindIndexPrefix = "bidx:v1:"

// BlindIndex generates a deterministic, one-way cryptographic hash of normalized plaintext
// using HKDF-SHA256 derived keys (RFC 5869).
// It allows exact search on encrypted columns in SQL (e.g. WHERE email_bidx = $1)
// without revealing the plaintext.
// Format: bidx:v1:<hex(HMAC-SHA256(lowercase(trim(plaintext)), salt))>
func BlindIndex(plaintext, salt string) string {
	normalized := strings.ToLower(strings.TrimSpace(plaintext))
	if normalized == "" || strings.TrimSpace(salt) == "" {
		return ""
	}

	key, err := DeriveKey(salt, "blind-index")
	if err != nil {
		return ""
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(normalized))
	hashBytes := mac.Sum(nil)

	return BlindIndexPrefix + hex.EncodeToString(hashBytes)
}

// BlindIndexExact generates a deterministic hash without case-folding normalization.
func BlindIndexExact(plaintext, salt string) string {
	trimmed := strings.TrimSpace(plaintext)
	if trimmed == "" || strings.TrimSpace(salt) == "" {
		return ""
	}

	key, err := DeriveKey(salt, "blind-index")
	if err != nil {
		return ""
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(trimmed))
	hashBytes := mac.Sum(nil)

	return BlindIndexPrefix + hex.EncodeToString(hashBytes)
}
