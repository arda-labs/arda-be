package service

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestResolveRejectsDangerousSniffedContent(t *testing.T) {
	policy := NewUploadPolicy(nil)
	html := []byte("<!DOCTYPE html><html><body><script>alert(1)</script></body></html>")

	_, err := policy.Resolve("image/png", "avatar.png", html)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("html content disguised as image/png: err = %v, want ErrInvalidInput", err)
	}

	_, err = policy.Resolve("image/jpeg", "photo.jpg", []byte(`<?xml version="1.0"?><root/>`))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("xml content disguised as image/jpeg: err = %v, want ErrInvalidInput", err)
	}
}

func TestValidateDeclaredRejectsDangerousTypesAndExtensions(t *testing.T) {
	policy := NewUploadPolicy(nil)
	for _, declared := range []string{"text/html", "image/svg+xml", "application/javascript", "text/xml", "application/xhtml+xml"} {
		if _, err := policy.ValidateDeclared(declared, "file.bin"); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("ValidateDeclared(%q) err = %v, want ErrInvalidInput", declared, err)
		}
	}
	if _, err := policy.ValidateDeclared("image/png", "payload.svg"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("dangerous extension: err = %v, want ErrInvalidInput", err)
	}
}

func TestResolveRejectsTypeOutsideAllowlist(t *testing.T) {
	policy := NewUploadPolicy(nil)
	_, err := policy.Resolve("application/x-msdownload", "tool.exe", []byte{0x4d, 0x5a, 0x90, 0x00})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("non allowlisted type: err = %v, want ErrInvalidInput", err)
	}
}

func TestResolveUsesCustomAllowlist(t *testing.T) {
	policy := NewUploadPolicy([]string{"image/png"})
	if _, err := policy.Resolve("image/png", "avatar.png", []byte("\x89PNG\r\n\x1a\n")); err != nil {
		t.Fatalf("allowlisted image/png rejected: %v", err)
	}
	if _, err := policy.Resolve("application/pdf", "doc.pdf", []byte("%PDF-1.7")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("pdf outside custom allowlist: err = %v, want ErrInvalidInput", err)
	}
}

func TestResolvePrefersSniffedTypeOnMismatch(t *testing.T) {
	policy := NewUploadPolicy(nil)
	resolved, err := policy.Resolve("image/png", "report.png", []byte("%PDF-1.7\n"))
	if err != nil {
		t.Fatalf("pdf disguised as png: %v", err)
	}
	if resolved != "application/pdf" {
		t.Fatalf("resolved = %q, want application/pdf", resolved)
	}
}

func TestResolveKeepsRicherDeclaredTypeForContainers(t *testing.T) {
	policy := NewUploadPolicy(nil)
	docx := append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0}, 32)...)
	resolved, err := policy.Resolve(
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"contract.docx",
		docx,
	)
	if err != nil {
		t.Fatalf("docx rejected: %v", err)
	}
	if resolved != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("resolved = %q, want declared docx type", resolved)
	}

	resolved, err = policy.Resolve("text/csv", "export.csv", []byte("a,b\n1,2\n"))
	if err != nil {
		t.Fatalf("csv rejected: %v", err)
	}
	if resolved != "text/csv" {
		t.Fatalf("resolved = %q, want text/csv", resolved)
	}
}

func TestResolveAcceptsPlainTextBusinessTypes(t *testing.T) {
	policy := NewUploadPolicy(nil)
	for _, tc := range []struct {
		declared string
		filename string
		head     []byte
		want     string
	}{
		{"text/markdown", "notes.md", []byte("# Title\n\nBody\n"), "text/markdown"},
		{"application/json", "form.json", []byte(`{"a":1}`), "application/json"},
		{"text/plain", "notes.txt", []byte("hello"), "text/plain"},
	} {
		resolved, err := policy.Resolve(tc.declared, tc.filename, tc.head)
		if err != nil {
			t.Errorf("Resolve(%q) err = %v", tc.declared, err)
			continue
		}
		if resolved != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.declared, resolved, tc.want)
		}
	}
}

func TestResolveAcceptsCommonMediaTypes(t *testing.T) {
	policy := NewUploadPolicy(nil)
	for _, tc := range []struct {
		declared string
		filename string
		head     []byte
	}{
		{"video/mp4", "clip.mp4", []byte("\x00\x00\x00\x18ftypisom")},
		{"audio/mpeg", "voice.mp3", []byte("ID3\x04\x00\x00")},
		{"image/heic", "photo.heic", []byte("\x00\x00\x00\x18ftypheic")},
	} {
		if _, err := policy.Resolve(tc.declared, tc.filename, tc.head); err != nil {
			t.Errorf("Resolve(%q) err = %v", tc.declared, err)
		}
	}
}

