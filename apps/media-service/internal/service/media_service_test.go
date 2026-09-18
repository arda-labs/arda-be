package service

import (
	"testing"

	"github.com/arda-labs/arda/apps/media-service/internal/config"
)

func TestMaxStreamBytesDefaultsToTwoMegabytes(t *testing.T) {
	svc := NewMediaService(config.Config{}, nil, nil)
	if got := svc.MaxStreamBytes(); got != 2*1024*1024 {
		t.Fatalf("MaxStreamBytes() = %d, want %d", got, 2*1024*1024)
	}
}

func TestMaxStreamBytesUsesConfiguredLimit(t *testing.T) {
	svc := NewMediaService(config.Config{StreamMaxSizeMB: 64}, nil, nil)
	if got := svc.MaxStreamBytes(); got != 64*1024*1024 {
		t.Fatalf("MaxStreamBytes() = %d, want %d", got, 64*1024*1024)
	}
}

func TestSupportsBrowserPresignWithoutProvider(t *testing.T) {
	svc := NewMediaService(config.Config{}, nil, nil)
	if svc.SupportsBrowserPresign() {
		t.Fatal("SupportsBrowserPresign() = true, want false without a provider")
	}
}
