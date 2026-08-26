package ardacrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	VersionPrefix  = "enc:v1:"
	NonceSizeBytes = 12 // Standard NIST 96-bit nonce for AES-GCM
	KeySizeBytes   = 32 // 256-bit AES key
)

var (
	ErrInvalidCiphertext = errors.New("ardacrypto: invalid or corrupted ciphertext")
	ErrKeyRequired       = errors.New("ardacrypto: secret key is required")
)

// HKDF-SHA256 (RFC 5869) implementation

var hkdfSalt = []byte("arda-crypto-hkdf-salt-v1")

// hkdfExtract performs the HMAC-SHA256 extract step of RFC 5869.
func hkdfExtract(salt, ikm []byte) []byte {
	if len(salt) == 0 {
		salt = make([]byte, sha256.Size)
	}
	h := hmac.New(sha256.New, salt)
	h.Write(ikm)
	return h.Sum(nil)
}

// hkdfExpand performs the HMAC-SHA256 expand step of RFC 5869.
func hkdfExpand(prk, info []byte, length int) ([]byte, error) {
	if length > 255*sha256.Size {
		return nil, errors.New("ardacrypto: requested hkdf length exceeds maximum")
	}
	var okm []byte
	var t []byte
	counter := byte(1)
	for len(okm) < length {
		h := hmac.New(sha256.New, prk)
		h.Write(t)
		h.Write(info)
		h.Write([]byte{counter})
		t = h.Sum(nil)
		okm = append(okm, t...)
		counter++
	}
	return okm[:length], nil
}

// DeriveKey derives a cryptographically isolated 32-byte (256-bit) AES key using HKDF-SHA256 (RFC 5869).
func DeriveKey(secret, info string) ([]byte, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, ErrKeyRequired
	}
	prk := hkdfExtract(hkdfSalt, []byte(secret))
	return hkdfExpand(prk, []byte("arda-crypto-v1:"+info), KeySizeBytes)
}

// Encrypt encrypts plaintext using AES-256-GCM with a cryptographically secure random 12-byte nonce
// and HKDF-SHA256 key derivation (RFC 5869).
// Output format: enc:v1:<base64(nonce + ciphertext + 16-byte-auth-tag)>
func Encrypt(plaintext, secret string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if strings.TrimSpace(secret) == "" {
		return "", ErrKeyRequired
	}

	key, err := DeriveKey(secret, "data-encryption")
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("ardacrypto: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("ardacrypto: create gcm: %w", err)
	}

	nonce := make([]byte, NonceSizeBytes)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("ardacrypto: generate nonce: %w", err)
	}

	// Seal appends ciphertext + 16-byte auth tag to nonce
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.RawURLEncoding.EncodeToString(sealed)

	return VersionPrefix + encoded, nil
}

// Decrypt decrypts ciphertext previously encrypted with Encrypt.
// If input does not have the "enc:v1:" prefix, it is returned as-is (for legacy plaintext fallback).
func Decrypt(ciphertext, secret string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if !strings.HasPrefix(ciphertext, VersionPrefix) {
		// Legacy unencrypted plaintext fallback
		return ciphertext, nil
	}
	if strings.TrimSpace(secret) == "" {
		return "", ErrKeyRequired
	}

	raw := strings.TrimPrefix(ciphertext, VersionPrefix)
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	if len(data) < NonceSizeBytes+16 { // nonce + minimum 16-byte auth tag
		return "", ErrInvalidCiphertext
	}

	key, err := DeriveKey(secret, "data-encryption")
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("ardacrypto: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("ardacrypto: create gcm: %w", err)
	}

	nonce := data[:NonceSizeBytes]
	ciphertextWithTag := data[NonceSizeBytes:]

	plaintext, err := gcm.Open(nil, nonce, ciphertextWithTag, nil)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	return string(plaintext), nil
}
