package ardadoc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMediaUploaderUpload(t *testing.T) {
	var captured struct {
		auth        http.Header
		contentType string
		body        string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/media" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		captured.auth = r.Header.Clone()
		captured.contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		captured.body = string(body)

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":  map[string]any{"public_id": "mf123"},
			"success": true,
		})
	}))
	defer server.Close()

	uploader, err := NewMediaUploader(server.URL)
	if err != nil {
		t.Fatalf("NewMediaUploader: %v", err)
	}
	src := http.Header{}
	src.Set("X-Tenant-Id", "tenant-1")
	src.Set("X-Org-Id", "org-1")
	src.Set("X-User-Id", "user-1")

	publicID, err := uploader.Upload(context.Background(), src, "voucher.pdf", "application/pdf", "loan", []byte("%PDF"))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if publicID != "mf123" {
		t.Errorf("publicID = %q", publicID)
	}
	for name, want := range map[string]string{
		"X-Tenant-Id": "tenant-1",
		"X-Org-Id":    "org-1",
		"X-User-Id":   "user-1",
	} {
		if captured.auth.Get(name) != want {
			t.Errorf("header %s = %q, want %q", name, captured.auth.Get(name), want)
		}
	}
	if _, v := parseMultipartField(t, captured.contentType, captured.body, "module"); v != "loan" {
		t.Errorf("module = %q", v)
	}
	filename, v := parseMultipartField(t, captured.contentType, captured.body, "files")
	if filename != "voucher.pdf" || v != "%PDF" {
		t.Errorf("file part = %q,%q", filename, v)
	}
}

func TestMediaUploaderAttach(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/media/files/attach" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":  map[string]any{"attached": 1},
			"success": true,
		})
	}))
	defer server.Close()

	uploader, _ := NewMediaUploader(server.URL)
	src := http.Header{}
	src.Set("X-Tenant-Id", "tenant-1")
	src.Set("X-Org-Id", "org-1")

	if err := uploader.Attach(context.Background(), src, []string{"mf123"}, "business_case", "case-9"); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if gotBody["owner_type"] != "business_case" || gotBody["owner_id"] != "case-9" {
		t.Errorf("attach body = %v", gotBody)
	}
	ids, _ := gotBody["public_ids"].([]any)
	if len(ids) != 1 || ids[0] != "mf123" {
		t.Errorf("public_ids = %v", gotBody["public_ids"])
	}
}

func TestMediaUploaderAttachValidation(t *testing.T) {
	uploader, _ := NewMediaUploader("http://localhost:1")
	if err := uploader.Attach(context.Background(), nil, nil, "business_case", "case-9"); err == nil {
		t.Fatal("expected error for empty public_ids")
	}
	if err := uploader.Attach(context.Background(), nil, []string{"mf"}, " ", "case-9"); err == nil {
		t.Fatal("expected error for blank owner_type")
	}
}

func TestMediaUploaderUploadErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"title":"forbidden"}`))
	}))
	defer server.Close()

	uploader, _ := NewMediaUploader(server.URL)
	_, err := uploader.Upload(context.Background(), http.Header{}, "f.pdf", "application/pdf", "loan", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}
