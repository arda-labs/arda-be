package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	versionPrefix = "enc:v1:"
	nonceSizeBytes = 12 // Standard GCM nonce size
)

var (
	ErrInvalidCiphertext = errors.New("invalid or corrupted ciphertext")
	ErrKeyRequired       = errors.New("encryption key is required")
)

// deriveKey ensures the secret key is strictly 32 bytes (AES-256) using SHA-256.
func deriveKey(secret string) []byte {
	hash := sha256.Sum256([]byte(secret))
	return hash[:]
}

// Encrypt encrypts plaintext using AES-256-GCM with a cryptographically secure random nonce.
// Output format: enc:v1:<base64(nonce + ciphertext + tag)>
func Encrypt(plaintext, secret string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if strings.TrimSpace(secret) == "" {
		return "", ErrKeyRequired
	}

	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, nonceSizeBytes)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	// Seal appends ciphertext + 16-byte auth tag to nonce
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.RawURLEncoding.EncodeToString(sealed)

	return versionPrefix + encoded, nil
}

// Decrypt decrypts ciphertext previously encrypted with Encrypt.
// If input does not have the "enc:v1:" prefix, it is returned as-is (for legacy plaintext fallback).
func Decrypt(ciphertext, secret string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if !strings.HasPrefix(ciphertext, versionPrefix) {
		// Not encrypted (legacy plaintext)
		return ciphertext, nil
	}
	if strings.TrimSpace(secret) == "" {
		return "", ErrKeyRequired
	}

	raw := strings.TrimPrefix(ciphertext, versionPrefix)
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	if len(data) < nonceSizeBytes+16 { // nonce + minimum auth tag
		return "", ErrInvalidCiphertext
	}

	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := data[:nonceSizeBytes]
	ciphertextWithTag := data[nonceSizeBytes:]

	plaintext, err := gcm.Open(nil, nonce, ciphertextWithTag, nil)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	return string(plaintext), nil
}
