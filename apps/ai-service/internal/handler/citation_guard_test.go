package handler

import (
	"strings"
	"testing"
)

func TestSanitizeInventedCitations(t *testing.T) {
	known := []string{"Chính sách nghỉ phép — Điều 4 (v2)"}

	reply, removed := sanitizeInventedCitations("Theo [source-3] và [source_id:1], bạn được 12 ngày.", known)
	if removed != 2 {
		t.Fatalf("expected 2 invented tokens removed, got %d (%q)", removed, reply)
	}
	if strings.Contains(reply, "[source-3]") || strings.Contains(reply, "[source_id:1]") {
		t.Fatalf("invented tokens must be stripped: %q", reply)
	}

	reply, removed = sanitizeInventedCitations("Xem [chunk 7] để biết thêm.", known)
	if removed != 1 || strings.Contains(reply, "[chunk 7]") {
		t.Fatalf("chunk token must be stripped, got removed=%d %q", removed, reply)
	}

	// A real citation label is not in [source…] shape and must stay untouched.
	real := "Nguồn: " + known[0]
	reply, removed = sanitizeInventedCitations(real, known)
	if removed != 0 || reply != real {
		t.Fatalf("real citation label must be preserved, got removed=%d %q", removed, reply)
	}

	reply, removed = sanitizeInventedCitations("Không có trích dẫn.", known)
	if removed != 0 || reply != "Không có trích dẫn." {
		t.Fatalf("plain text must pass through unchanged, got removed=%d %q", removed, reply)
	}

	// With no evidence, an invented source token is still removed.
	reply, removed = sanitizeInventedCitations("Theo [source-9].", nil)
	if removed != 1 || strings.Contains(reply, "[source-9]") {
		t.Fatalf("token must be removed even without known citations, got removed=%d %q", removed, reply)
	}
}
