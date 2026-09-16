package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
)

// sniffLen is the byte window http.DetectContentType inspects. Upload paths
// read this prefix before the object is persisted so the stored content_type
// reflects real bytes, not the client-declared header.
const sniffLen = 512

// dangerousContentTypes render as active content (script, external entity
// expansion) when a browser is allowed to sniff or render them inline.
var dangerousContentTypes = map[string]struct{}{
	"text/html":                {},
	"application/xhtml+xml":    {},
	"image/svg+xml":            {},
	"application/javascript":   {},
	"text/javascript":          {},
	"application/x-javascript": {},
	"text/ecmascript":          {},
	"application/ecmascript":   {},
	"text/xml":                 {},
	"application/xml":          {},
	"text/xsl":                 {},
}

// dangerousExtensions are blocked even when the declared MIME type looks safe.
var dangerousExtensions = map[string]struct{}{
	"html": {}, "htm": {}, "xhtml": {}, "shtml": {}, "svg": {}, "svgz": {},
	"js": {}, "mjs": {}, "cjs": {}, "xml": {}, "xsl": {}, "xslt": {}, "xht": {},
}

// zipContainerContentTypes sniff as application/zip but carry a richer,
// non-executable document type. They may keep the declared type.
var zipContainerContentTypes = map[string]struct{}{
	"application/zip":              {},
	"application/x-zip-compressed": {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {},
	"application/vnd.oasis.opendocument.text":                                   {},
	"application/vnd.oasis.opendocument.spreadsheet":                            {},
	"application/vnd.oasis.opendocument.presentation":                           {},
	"application/epub+zip": {},
}

// defaultAllowedUploadMIME is the built-in safe upload allowlist, overridable
// with ALLOWED_UPLOAD_MIME. Everything here is non-executable: active content
// (HTML, SVG, JS, XML) is never allowed, and anything outside the inline-safe
// set is served as an attachment.
var defaultAllowedUploadMIME = []string{
	"application/json",
	"application/pdf",
	"application/octet-stream",
	"application/zip",
	"application/msword",
	"application/vnd.ms-excel",
	"application/vnd.ms-powerpoint",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"application/vnd.oasis.opendocument.text",
	"application/vnd.oasis.opendocument.spreadsheet",
	"application/vnd.oasis.opendocument.presentation",
	"audio/mp4",
	"audio/mpeg",
	"audio/ogg",
	"audio/wav",
	"audio/webm",
	"image/avif",
	"image/bmp",
	"image/gif",
	"image/heic",
	"image/heif",
	"image/jpeg",
	"image/png",
	"image/tiff",
	"image/webp",
	"text/csv",
	"text/markdown",
	"text/plain",
	"video/mp4",
	"video/webm",
}

// inlineSafeContentTypes may be rendered by the browser. Types outside this
// set are always forced to Content-Disposition: attachment.
var inlineSafeContentTypes = map[string]struct{}{
	"application/pdf": {},
	"image/bmp":       {},
	"image/gif":       {},
	"image/jpeg":      {},
	"image/png":       {},
	"image/tiff":      {},
	"image/webp":      {},
	"text/plain":      {},
}

// NormalizeContentType lowercases a content type and drops parameters
// (`text/plain; charset=utf-8` -> `text/plain`).
func NormalizeContentType(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	if mediaType, _, err := mime.ParseMediaType(raw); err == nil {
		return mediaType
	}
	if idx := strings.IndexByte(raw, ';'); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	return raw
}

// IsDangerousContentType reports whether the type can execute script or load
// external entities when rendered by a browser.
func IsDangerousContentType(raw string) bool {
	_, dangerous := dangerousContentTypes[NormalizeContentType(raw)]
	return dangerous
}

// IsDangerousExtension blocks script/XML family file extensions regardless of
// the declared MIME type.
func IsDangerousExtension(filename string) bool {
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(filename)), ".")
	_, dangerous := dangerousExtensions[extension]
	return dangerous
}

