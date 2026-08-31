package svcclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

const testSecret = "01234567890123456789012345678901"

func TestNewRequest_SetsCallerAndSubject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Service-Auth") == "" {
			t.Error("missing x-service-auth header")
		}
		if r.Header.Get("X-Tenant-Id") != "t-1" {
			t.Errorf("X-Tenant-Id = %q, want t-1", r.Header.Get("X-Tenant-Id"))
		}
		if r.Header.Get("X-User-Id") != "u-1" {
			t.Errorf("X-User-Id = %q, want u-1", r.Header.Get("X-User-Id"))
		}
		if r.Header.Get("X-Auth-Checked") != "true" {
			t.Errorf("X-Auth-Checked = %q, want true", r.Header.Get("X-Auth-Checked"))
		}
		if r.Header.Get("X-Source-Service") != "ai-service" {
			t.Errorf("X-Source-Service = %q, want ai-service", r.Header.Get("X-Source-Service"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient("test-service", server.URL, "ai-service", testSecret, nil)
	md := metadata.Context{
		TenantID:    "t-1",
		UserID:      "u-1",
		ActorUserID: "u-1",
		AuthChecked: "true",
	}
	req, err := client.NewRequest(context.Background(), http.MethodGet, "/test", md)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
}

func TestDo_DecodesResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"id":"c1","name":"Test"}}`))
	}))
	defer server.Close()

	client := NewClient("test", server.URL, "ai-service", testSecret, nil)
	req, err := client.NewRequest(context.Background(), http.MethodGet, "/", metadata.Context{})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	var envelope struct {
		Result struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := client.Do(context.Background(), req, &envelope); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if envelope.Result.ID != "c1" || envelope.Result.Name != "Test" {
		t.Errorf("decoded = %+v, want {c1 Test}", envelope.Result)
	}
}

func TestDo_StatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient("test", server.URL, "ai-service", testSecret, nil)
	req, err := client.NewRequest(context.Background(), http.MethodGet, "/", metadata.Context{})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	err = client.Do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StatusError, got %T", err)
	}
	if se.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404", se.Status)
	}
}

func TestDo_BoundedResponse_ExceedsMax(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"` + strings.Repeat("x", 200) + `"}`))
	}))
	defer server.Close()

	client := NewClient("test", server.URL, "ai-service", testSecret, nil)
	client.MaxResponse = 50 // 50 bytes — response is ~200 bytes
	req, err := client.NewRequest(context.Background(), http.MethodGet, "/", metadata.Context{})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	err = client.Do(context.Background(), req, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected 'exceeds' error, got %v", err)
	}
}

func TestDo_GettRetry(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient("test", server.URL, "ai-service", testSecret, nil)
	req, err := client.NewRequest(context.Background(), http.MethodGet, "/", metadata.Context{})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := client.Do(context.Background(), req, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestDo_PostNoRetry(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient("test", server.URL, "ai-service", testSecret, nil)
	req, err := client.NewRequest(context.Background(), http.MethodPost, "/", metadata.Context{})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	err = client.Do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestDo_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient("test", server.URL, "ai-service", testSecret, nil)
	client.Timeout = 10 * time.Millisecond // very short
	req, err := client.NewRequest(context.Background(), http.MethodGet, "/", metadata.Context{})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	err = client.Do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
