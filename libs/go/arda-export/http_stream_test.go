package ardaexport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// failingResponseWriter simulates a client that has gone away: every Write
// fails like a broken TCP connection, and neither ReadFrom nor Flush are
// available (closer to a real net/http response writer than a recorder).
type failingResponseWriter struct {
	header http.Header
	err    error
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *failingResponseWriter) WriteHeader(int) {}

func (w *failingResponseWriter) Write([]byte) (int, error) {
	if w.err == nil {
		w.err = errors.New("client disconnected")
	}
	return 0, w.err
}

func newExportRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, "/api/export?format=csv", nil)
}

// TestServeStreamHTTP_ClientDisconnectDoesNotHang reproduces the deadlock:
// io.Copy fails on the first write to a disconnected client while the producer
// is still blocked in pw.Write. The handler must return and the producer must
// be unblocked shortly after.
func TestServeStreamHTTP_ClientDisconnectDoesNotHang(t *testing.T) {
	t.Parallel()

	w := &failingResponseWriter{}
	producerExited := make(chan struct{})

	// 36 KiB per write > io.Copy's 32 KiB buffer, so the producer is
	// guaranteed to be mid-Write when the response write fails.
	payload := bytes.Repeat([]byte("aaaa,bbbb\n"), 4096)

	handlerDone := make(chan error, 1)
	go func() {
		handlerDone <- ServeStreamHTTP(w, newExportRequest(t), FormatCSV, "report", func(_ context.Context, out io.Writer) error {
			defer close(producerExited)
			for range 1000 {
				if _, err := out.Write(payload); err != nil {
					return err
				}
			}
			return nil
		})
	}()

	select {
	case err := <-handlerDone:
		if err == nil {
			t.Fatal("ServeStreamHTTP returned nil, want a client disconnect error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeStreamHTTP deadlocked after the client disconnected")
	}

	select {
	case <-producerExited:
	case <-time.After(5 * time.Second):
		t.Fatal("producer goroutine was not released after the client disconnected")
	}
}

// TestServeStreamHTTP_ReturnsCopyError ensures a write failure is surfaced even
// when the producer finishes on its own without noticing the broken pipe.
func TestServeStreamHTTP_ReturnsCopyError(t *testing.T) {
	t.Parallel()

	w := &failingResponseWriter{}
	err := ServeStreamHTTP(w, newExportRequest(t), FormatCSV, "report", func(_ context.Context, out io.Writer) error {
		_, _ = out.Write([]byte("some,data\n"))
		return nil
	})
	if err == nil {
		t.Fatal("ServeStreamHTTP returned nil, want the response write error")
	}
	if !strings.Contains(err.Error(), "client disconnected") {
		t.Fatalf("ServeStreamHTTP returned %v, want the copy error", err)
	}
}

// TestServeStreamHTTP_Success verifies the happy path still flushes the full
// payload and sets the download headers.
func TestServeStreamHTTP_Success(t *testing.T) {
	t.Parallel()

	const body = "col_a,col_b\n1,2\n"
	rec := httptest.NewRecorder()

	err := ServeStreamHTTP(rec, newExportRequest(t), FormatCSV, "BaoCao", func(_ context.Context, out io.Writer) error {
		_, err := io.WriteString(out, body)
		return err
	})
	if err != nil {
		t.Fatalf("ServeStreamHTTP returned error: %v", err)
	}
	if got := rec.Body.String(); got != body {
		t.Errorf("body = %q, want %q", got, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="BaoCao.csv"`) {
		t.Errorf("Content-Disposition = %q, want filename BaoCao.csv", cd)
	}
	if xcto := rec.Header().Get("X-Content-Type-Options"); xcto != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", xcto)
	}
}

// TestServeStreamHTTP_ProducerError verifies producer errors are propagated.
func TestServeStreamHTTP_ProducerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("supplier failed")
	rec := httptest.NewRecorder()

	err := ServeStreamHTTP(rec, newExportRequest(t), FormatXLSX, "report", func(_ context.Context, out io.Writer) error {
		_, _ = io.WriteString(out, "partial")
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ServeStreamHTTP returned %v, want %v", err, sentinel)
	}
}
