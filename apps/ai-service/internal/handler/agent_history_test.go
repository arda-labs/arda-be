package handler

import (
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

func TestBoundedHistoryKeepsNewestAndReportsDropped(t *testing.T) {
	items := []repository.HistoryMessage{
		{Role: "user", Content: strings.Repeat("a", 100)},
		{Role: "assistant", Content: strings.Repeat("b", 100)},
		{Role: "user", Content: strings.Repeat("c", 100)},
		{Role: "assistant", Content: strings.Repeat("d", 100)},
	}

	replayed, dropped, truncated := boundedHistory(items, 250, 1000)
	if dropped != 2 {
		t.Fatalf("expected the two oldest messages to be dropped, got %d", dropped)
	}
	if truncated != 0 {
		t.Fatalf("nothing should be truncated, got %d", truncated)
	}
	if len(replayed) != 2 || replayed[0].Content != items[2].Content || replayed[1].Content != items[3].Content {
		t.Fatalf("expected the newest two messages in chronological order, got %+v", replayed)
	}
}

func TestBoundedHistoryTruncatesOversizedMessage(t *testing.T) {
	items := []repository.HistoryMessage{
		{Role: "assistant", Content: strings.Repeat("x", 500)},
		{Role: "user", Content: "câu hỏi mới"},
	}

	replayed, dropped, truncated := boundedHistory(items, 10_000, 50)
	if dropped != 0 || truncated != 1 {
		t.Fatalf("expected one truncation and no drops, got dropped=%d truncated=%d", dropped, truncated)
	}
	if len(replayed) != 2 {
		t.Fatalf("both messages must replay, got %d", len(replayed))
	}
	if len(replayed[0].Content) > 100 || !strings.Contains(replayed[0].Content, "truncated") {
		t.Fatalf("oversized message must be truncated with a marker: %q", replayed[0].Content)
	}
}

func TestBoundedHistoryIgnoresToolRoles(t *testing.T) {
	items := []repository.HistoryMessage{
		{Role: "tool", Content: "tool output"},
		{Role: "user", Content: "hello"},
		{Role: "system", Content: "system note"},
	}

	replayed, dropped, _ := boundedHistory(items, 10_000, 10_000)
	if dropped != 0 {
		t.Fatalf("unpaired roles are skipped, not dropped: %d", dropped)
	}
	if len(replayed) != 1 || replayed[0].Role != "user" {
		t.Fatalf("only user/assistant replay, got %+v", replayed)
	}
}
