package ardahttp

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestMetricsMiddlewareRecordsRequestsAndTraceParent(t *testing.T) {
	handler := MetricsMiddleware("test-service", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusCreated)
	}
	traceParent := res.Header().Get(HeaderTraceParent)
	if !regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`).MatchString(traceParent) {
		t.Fatalf("traceparent = %q, want W3C traceparent", traceParent)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRes := httptest.NewRecorder()
	handler.ServeHTTP(metricsRes, metricsReq)
	body := metricsRes.Body.String()
	for _, expected := range []string{
		`arda_http_requests_total{service="test-service"} 1`,
		`arda_http_responses_total{service="test-service",class="2xx"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q in %s", expected, body)
		}
	}
}

func TestMetricsMiddlewarePreservesIncomingTraceParentAndFlush(t *testing.T) {
	const traceParent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	flushed := false
	handler := MetricsMiddleware("test-service", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(HeaderTraceParent); got != traceParent {
			t.Errorf("incoming traceparent = %q, want %q", got, traceParent)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: ok\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
			flushed = true
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set(HeaderTraceParent, traceParent)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Header().Get(HeaderTraceParent) != traceParent {
		t.Fatalf("response traceparent = %q, want %q", res.Header().Get(HeaderTraceParent), traceParent)
	}
	if !flushed {
		t.Fatal("wrapped response writer did not preserve http.Flusher")
	}
}
