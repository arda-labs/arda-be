package ardacrypto

import (
	"strings"
	"testing"
)

func TestAES256GCM_Roundtrip(t *testing.T) {
	secret := "super-secure-arda-secret-key-12345678"
	original := "sk-proj-test-api-key-998877665544332211"

	encrypted, err := Encrypt(original, secret)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if !strings.HasPrefix(encrypted, VersionPrefix) {
		t.Fatalf("expected enc:v1: prefix, got: %s", encrypted)
	}
	if encrypted == original {
		t.Fatal("encrypted text must not equal plaintext")
	}

	decrypted, err := Decrypt(encrypted, secret)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != original {
		t.Fatalf("expected '%s', got '%s'", original, decrypted)
	}
}

func TestAES256GCM_RandomNonce(t *testing.T) {
	secret := "secret-key"
	text := "my-secret-token"

	enc1, _ := Encrypt(text, secret)
	enc2, _ := Encrypt(text, secret)

	if enc1 == enc2 {
		t.Fatal("two encryptions of the same plaintext must yield different ciphertexts (random nonce)")
	}
}

func TestAES256GCM_WrongKeyRejects(t *testing.T) {
	secret1 := "correct-secret-key"
	secret2 := "wrong-secret-key"
	original := "secret-content"

	encrypted, _ := Encrypt(original, secret1)
	_, err := Decrypt(encrypted, secret2)

	if err == nil {
		t.Fatal("expected decrypt with wrong key to fail, got nil")
	}
}

func TestAES256GCM_TamperedCiphertext(t *testing.T) {
	secret := "secret-key"
	encrypted, _ := Encrypt("hello world", secret)

	// Tamper one byte in the base64 ciphertext
	tampered := encrypted[:len(encrypted)-2] + "AA"
	_, err := Decrypt(tampered, secret)

	if err == nil {
		t.Fatal("expected tamper detection to reject corrupted ciphertext")
	}
}

func TestAES256GCM_LegacyPlaintextFallback(t *testing.T) {
	secret := "secret-key"
	plain := "sk-legacy-unencrypted-key"

	decrypted, err := Decrypt(plain, secret)
	if err != nil {
		t.Fatalf("unexpected error for unencrypted string: %v", err)
	}
	if decrypted != plain {
		t.Fatalf("expected legacy plaintext '%s', got '%s'", plain, decrypted)
	}
}