func TestReadSniffHeadPreservesBody(t *testing.T) {
	payload := bytes.Repeat([]byte("arda-media-"), 200)
	head, rest, err := ReadSniffHead(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadSniffHead: %v", err)
	}
	if len(head) != sniffLen {
		t.Fatalf("head length = %d, want %d", len(head), sniffLen)
	}
	combined, err := io.ReadAll(rest)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	if !bytes.Equal(combined, payload) {
		t.Fatal("ReadSniffHead lost or reordered bytes")
	}
}

func TestReadSniffHeadShortBody(t *testing.T) {
	payload := []byte("short body")
	head, rest, err := ReadSniffHead(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadSniffHead: %v", err)
	}
	if !bytes.Equal(head, payload) {
		t.Fatalf("head = %q, want %q", head, payload)
	}
	combined, err := io.ReadAll(rest)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	if !bytes.Equal(combined, payload) {
		t.Fatal("ReadSniffHead lost bytes for short body")
	}
}

func TestResolveKeepsDeclaredTypeForEmptyHead(t *testing.T) {
	policy := NewUploadPolicy(nil)
	resolved, err := policy.Resolve("image/png", "empty.png", nil)
	if err != nil {
		t.Fatalf("empty head rejected: %v", err)
	}
	if resolved != "image/png" {
		t.Fatalf("resolved = %q, want image/png", resolved)
	}
}

func TestCanServeInline(t *testing.T) {
	tests := map[string]bool{
		"image/png":                 true,
		"image/jpeg":                true,
		"application/pdf":           true,
		"text/plain":                true,
		"text/html":                 false,
		"image/svg+xml":             false,
		"application/javascript":    false,
		"application/octet-stream":  false,
		"text/csv":                  false,
		"application/zip":           false,
		"application/xhtml+xml":     false,
		"text/plain; charset=utf-8": true,
		"IMAGE/PNG":                 true,
		"":                          false,
		"application/x-msdownload":  false,
		"application/msword":        false,
		"image/gif; charset=binary": true,
		"application/pdf;version=1": true,
	}
	for contentType, want := range tests {
		if got := CanServeInline(contentType); got != want {
			t.Errorf("CanServeInline(%q) = %v, want %v", contentType, got, want)
		}
	}
}

func TestNormalizeContentType(t *testing.T) {
	tests := map[string]string{
		" Text/Plain; charset=utf-8": "text/plain",
		"image/PNG":                  "image/png",
		" application/pdf ":          "application/pdf",
		"not a type":                 "not a type",
		"":                           "",
	}
	for raw, want := range tests {
		if got := NormalizeContentType(raw); got != want {
			t.Errorf("NormalizeContentType(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestResolveAcceptsOctetStreamWithSpecificContent(t *testing.T) {
	policy := NewUploadPolicy(nil)
	resolved, err := policy.Resolve("application/octet-stream", "scan.pdf", []byte("%PDF-1.7\n"))
	if err != nil {
		t.Fatalf("octet-stream pdf rejected: %v", err)
	}
	if resolved != "application/pdf" {
		t.Fatalf("resolved = %q, want application/pdf", resolved)
	}

	// Unknown binary stays octet-stream (allowlisted) but is never inline.
	resolved, err = policy.Resolve("application/octet-stream", "blob.bin", []byte{0x00, 0x01, 0x02, 0x03})
	if err != nil {
		t.Fatalf("unknown binary rejected: %v", err)
	}
	if resolved != "application/octet-stream" {
		t.Fatalf("resolved = %q, want application/octet-stream", resolved)
	}
	if CanServeInline(resolved) {
		t.Fatal("application/octet-stream must not be inline safe")
	}
}

func TestResolveDoesNotDependOnFilenameExtension(t *testing.T) {
	policy := NewUploadPolicy(nil)
	// A dangerous extension is rejected before content sniffing.
	if _, err := policy.Resolve("text/plain", "note.xhtml", []byte("<p>hello</p>")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("xhtml extension: err = %v, want ErrInvalidInput", err)
	}
	// A misleading benign extension cannot salvage HTML content.
	_, err := policy.Resolve("text/plain", "note.txt", []byte(strings.Repeat("<html><body>x</body></html>", 40)))
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("html content with .txt name: err = %v, want ErrInvalidInput", err)
	}
}
