package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")

func requestHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal idempotency request: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func verifyIdempotentReplay(existingHash, requestHash string) error {
	if existingHash == "" || existingHash != requestHash {
		return ErrIdempotencyConflict
	}
	return nil
}
