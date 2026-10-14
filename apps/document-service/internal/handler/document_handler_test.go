package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/document-service/internal/cccd"
	"github.com/arda-labs/arda/apps/document-service/internal/extractor"
	"github.com/arda-labs/arda/apps/document-service/internal/metrics"
)

type fakeExtractor struct {
	res         cccd.Result
	err         error
	gotBack     bool
	hadDeadline bool
}

func (f *fakeExtractor) Extract(ctx context.Context, front, back []byte) (cccd.Result, error) {
	_, f.hadDeadline = ctx.Deadline()
	f.gotBack = back != nil
	return f.res, f.err
}

func jpegBytes(t *testing.T) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type part struct {
	name, filename string
	data           []byte
}

func multipartReq(t *testing.T, parts ...part) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, p := range parts {
		if p.filename == "" {
			_ = w.WriteField(p.name, string(p.data))
			continue
		}
		fw, _ := w.CreateFormFile(p.name, p.filename)
		_, _ = fw.Write(p.data)
	}
	_ = w.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/documents/extract", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func newHandler(ex Extractor, m *metrics.Metrics) *DocumentHandler {
	return NewDocumentHandler(Options{Extractor: ex, ModelLocation: "cloud", MaxImageBytes: 64 << 10, Timeout: time.Second, Metrics: m})
}

func problemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p.Code
}