// CanServeInline reports whether a stored content type may be served without
// forcing a download. This set is deliberately independent from the upload
// allowlist: an operator may widen uploads without widening inline rendering.
func CanServeInline(raw string) bool {
	_, ok := inlineSafeContentTypes[NormalizeContentType(raw)]
	return ok
}

// ReadSniffHead reads up to sniffLen bytes from r and returns them together
// with a reader that replays the consumed prefix. Callers never lose data.
func ReadSniffHead(r io.Reader) ([]byte, io.Reader, error) {
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, nil, err
	}
	head = head[:n]
	return head, io.MultiReader(bytes.NewReader(head), r), nil
}

// UploadPolicy enforces the upload allowlist and rejects active content.
type UploadPolicy struct {
	allowed map[string]struct{}
}

// NewUploadPolicy builds a policy from the configured allowlist. An empty
// list falls back to defaultAllowedUploadMIME.
func NewUploadPolicy(allowed []string) UploadPolicy {
	effective := allowed
	if len(effective) == 0 {
		effective = defaultAllowedUploadMIME
	}
	set := make(map[string]struct{}, len(effective))
	for _, item := range effective {
		if normalized := NormalizeContentType(item); normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return UploadPolicy{allowed: set}
}

// Allows reports whether the content type is inside the effective allowlist.
func (p UploadPolicy) Allows(raw string) bool {
	_, ok := p.allowed[NormalizeContentType(raw)]
	return ok
}

// ValidateDeclared checks a client-declared type before a presigned upload URL
// is issued, when the body is not available yet.
func (p UploadPolicy) ValidateDeclared(declared, filename string) (string, error) {
	normalized := NormalizeContentType(declared)
	if normalized == "" {
		return "", fmt.Errorf("%w: content_type is required", ErrInvalidInput)
	}
	if IsDangerousContentType(normalized) {
		return "", fmt.Errorf("%w: content_type %s is not allowed", ErrInvalidInput, normalized)
	}
	if IsDangerousExtension(filename) {
		return "", fmt.Errorf("%w: file extension is not allowed for %s", ErrInvalidInput, filename)
	}
	if !p.Allows(normalized) {
		return "", fmt.Errorf("%w: content_type %s is not allowed", ErrInvalidInput, normalized)
	}
	return normalized, nil
}

// Resolve validates the real content prefix and returns the content type to
// store. When the sniffed type is specific and disagrees with the declared
// type, the sniffed type wins; dangerous sniffed types are rejected outright.
func (p UploadPolicy) Resolve(declared, filename string, head []byte) (string, error) {
	normalized, err := p.ValidateDeclared(declared, filename)
	if err != nil {
		return "", err
	}
	if len(head) == 0 {
		// http.DetectContentType reports text/plain for an empty body; an empty
		// file must not silently downgrade the declared type.
		return normalized, nil
	}
	sniffed := NormalizeContentType(http.DetectContentType(head))
	if sniffed == "" {
		return normalized, nil
	}
	if IsDangerousContentType(sniffed) {
		return "", fmt.Errorf("%w: file content does not match declared type %s", ErrInvalidInput, normalized)
	}
	resolved := normalized
	switch {
	case sniffed == normalized:
	case sniffed == "application/octet-stream":
		// Binary containers have no magic signature; keep the declared type.
	case sniffed == "application/zip" && isZipContainerContentType(normalized):
		// Office/ODF documents sniff as a zip container; keep the richer type.
	case sniffed == "text/plain" && (strings.HasPrefix(normalized, "text/") || normalized == "application/json"):
		// text/csv, markdown and JSON sniff as generic text.
	default:
		resolved = sniffed
	}
	if !p.Allows(resolved) {
		return "", fmt.Errorf("%w: detected content_type %s is not allowed", ErrInvalidInput, resolved)
	}
	return resolved, nil
}

func isZipContainerContentType(raw string) bool {
	_, ok := zipContainerContentTypes[NormalizeContentType(raw)]
	return ok
}
