package main

import (
	"testing"

	"github.com/arda-labs/arda/apps/media-service/internal/config"
)

func TestStorageConfigFromWiresPublicEndpoint(t *testing.T) {
	cfg := config.Config{
		StorageEndpoint:       "http://garage.platform.svc.cluster.local:3900",
		StoragePublicEndpoint: "https://s3.arda.io.vn",
		StorageRegion:         "garage",
		StorageAccessKey:      "key",
		StorageSecretKey:      "secret",
		StorageForcePathStyle: true,
	}
	got := storageConfigFrom(cfg)
	if got.PublicEndpoint != "https://s3.arda.io.vn" {
		t.Fatalf("PublicEndpoint = %q, want https://s3.arda.io.vn", got.PublicEndpoint)
	}
	if got.Endpoint != cfg.StorageEndpoint {
		t.Fatalf("Endpoint = %q, want %q", got.Endpoint, cfg.StorageEndpoint)
	}
}