func TestCapabilities(t *testing.T) {
	for _, c := range []struct {
		ex   Extractor
		want string
	}{{&fakeExtractor{}, `"doc_types":["cccd"]`}, {nil, `"doc_types":[]`}} {
		rec := httptest.NewRecorder()
		newHandler(c.ex, metrics.New()).Capabilities(rec, httptest.NewRequest(http.MethodGet, "/api/documents/capabilities", nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), c.want) || !strings.Contains(rec.Body.String(), `"model_location":"cloud"`) {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
}

func TestExtractHappyPath(t *testing.T) {
	v := "082088123456"
	ex := &fakeExtractor{res: cccd.Result{DocType: "cccd", CardVersion: cccd.CardChip2021, Warnings: []string{},
		Fields: cccd.Fields{IDNumber: cccd.Field{Value: &v, Raw: &v, Flags: []string{"expired"}}}}}
	m := metrics.New()
	rec := httptest.NewRecorder()
	newHandler(ex, m).Extract(rec, multipartReq(t, part{"doc_type", "", []byte("cccd")},
		part{"front", "f.jpg", jpegBytes(t)}, part{"back", "b.jpg", jpegBytes(t)}))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id_number":{"value":"082088123456"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !ex.gotBack || !ex.hadDeadline {
		t.Fatalf("back=%v deadline=%v", ex.gotBack, ex.hadDeadline)
	}
	var b bytes.Buffer
	m.Render(&b)
	if !strings.Contains(b.String(), `outcome="ok"`) || !strings.Contains(b.String(), `field="id_number",flag="expired"} 1`) {
		t.Fatalf("metrics:\n%s", b.String())
	}
}

func TestExtractRejections(t *testing.T) {
	big := append(jpegBytes(t), bytes.Repeat([]byte{0}, 70<<10)...)
	cases := []struct {
		name   string
		ex     Extractor
		parts  []part
		status int
		code   string
	}{
		{"disabled", nil, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.jpg", jpegBytes(t)}}, 503, CodeModelUnavailable},
		{"wrong doc type", &fakeExtractor{}, []part{{"doc_type", "", []byte("passport")}, {"front", "f.jpg", jpegBytes(t)}}, 400, CodeUnsupportedDocType},
		{"missing front", &fakeExtractor{}, []part{{"doc_type", "", []byte("cccd")}}, 400, CodeMissingFront},
		{"front as text field", &fakeExtractor{}, []part{{"doc_type", "", []byte("cccd")}, {"front", "", []byte("abc")}}, 400, CodeMissingFront},
		{"text file", &fakeExtractor{}, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.jpg", []byte("hello world")}}, 415, CodeUnsupportedMediaType},
		{"heic", &fakeExtractor{}, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.heic", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00")}}, 415, CodeUnsupportedMediaType},
		{"too large", &fakeExtractor{}, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.jpg", big}}, 413, CodeImageTooLarge},
		{"not cccd", &fakeExtractor{err: cccd.ErrNotCCCD}, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.jpg", jpegBytes(t)}}, 422, CodeNotADocument},
		{"bad output", &fakeExtractor{err: cccd.ErrBadModelOutput}, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.jpg", jpegBytes(t)}}, 502, CodeModelBadOutput},
		{"timeout", &fakeExtractor{err: extractor.ErrModelTimeout}, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.jpg", jpegBytes(t)}}, 504, CodeModelTimeout},
		{"ctx deadline", &fakeExtractor{err: context.DeadlineExceeded}, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.jpg", jpegBytes(t)}}, 504, CodeModelTimeout},
		{"unavailable", &fakeExtractor{err: errors.Join(extractor.ErrModelUnavailable, io.EOF)}, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.jpg", jpegBytes(t)}}, 503, CodeModelUnavailable},
		{"decode bomb", &fakeExtractor{err: extractor.ErrImageTooLarge}, []part{{"doc_type", "", []byte("cccd")}, {"front", "f.jpg", jpegBytes(t)}}, 413, CodeImageTooLarge},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		newHandler(c.ex, metrics.New()).Extract(rec, multipartReq(t, c.parts...))
		if rec.Code != c.status || problemCode(t, rec) != c.code {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestExtractNotMultipart(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/documents/extract", strings.NewReader(`{"doc_type":"cccd"}`))
	r.Header.Set("Content-Type", "application/json")
	newHandler(&fakeExtractor{}, metrics.New()).Extract(rec, r)
	if rec.Code != 400 || problemCode(t, rec) != CodeInvalidForm {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestExtractClientCanceled(t *testing.T) {
	ex := &fakeExtractor{err: context.Canceled}
	m := metrics.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := multipartReq(t, part{"doc_type", "", []byte("cccd")}, part{"front", "f.jpg", jpegBytes(t)}).WithContext(ctx)
	rec := httptest.NewRecorder()
	newHandler(ex, m).Extract(rec, req)
	if rec.Code != http.StatusRequestTimeout || problemCode(t, rec) != "document.extract.canceled" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var b bytes.Buffer
	m.Render(&b)
	if !strings.Contains(b.String(), `outcome="canceled"`) {
		t.Fatalf("metrics: %s", b.String())
	}
}

func TestExtractCanceledLogsAtInfo(t *testing.T) {
	var logs bytes.Buffer
	h := NewDocumentHandler(Options{Extractor: &fakeExtractor{err: context.Canceled}, ModelLocation: "cloud", MaxImageBytes: 64 << 10,
		Timeout: time.Second, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.Extract(httptest.NewRecorder(), multipartReq(t, part{"doc_type", "", []byte("cccd")}, part{"front", "f.jpg", jpegBytes(t)}).WithContext(ctx))
	if !strings.Contains(logs.String(), `"level":"INFO"`) || strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("logs: %s", logs.String())
	}
}

func TestExtractSuccessLogsAttempts(t *testing.T) {
	var logs bytes.Buffer
	ex := &fakeExtractor{res: cccd.Result{DocType: "cccd", Warnings: []string{}, Attempts: 2}}
	h := NewDocumentHandler(Options{Extractor: ex, ModelLocation: "cloud", MaxImageBytes: 64 << 10,
		Timeout: time.Second, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	h.Extract(httptest.NewRecorder(), multipartReq(t, part{"doc_type", "", []byte("cccd")}, part{"front", "f.jpg", jpegBytes(t)}))
	if !strings.Contains(logs.String(), `"attempts":2`) {
		t.Fatalf("logs: %s", logs.String())
	}
}
