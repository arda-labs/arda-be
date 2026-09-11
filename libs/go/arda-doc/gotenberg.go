package ardadoc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// GotenbergClient talks to a Gotenberg 8 service for HTML→PDF (Chromium) and
// Office→PDF (LibreOffice) conversion. Gotenberg is deployed as a dedicated
// in-cluster pod; Vietnamese diacritics render correctly because the
// gotenberg image bundles Noto fonts.
type GotenbergClient struct {
	baseURL    string
	httpClient *http.Client
}

// PDFOptions carries the Gotenberg Chromium conversion form fields. Zero
// values are omitted and Gotenberg defaults apply (A4 portrait, default
// margins, scale 1).
type PDFOptions struct {
	PaperWidth   string // e.g. "8.27in"
	PaperHeight  string // e.g. "11.69in"
	MarginTop    string
	MarginBottom string
	MarginLeft   string
	MarginRight  string
	Landscape    bool
	Scale        float64
	// EmulatedMediaType forces CSS media type rendering: "screen" keeps
	// screen styling, "print" applies print stylesheets.
	EmulatedMediaType string
}

// ClientOption configures a GotenbergClient or MediaUploader.
type ClientOption func(*clientConfig)

type clientConfig struct {
	httpClient *http.Client
	timeout    time.Duration
}

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *clientConfig) { c.httpClient = hc }
}

// WithTimeout sets the request timeout on the default HTTP client.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.timeout = d
		c.httpClient = &http.Client{Timeout: d}
	}
}

func newClientConfig(opts []ClientOption) *clientConfig {
	cfg := &clientConfig{httpClient: &http.Client{Timeout: 2 * time.Minute}}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// NewGotenbergClient creates a client for the Gotenberg API at baseURL
// (e.g. http://gotenberg.platform.svc.cluster.local:3000).
func NewGotenbergClient(baseURL string, opts ...ClientOption) (*GotenbergClient, error) {
	base, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid gotenberg base url: %w", err)
	}
	return &GotenbergClient{baseURL: base, httpClient: newClientConfig(opts).httpClient}, nil
}

// HTMLToPDF converts an HTML document to PDF through Gotenberg Chromium.
// html must be a complete document; assets referenced by URL are fetched by
// Chromium itself, so keep templates self-contained for in-cluster use.
func (c *GotenbergClient) HTMLToPDF(ctx context.Context, html []byte, opts *PDFOptions) ([]byte, error) {
	return c.convert(ctx, "/forms/chromium/convert/html", func(mw *multipart.Writer) error {
		if err := writeFormFile(mw, "index.html", html, "text/html"); err != nil {
			return err
		}
		return writePDFFormFields(mw, opts)
	})
}

// OfficeToPDF converts a docx/xlsx/pptx file to PDF through Gotenberg
// LibreOffice. LibreOffice serializes conversions internally (single-lock),
// so callers should keep timeouts generous.
func (c *GotenbergClient) OfficeToPDF(ctx context.Context, filename string, content []byte) ([]byte, error) {
	if strings.TrimSpace(filename) == "" {
		return nil, fmt.Errorf("filename is required")
	}
	contentType := mime.TypeByExtension(filepath.Ext(filename))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return c.convert(ctx, "/forms/libreoffice/convert", func(mw *multipart.Writer) error {
		return writeFormFile(mw, filename, content, contentType)
	})
}

// Health reports whether Gotenberg is reachable and ready.
func (c *GotenbergClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.baseURL, "/")+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gotenberg health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("gotenberg health status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *GotenbergClient) convert(ctx context.Context, path string, buildForm func(*multipart.Writer) error) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := buildForm(mw); err != nil {
		return nil, fmt.Errorf("build gotenberg form: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.baseURL, "/")+path, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gotenberg convert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("gotenberg convert status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return io.ReadAll(resp.Body)
}

func writeFormFile(mw *multipart.Writer, filename string, content []byte, contentType string) error {
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="files"; filename=%q`, filename)}
	h["Content-Type"] = []string{contentType}
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = part.Write(content)
	return err
}

func writePDFFormFields(mw *multipart.Writer, opts *PDFOptions) error {
	if opts == nil {
		return nil
	}
	fields := map[string]string{}
	if opts.PaperWidth != "" {
		fields["paperWidth"] = opts.PaperWidth
	}
	if opts.PaperHeight != "" {
		fields["paperHeight"] = opts.PaperHeight
	}
	if opts.MarginTop != "" {
		fields["marginTop"] = opts.MarginTop
	}
	if opts.MarginBottom != "" {
		fields["marginBottom"] = opts.MarginBottom
	}
	if opts.MarginLeft != "" {
		fields["marginLeft"] = opts.MarginLeft
	}
	if opts.MarginRight != "" {
		fields["marginRight"] = opts.MarginRight
	}
	if opts.Landscape {
		fields["landscape"] = "true"
	}
	if opts.Scale > 0 {
		fields["scale"] = strconv.FormatFloat(opts.Scale, 'f', -1, 64)
	}
	if opts.EmulatedMediaType != "" {
		fields["emulatedMediaType"] = opts.EmulatedMediaType
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return err
		}
	}
	return nil
}

func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "/"))
	if raw == "" {
		return "", fmt.Errorf("base url is required")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("must be an absolute http(s) url: %q", raw)
	}
	return raw, nil
}
