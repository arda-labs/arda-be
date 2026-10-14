package metrics

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRender(t *testing.T) {
	m := New()
	m.ObserveRequest("cccd", "ok", "cloud", 1500*time.Millisecond)
	m.ObserveRequest("cccd", "ok", "cloud", 12*time.Second)
	m.ObserveRequest("cccd", "timeout", "cloud", 25*time.Second)
	m.ObserveFlag("id_number", "invalid_format")
	var b bytes.Buffer
	m.Render(&b)
	out := b.String()
	for _, want := range []string{
		`document_extract_requests_total{doc_type="cccd",outcome="ok",model_location="cloud"} 2`,
		`document_extract_requests_total{doc_type="cccd",outcome="timeout",model_location="cloud"} 1`,
		`document_extract_duration_seconds_bucket{doc_type="cccd",le="2"} 1`,
		`document_extract_duration_seconds_bucket{doc_type="cccd",le="15"} 2`,
		`document_extract_duration_seconds_bucket{doc_type="cccd",le="+Inf"} 3`,
		`document_extract_duration_seconds_count{doc_type="cccd"} 3`,
		`document_field_flags_total{field="id_number",flag="invalid_format"} 1`,
		"# TYPE document_extract_duration_seconds histogram",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}
