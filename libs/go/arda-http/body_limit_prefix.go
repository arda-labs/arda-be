package ardahttp

import (
	"net/http"
	"strconv"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// Body size budgets.
//
// These are not arbitrary. Each one is the smallest value that still admits the
// largest legitimate payload that service accepts, so turning the limit on
// cannot silently break a working feature:
//
//   - media-service accepts uploads up to upload_max_size_mb (100) plus 1MiB of
//     slack, and media uploads traverse auth-gateway's proxy on the way in.
//   - ai-service buffers a 32MiB RAG knowledge document.
//   - workflow-service parses 10MiB of BPMN/definition uploads.
//   - finance-service parses an 8MiB XLSX posting sheet.
//   - everything else exchanges JSON documents, where 1MiB is already generous.
//
// A limit is only useful if it is enforced before the body is buffered. The
// middleware therefore rejects an over-sized Content-Length eagerly, and keeps
// http.MaxBytesReader underneath for requests that arrive without a declared
// length (chunked transfer encoding), which is the case a Content-Length check
// alone would miss.

// DefaultMaxBodyBytes is the budget for services that only exchange JSON.
const DefaultMaxBodyBytes int64 = 1 << 20 // 1MiB

// MaxUploadBodyBytes matches media-service's MaxUploadBytes: upload_max_size_mb
// (100) plus the 1MiB of slack that helper adds.
const MaxUploadBodyBytes int64 = 101 << 20

// MaxRAGBodyBytes matches ai-service's ParseMultipartForm(32 << 20).
const MaxRAGBodyBytes int64 = 33 << 20

// MaxWorkflowBodyBytes matches workflow-service's ParseMultipartForm(10 << 20).
const MaxWorkflowBodyBytes int64 = 11 << 20

// MaxPostingSheetBodyBytes matches finance-service's ParseMultipartForm(8 << 20).
const MaxPostingSheetBodyBytes int64 = 9 << 20

// BodyLimitOverride raises the budget for a path prefix that legitimately
// carries a larger payload than the service default.
type BodyLimitOverride struct {
	// Prefix is matched against r.URL.Path. The longest matching prefix wins, so
	// a specific route can be carved out of a broad one.
	Prefix string
	// MaxBytes is the budget for that prefix. A non-positive value is ignored,
	// matching LimitRequestBody, so a misconfigured override degrades to the
	// default instead of removing the limit.
	MaxBytes int64
}

// LimitBodyMiddlewareByPrefix caps the request body at defaultLimit, raising it
// for any path matching an override.
//
// Two layers, because either alone is bypassable:
//
//   - Content-Length is checked before the handler runs, so a declared oversize
//     body is refused with 413 without ever being buffered.
//   - http.MaxBytesReader still caps the reader, so a chunked request with no
//     declared length is cut off mid-stream.
//
// Exceeding the limit yields *http.MaxBytesError from the reader, which a
// handler that decodes JSON will usually report as a 400. The eager
// Content-Length rejection is what makes the common case report 413 instead.
func LimitBodyMiddlewareByPrefix(defaultLimit int64, overrides []BodyLimitOverride, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := bodyLimitFor(r.URL.Path, defaultLimit, overrides)
		if limit <= 0 {
			next.ServeHTTP(w, r)
			return
		}
		if r.ContentLength > limit {
			w.Header().Set("Connection", "close")
			WriteProblem(w, r, http.StatusRequestEntityTooLarge, bodyTooLargeError(limit))
			return
		}
		LimitRequestBody(w, r, limit)
		next.ServeHTTP(w, r)
	})
}

// bodyLimitFor resolves the budget for a path. Exported for tests and for the
// CI invariant that keeps service budgets and upload sizes in agreement.
func bodyLimitFor(path string, defaultLimit int64, overrides []BodyLimitOverride) int64 {
	limit := defaultLimit
	bestLen := -1
	for _, o := range overrides {
		if o.MaxBytes <= 0 || o.Prefix == "" {
			continue
		}
		if !pathHasPrefix(path, o.Prefix) {
			continue
		}
		// Longest prefix wins, so "/api/media/public" beats "/api/media".
		if len(o.Prefix) > bestLen {
			bestLen = len(o.Prefix)
			limit = o.MaxBytes
		}
	}
	return limit
}

// pathHasPrefix matches on a path-segment boundary. A bare strings.HasPrefix
// would let "/api/mediax/..." inherit the 100MB upload budget from
// "/api/media": over-granting, and it hides intent, because a route added later
// under a similar name would silently get an upload-sized limit.
func pathHasPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	return strings.HasPrefix(path[len(prefix):], "/")
}

// bodyTooLargeError builds the catalogued 413 problem. The limit is included in
// the message so a client can size its request without guessing; the code stays
// stable so callers can branch on it.
func bodyTooLargeError(limit int64) *ardaerrors.Error {
	return ardaerrors.New(
		ardaerrors.CodeBodyTooLarge,
		"Request body exceeds the limit of "+strconv.FormatInt(limit, 10)+" bytes",
	)
}
