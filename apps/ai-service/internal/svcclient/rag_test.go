package svcclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

func TestRAGSearch_SendsSignedAndDelegatedHeaders(t *testing.T) {
	var gotBody RAGQueryRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("X-Service-Auth") == "" {
			t.Error("missing x-service-auth header")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if r.Header.Get("X-Source-Service") != "ai-service" {
			t.Errorf("X-Source-Service = %q, want ai-service", r.Header.Get("X-Source-Service"))
		}
		if r.Header.Get("X-Tenant-Id") != "t-1" {
			t.Errorf("X-Tenant-Id = %q, want t-1", r.Header.Get("X-Tenant-Id"))
		}
		if r.Header.Get("X-User-Id") != "u-1" {
			t.Errorf("X-User-Id = %q, want u-1", r.Header.Get("X-User-Id"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-1","hits":[],"latency_ms":12,"rewritten":false,"retrieved_count":0,"reranked_count":0}`))
	}))
	defer server.Close()

	client := NewRAGClient(server.URL, "ai-service", testSecret, nil)
	md := metadata.Context{TenantID: "t-1", UserID: "u-1", ActorUserID: "u-1", AuthChecked: "true"}
	resp, err := client.Search(context.Background(), md, "phép năm", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.RunID != "run-1" {
		t.Errorf("RunID = %q, want run-1", resp.RunID)
	}
	if gotBody.Query != "phép năm" || gotBody.TopK != 3 {
		t.Errorf("body = %+v, want {phép năm 3}", gotBody)
	}
}

func TestRAGSearch_DecodesHits(t *testing.T) {
	payload := `{"run_id":"run-2","hits":[{"source_id":7,"source_version_id":9,"version":"v1","title":"T","heading":"H","content":"C","score":0.5,"citation":"[7:H]"}],"latency_ms":42,"rewritten":false,"retrieved_count":3,"reranked_count":1}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	client := NewRAGClient(server.URL, "ai-service", testSecret, nil)
	resp, err := client.Search(context.Background(), metadata.Context{TenantID: "t-1"}, "nghỉ phép", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.LatencyMs != 42 || resp.RetrievedCount != 3 || resp.RerankedCount != 1 {
		t.Errorf("response meta = %+v, want latency 42 retrieved 3 reranked 1", resp)
	}
	if len(resp.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(resp.Hits))
	}
	h := resp.Hits[0]
	if h.SourceID != 7 || h.SourceVersionID != 9 || h.Score != 0.5 || h.Citation != "[7:H]" || h.Title != "T" {
		t.Errorf("hit = %+v, want {7 9 T H C 0.5 [7:H]}", h)
	}
}

func TestRAGSearch_StatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewRAGClient(server.URL, "ai-service", testSecret, nil)
	_, err := client.Search(context.Background(), metadata.Context{TenantID: "t-1"}, "x", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StatusError, got %T", err)
	}
	if se.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500", se.Status)
	}
}

func TestRAGSearch_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":`)) // truncated JSON
	}))
	defer server.Close()

	client := NewRAGClient(server.URL, "ai-service", testSecret, nil)
	_, err := client.Search(context.Background(), metadata.Context{TenantID: "t-1"}, "x", 1)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestRAGSearch_ValidatesQuery(t *testing.T) {
	client := NewRAGClient("http://unused", "ai-service", testSecret, nil)
	if _, err := client.Search(context.Background(), metadata.Context{TenantID: "t-1"}, "   ", 1); err == nil {
		t.Fatal("expected error for blank query, got nil")
	}
}

func TestRAGFeedback_SendsSignedAndDelegatedHeaders(t *testing.T) {
	var gotBody FeedbackRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("X-Service-Auth") == "" {
			t.Error("missing x-service-auth header")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if r.Header.Get("X-Source-Service") != "ai-service" {
			t.Errorf("X-Source-Service = %q, want ai-service", r.Header.Get("X-Source-Service"))
		}
		if r.Header.Get("X-Tenant-Id") != "t-1" {
			t.Errorf("X-Tenant-Id = %q, want t-1", r.Header.Get("X-Tenant-Id"))
		}
		if r.Header.Get("X-User-Id") != "u-1" {
			t.Errorf("X-User-Id = %q, want u-1", r.Header.Get("X-User-Id"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"fb-1","run_id":"run-1","helpful":true,"comment":"Great answer","created_at":"2026-09-03T12:00:00Z"}`))
	}))
	defer server.Close()

	client := NewRAGClient(server.URL, "ai-service", testSecret, nil)
	md := metadata.Context{TenantID: "t-1", UserID: "u-1", ActorUserID: "u-1", AuthChecked: "true"}
	out, err := client.Feedback(context.Background(), md, "run-1", true, "Great answer")
	if err != nil {
		t.Fatalf("Feedback: %v", err)
	}
	if out.ID != "fb-1" || out.RunID != "run-1" || out.Helpful != true || out.Comment != "Great answer" {
		t.Errorf("FeedbackOut = %+v", out)
	}
	if gotBody.RunID != "run-1" || gotBody.Helpful != true || gotBody.Comment != "Great answer" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestRAGFeedback_ValidatesRunID(t *testing.T) {
	client := NewRAGClient("http://unused", "ai-service", testSecret, nil)
	if _, err := client.Feedback(context.Background(), metadata.Context{TenantID: "t-1"}, "", true, ""); err == nil {
		t.Fatal("expected error for blank runID, got nil")
	}
	// long runID (over 64 chars) should also error
	long := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := client.Feedback(context.Background(), metadata.Context{TenantID: "t-1"}, long, true, ""); err == nil {
		t.Fatal("expected error for long runID, got nil")
	}
}
