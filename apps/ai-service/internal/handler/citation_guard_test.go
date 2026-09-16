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

func TestSanitizeAnswerSourcesDropsUnknownItems(t *testing.T) {
	known := []string{"Quy trình hoàn ứng chi phí — Quy trình 5 bước (v1)"}
	reply := "Quy trình hoàn ứng gồm 5 bước theo tài liệu.\n\nNguồn tham khảo:\n- Quy trình hoàn ứng chi phí — Quy trình 5 bước (v1)\n- Quy trình đăng ký người dùng và phân quyền — Vòng đời tài khoản (v1)"

	sanitized, dropped := sanitizeAnswerSources(reply, known)
	if dropped != 1 {
		t.Fatalf("expected the unknown item to be dropped, got %d", dropped)
	}
	if strings.Contains(sanitized, "Quy trình đăng ký người dùng") {
		t.Fatalf("unknown source must be removed: %q", sanitized)
	}
	if !strings.Contains(sanitized, known[0]) {
		t.Fatalf("known source must stay: %q", sanitized)
	}
}

// The incident answer listed references while stating no content exists; such
// a section must disappear entirely.
func TestSanitizeAnswerSourcesRemovesSectionFromNoEvidenceAnswer(t *testing.T) {
	known := []string{"Quy trình hoàn ứng chi phí — Quy trình 5 bước (v1)"}
	reply := "Hiện chưa có nội dung nào trong kho tri thức về quy trình mở sổ tiết kiệm.\n\nNguồn tham khảo:\n- " + known[0]

	sanitized, dropped := sanitizeAnswerSources(reply, known)
	if dropped == 0 {
		t.Fatal("expected the whole section to be removed")
	}
	if strings.Contains(sanitized, "Nguồn tham khảo") {
		t.Fatalf("no-evidence answers must not advertise sources: %q", sanitized)
	}
	if !strings.Contains(sanitized, "chưa có nội dung") {
		t.Fatalf("body must be preserved: %q", sanitized)
	}
}

func TestSanitizeAnswerSourcesDropsWholeSectionWhenAllUnknown(t *testing.T) {
	reply := "Trả lời.\n\nSources:\n- Something unrelated\n- Another thing"

	sanitized, dropped := sanitizeAnswerSources(reply, nil)
	if dropped != 2 || strings.Contains(sanitized, "Sources") {
		t.Fatalf("expected the section to disappear, dropped=%d reply=%q", dropped, sanitized)
	}
	if sanitized != "Trả lời." {
		t.Fatalf("unexpected sanitized reply: %q", sanitized)
	}
}

func TestSanitizeAnswerSourcesLeavesPlainAnswersAlone(t *testing.T) {
	reply := "Bạn được 12 ngày phép."
	sanitized, dropped := sanitizeAnswerSources(reply, []string{"Chính sách nghỉ phép"})
	if dropped != 0 || sanitized != reply {
		t.Fatalf("plain answer must pass through, dropped=%d %q", dropped, sanitized)
	}
}

func TestStatesNoEvidence(t *testing.T) {
	yes := []string{
		"Hiện chưa có nội dung nào về quy trình này.",
		"Tôi không tìm thấy tài liệu nào phù hợp.",
		"No matching content was found in the knowledge base.",
	}
	for _, reply := range yes {
		if !statesNoEvidence(reply) {
			t.Errorf("expected no-evidence detection for %q", reply)
		}
	}
	no := []string{
		"Quy trình gồm 5 bước.",
		"Tôi không thể truy cập tài liệu do lỗi.",
	}
	for _, reply := range no {
		if statesNoEvidence(reply) {
			t.Errorf("unexpected no-evidence detection for %q", reply)
		}
	}
}
