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

func TestLoadStreamMaxSizeMBEnv(t *testing.T) {
	t.Setenv("MEDIA_STREAM_MAX_SIZE_MB", "64")
	cfg := Load()
	if cfg.StreamMaxSizeMB != 64 {
		t.Fatalf("StreamMaxSizeMB = %d, want 64", cfg.StreamMaxSizeMB)
	}
}
