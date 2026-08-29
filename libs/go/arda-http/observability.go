package ardahttp

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// MetricsMiddleware provides a small dependency-free Prometheus endpoint for
// every HTTP service. It deliberately exposes aggregate counters only; URL
// paths and tenant IDs are not labels, preventing cardinality and PII leaks.
// OpenTelemetry exporters can consume the same request/trace context later
// without changing the service handler contract.
//
// extraRenderers append service-specific metric lines to the same /metrics
// response (after the shared arda_http_* block); each renderer must emit
// valid Prometheus text format itself.
func MetricsMiddleware(service string, next http.Handler, extraRenderers ...func(io.Writer)) http.Handler {
	metrics := &httpMetrics{service: strings.TrimSpace(service)}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			metrics.write(w)
			for _, renderer := range extraRenderers {
				if renderer != nil {
					renderer(w)
				}
			}
			return
		}
		w.Header().Set(HeaderRequestID, RequestID(r))
		if !validTraceParent(r.Header.Get(HeaderTraceParent)) {
			if traceParent, err := newTraceParent(); err == nil {
				r.Header.Set(HeaderTraceParent, traceParent)
			}
		}
		if traceParent := r.Header.Get(HeaderTraceParent); traceParent != "" {
			w.Header().Set(HeaderTraceParent, traceParent)
		}
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		metrics.observe(wrapped.statusCode(), time.Since(start))
	})
}

type httpMetrics struct {
	service         string
	total           atomic.Uint64
	status2xx       atomic.Uint64
	status3xx       atomic.Uint64
	status4xx       atomic.Uint64
	status5xx       atomic.Uint64
	durationNanos   atomic.Uint64
	durationBuckets [10]atomic.Uint64
}

var durationBucketSeconds = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2}

func (m *httpMetrics) observe(status int, duration time.Duration) {
	m.total.Add(1)
	m.durationNanos.Add(uint64(duration.Nanoseconds()))
	seconds := duration.Seconds()
	for index, bound := range durationBucketSeconds {
		if seconds <= bound {
			m.durationBuckets[index].Add(1)
		}
	}
	m.durationBuckets[len(durationBucketSeconds)].Add(1)
	switch {
	case status >= 200 && status < 300:
		m.status2xx.Add(1)
	case status >= 300 && status < 400:
		m.status3xx.Add(1)
	case status >= 400 && status < 500:
		m.status4xx.Add(1)
	default:
		m.status5xx.Add(1)
	}
}

func (m *httpMetrics) write(w http.ResponseWriter) {
	service := prometheusLabel(m.service)
	total := m.total.Load()
	avgSeconds := float64(m.durationNanos.Load()) / float64(time.Second)
	if total > 0 {
		avgSeconds /= float64(total)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "# TYPE arda_http_requests_total counter\narda_http_requests_total{service=\"%s\"} %d\n", service, total)
	_, _ = fmt.Fprintf(w, "# TYPE arda_http_responses_total counter\narda_http_responses_total{service=\"%s\",class=\"2xx\"} %d\n", service, m.status2xx.Load())
	_, _ = fmt.Fprintf(w, "arda_http_responses_total{service=\"%s\",class=\"3xx\"} %d\n", service, m.status3xx.Load())
	_, _ = fmt.Fprintf(w, "arda_http_responses_total{service=\"%s\",class=\"4xx\"} %d\n", service, m.status4xx.Load())
	_, _ = fmt.Fprintf(w, "arda_http_responses_total{service=\"%s\",class=\"5xx\"} %d\n", service, m.status5xx.Load())
	_, _ = fmt.Fprintf(w, "# TYPE arda_http_request_duration_seconds histogram\n")
	for index, bound := range durationBucketSeconds {
		_, _ = fmt.Fprintf(w, "arda_http_request_duration_seconds_bucket{service=\"%s\",le=\"%s\"} %d\n", service, strconv.FormatFloat(bound, 'f', -1, 64), m.durationBuckets[index].Load())
	}
	_, _ = fmt.Fprintf(w, "arda_http_request_duration_seconds_bucket{service=\"%s\",le=\"+Inf\"} %d\n", service, m.durationBuckets[len(durationBucketSeconds)].Load())
	_, _ = fmt.Fprintf(w, "arda_http_request_duration_seconds_sum{service=\"%s\"} %s\n", service, strconv.FormatFloat(avgSeconds*float64(total), 'f', 9, 64))
	_, _ = fmt.Fprintf(w, "arda_http_request_duration_seconds_count{service=\"%s\"} %d\n", service, total)
}

func prometheusLabel(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(value)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("response writer does not support hijacking")
}

func (w *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(w.ResponseWriter, r)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func newTraceParent() (string, error) {
	var traceID [16]byte
	var spanID [8]byte
	if _, err := rand.Read(traceID[:]); err != nil {
		return "", err
	}
	if _, err := rand.Read(spanID[:]); err != nil {
		return "", err
	}
	return "00-" + hex.EncodeToString(traceID[:]) + "-" + hex.EncodeToString(spanID[:]) + "-01", nil
}

func validTraceParent(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 4 || len(parts[0]) != 2 || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return false
	}
	if parts[0] == "ff" || strings.Repeat("0", 32) == parts[1] || strings.Repeat("0", 16) == parts[2] {
		return false
	}
	_, err := hex.DecodeString(parts[0] + parts[1] + parts[2] + parts[3])
	return err == nil
}
