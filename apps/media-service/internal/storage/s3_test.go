package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newTestProvider(t *testing.T, cfg S3Config) *S3Provider {
	t.Helper()
	provider, err := NewS3Provider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewS3Provider: %v", err)
	}
	return provider
}

func TestPresignGetObjectUsesPublicEndpointForBrowser(t *testing.T) {
	provider := newTestProvider(t, S3Config{
		Endpoint:       "http://garage.platform.svc.cluster.local:3900",
		PublicEndpoint: "https://s3.arda.io.vn",
		Region:         "garage",
		AccessKey:      "test-access-key",
		SecretKey:      "test-secret-key",
		ForcePathStyle: true,
	})
	if !provider.BrowserPresignAvailable() {
		t.Fatal("BrowserPresignAvailable() = false, want true")
	}

	internal, err := provider.PresignGetObject(context.Background(), PresignGetInput{
		Bucket:    "media",
		Key:       "templates/contract.pdf",
		ExpiresIn: time.Minute,
	})
	if err != nil {
		t.Fatalf("PresignGetObject(internal): %v", err)
	}
	if !strings.HasPrefix(internal.URL, "http://garage.platform.svc.cluster.local:3900/media/templates/contract.pdf?") {
		t.Fatalf("internal URL = %q, want service endpoint", internal.URL)
	}

	browser, err := provider.PresignGetObject(context.Background(), PresignGetInput{
		Bucket:        "media",
		Key:           "templates/contract.pdf",
		ExpiresIn:     time.Minute,
		BrowserFacing: true,
	})
	if err != nil {
		t.Fatalf("PresignGetObject(browser): %v", err)
	}
	if !strings.HasPrefix(browser.URL, "https://s3.arda.io.vn/media/templates/contract.pdf?") {
		t.Fatalf("browser URL = %q, want public endpoint", browser.URL)
	}
	if !strings.Contains(browser.URL, "X-Amz-Signature") {
		t.Fatalf("browser URL = %q, want a signed URL", browser.URL)
	}
}

func TestPresignGetObjectFallsBackWithoutPublicEndpoint(t *testing.T) {
	provider := newTestProvider(t, S3Config{
		Endpoint:       "http://garage.platform.svc.cluster.local:3900",
		Region:         "garage",
		AccessKey:      "test-access-key",
		SecretKey:      "test-secret-key",
		ForcePathStyle: true,
	})
	if provider.BrowserPresignAvailable() {
		t.Fatal("BrowserPresignAvailable() = true, want false without public endpoint")
	}

	out, err := provider.PresignGetObject(context.Background(), PresignGetInput{
		Bucket:        "media",
		Key:           "templates/contract.pdf",
		ExpiresIn:     time.Minute,
		BrowserFacing: true,
	})
	if err != nil {
		t.Fatalf("PresignGetObject: %v", err)
	}
	if !strings.HasPrefix(out.URL, "http://garage.platform.svc.cluster.local:3900/media/templates/contract.pdf?") {
		t.Fatalf("URL = %q, want service endpoint fallback", out.URL)
	}
}

func TestPresignGetObjectIgnoresPublicEndpointEqualToService(t *testing.T) {
	provider := newTestProvider(t, S3Config{
		Endpoint:       "https://s3.arda.io.vn",
		PublicEndpoint: "https://s3.arda.io.vn/",
		Region:         "garage",
		AccessKey:      "test-access-key",
		SecretKey:      "test-secret-key",
		ForcePathStyle: true,
	})
	if provider.BrowserPresignAvailable() {
		t.Fatal("BrowserPresignAvailable() = true, want false when endpoints match")
	}
}
