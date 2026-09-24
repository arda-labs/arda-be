package decision

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildStateUsesLatestUserMessageOnly(t *testing.T) {
	state, ok := BuildState([]Message{
		{Role: "system", Content: "base"},
		{Role: "assistant", Content: "ignored"},
		{Role: "user", Content: "Phân tích dư nợ"},
	})
	if !ok || state != "Latest user request: Phân tích dư nợ" {
		t.Fatalf("state=%q ok=%v", state, ok)
	}
}

func TestBuildStateAttachesPreviousUserMessageWithinBudget(t *testing.T) {
	state, ok := BuildState([]Message{
		{Role: "user", Content: "Phân tích dư nợ theo nhóm nợ"},
		{Role: "user", Content: "thế còn nhóm 3-5?"},
	})
	want := "Previous user request (context only): Phân tích dư nợ theo nhóm nợ\nLatest user request: thế còn nhóm 3-5?"
	if !ok || state != want {
		t.Fatalf("state=%q ok=%v", state, ok)
	}
}

func TestBuildStateAttachesPreviousAssistantContextInMultiTurn(t *testing.T) {
	state, ok := BuildState([]Message{
		{Role: "user", Content: "Phân tích dư nợ theo nhóm nợ"},
		{Role: "assistant", Content: "Bạn muốn xem kỳ báo cáo nào?"},
		{Role: "user", Content: "kỳ 2026-08"},
	})
	want := "Previous user request (context only): Phân tích dư nợ theo nhóm nợ\nPrevious assistant context: Bạn muốn xem kỳ báo cáo nào?\nLatest user request: kỳ 2026-08"
	if !ok || state != want {
		t.Fatalf("state=%q ok=%v want=%q", state, ok, want)
	}
}

func TestBuildStateDropsOversizedPreviousAssistant(t *testing.T) {
	state, ok := BuildState([]Message{
		{Role: "user", Content: "Phân tích dư nợ theo nhóm nợ"},
		{Role: "assistant", Content: strings.Repeat("x", 1025)},
		{Role: "user", Content: "kỳ 2026-08"},
	})
	want := "Previous user request (context only): Phân tích dư nợ theo nhóm nợ\nLatest user request: kỳ 2026-08"
	if !ok || state != want {
		t.Fatalf("oversized assistant must be dropped: %q ok=%v", state, ok)
	}
}

func TestBuildStateDropsOversizedPrevious(t *testing.T) {
	state, ok := BuildState([]Message{
		{Role: "user", Content: strings.Repeat("x", 2049)},
		{Role: "user", Content: "Phân tích dư nợ"},
	})
	if !ok || strings.Contains(state, "Previous user request") {
		t.Fatalf("oversized previous must be dropped: %q ok=%v", state, ok)
	}
}

func TestBuildStateSkipsOversizedLatest(t *testing.T) {
	if _, ok := BuildState([]Message{{Role: "user", Content: strings.Repeat("x", 4097)}}); ok {
		t.Fatal("latest over 4096 bytes must skip classification")
	}
	if _, ok := BuildState([]Message{{Role: "user", Content: strings.Repeat("x", 4096)}}); !ok {
		t.Fatal("latest at exactly 4096 bytes must still be classified")
	}
}

func TestBuildStateRequiresAUserMessage(t *testing.T) {
	if _, ok := BuildState([]Message{{Role: "system", Content: "base"}}); ok {
		t.Fatal("no user message must skip classification")
	}
	if _, ok := BuildState(nil); ok {
		t.Fatal("empty history must skip classification")
	}
}

func TestBuildStateRedactsCredentials(t *testing.T) {
	state, ok := BuildState([]Message{{Role: "user", Content: "Authorization: Bearer sk-secret hãy phân tích dư nợ"}})
	if !ok || strings.Contains(state, "sk-secret") || !strings.Contains(state, "[REDACTED]") {
		t.Fatalf("state=%q ok=%v", state, ok)
	}
}

func TestTruncateRunesKeepsValidUTF8(t *testing.T) {
	got := TruncateRunes(strings.Repeat("ế", 10), 16)
	if len(got) > 16 || !utf8.ValidString(got) {
		t.Fatalf("truncated value invalid: %q (%d bytes)", got, len(got))
	}
	if TruncateRunes("short", 16) != "short" {
		t.Fatal("short value must be unchanged")
	}
}
