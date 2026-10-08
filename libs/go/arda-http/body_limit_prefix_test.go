package ardahttp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// readAllLimited drains a request body, surfacing the *http.MaxBytesError that
// the backstop produces.
func readAllLimited(r *http.Request) (int64, error) {
	return io.Copy(io.Discard, r.Body)
}

func TestBodyLimitFor(t *testing.T) {
	overrides := []BodyLimitOverride{
		{Prefix: "/api/media", MaxBytes: MaxUploadBodyBytes},
		{Prefix: "/api/ai/rag", MaxBytes: MaxRAGBodyBytes},
	}
	const def = DefaultMaxBodyBytes

	cases := []struct {
		name string
		path string
		want int64
	}{
		{"json route uses the default", "/api/finance/postings", def},
		{"media upload gets the raised budget", "/api/media/files", MaxUploadBodyBytes},
		{"media public route also raised", "/api/media/public/abc/download", MaxUploadBodyBytes},
		{"rag gets its own budget", "/api/ai/rag/sources", MaxRAGBodyBytes},
		{"a prefix that only looks similar is not matched", "/api/mediax/files", def},
		{"longest prefix wins", "/api/media", MaxUploadBodyBytes},
		{"empty path uses the default", "", def},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bodyLimitFor(tc.path, def, overrides); got != tc.want {
				t.Fatalf("bodyLimitFor(%q) = %d, want %d", tc.path, got, tc.want)
			}
		})
	}
}

// A non-positive override must degrade to the default rather than removing the
// limit, so a config typo cannot silently reopen the DoS.
func TestBodyLimitForIgnoresNonPositiveOverride(t *testing.T) {
	overrides := []BodyLimitOverride{{Prefix: "/api/media", MaxBytes: 0}}
	if got := bodyLimitFor("/api/media/files", DefaultMaxBodyBytes, overrides); got != DefaultMaxBodyBytes {
		t.Fatalf("bodyLimitFor = %d, want the default %d", got, DefaultMaxBodyBytes)
	}
}

func TestLimitBodyMiddlewareRejectsOversizeContentLength(t *testing.T) {
	var reached bool
	h := LimitBodyMiddlewareByPrefix(DefaultMaxBodyBytes, nil,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))

	body := bytes.Repeat([]byte("a"), int(DefaultMaxBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/api/finance/postings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if reached {
		t.Fatal("handler ran despite an oversize Content-Length")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}
	var problem struct {
		Code   string `json:"code"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not a problem document: %v", err)
	}
	if problem.Code != ardaerrors.CodeBodyTooLarge {
		t.Fatalf("code = %q, want %q", problem.Code, ardaerrors.CodeBodyTooLarge)
	}
	if problem.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status field = %d, want 413", problem.Status)
	}
}

func TestLimitBodyMiddlewareAdmitsBodyAtTheLimit(t *testing.T) {
	var reached bool
	h := LimitBodyMiddlewareByPrefix(DefaultMaxBodyBytes, nil,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = true
			if _, err := readAllLimited(r); err != nil {
				t.Errorf("reading a body exactly at the limit failed: %v", err)
			}
		}))

	req := httptest.NewRequest(http.MethodPost, "/api/finance/postings",
		bytes.NewReader(bytes.Repeat([]byte("a"), int(DefaultMaxBodyBytes))))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if !reached {
		t.Fatal("handler did not run for a body exactly at the limit")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// A chunked request declares no length, so only the MaxBytesReader backstop can
// stop it. This is the case a Content-Length check alone would miss.
func TestLimitBodyMiddlewareCapsChunkedBodyMidStream(t *testing.T) {
	h := LimitBodyMiddlewareByPrefix(DefaultMaxBodyBytes, nil,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength >= 0 {
				t.Fatalf("expected an undeclared length, got %d", r.ContentLength)
			}
			if _, err := readAllLimited(r); err == nil {
				t.Fatal("reading past the limit succeeded; the backstop is not applied")
			}
		}))

	// A body larger than the limit with no declared length.
	oversize := io.MultiReader(
		strings.NewReader(""),
		bytes.NewReader(bytes.Repeat([]byte("a"), int(DefaultMaxBodyBytes)+4096)),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/finance/postings", oversize)
	req.ContentLength = -1
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
}

func TestRecoveryMiddlewareReturnsCataloguedInternalError(t *testing.T) {
	h := RecoveryMiddleware(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		}))

	req := httptest.NewRequest(http.MethodGet, "/api/finance/postings", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not a problem document: %v", err)
	}
	if problem.Code != ardaerrors.CodeInternal {
		t.Fatalf("code = %q, want %q", problem.Code, ardaerrors.CodeInternal)
	}
	// The panic value must never reach the client.
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatal("the panic value leaked into the response body")
	}
}

func TestRecoveryMiddlewarePassesThroughNormalResponses(t *testing.T) {
	h := RecoveryMiddleware(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusTeapot || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q, want 418 %q", rec.Code, rec.Body.String(), "ok")
	}
}
