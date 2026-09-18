package config

import "testing"

func TestLoadStoragePublicEndpointEnvAliases(t *testing.T) {
	t.Setenv("GARAGE_PUBLIC_ENDPOINT", "https://s3.arda.io.vn")
	cfg := Load()
	if cfg.StoragePublicEndpoint != "https://s3.arda.io.vn" {
		t.Fatalf("StoragePublicEndpoint = %q, want https://s3.arda.io.vn", cfg.StoragePublicEndpoint)
	}
}

func TestLoadStoragePublicEndpointGenericAliasWinsLast(t *testing.T) {
	t.Setenv("STORAGE_PUBLIC_ENDPOINT", "https://storage.example.test")
	cfg := Load()
	if cfg.StoragePublicEndpoint != "https://storage.example.test" {
		t.Fatalf("StoragePublicEndpoint = %q, want https://storage.example.test", cfg.StoragePublicEndpoint)
	}
}
