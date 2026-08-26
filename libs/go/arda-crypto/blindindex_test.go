package ardacrypto

import (
	"context"
	"strings"
	"testing"
)

func TestBlindIndex_Deterministic(t *testing.T) {
	salt := "company-salt-12345"
	email1 := "User@Example.COM"
	email2 := "user@example.com  "

	idx1 := BlindIndex(email1, salt)
	idx2 := BlindIndex(email2, salt)

	if !strings.HasPrefix(idx1, BlindIndexPrefix) {
		t.Fatalf("expected bidx:v1: prefix, got: %s", idx1)
	}
	if idx1 != idx2 {
		t.Fatalf("expected normalized blind index to match, got %s vs %s", idx1, idx2)
	}

	differentEmail := "other@example.com"
	idx3 := BlindIndex(differentEmail, salt)
	if idx1 == idx3 {
		t.Fatal("different emails must produce different blind indices")
	}
}

func TestStaticKeyProvider(t *testing.T) {
	provider := NewStaticKeyProvider("default-secret")
	ctx := context.Background()

	key, err := provider.GetSecretKey(ctx, "default")
	if err != nil || key != "default-secret" {
		t.Fatalf("expected 'default-secret', got '%s' (err: %v)", key, err)
	}

	provider.SetKey("tenant-123", "tenant-secret-999")
	tenantKey, err := provider.GetSecretKey(ctx, "tenant-123")
	if err != nil || tenantKey != "tenant-secret-999" {
		t.Fatalf("expected 'tenant-secret-999', got '%s' (err: %v)", tenantKey, err)
	}

	_, err = provider.GetSecretKey(ctx, "unknown-key")
	if err != ErrKeyNotFound {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}
