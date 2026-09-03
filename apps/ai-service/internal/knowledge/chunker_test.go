package knowledge

import (
	"strings"
	"testing"
)

func TestChunkMarkdownHeadingSplit(t *testing.T) {
	md := "# Chính sách nhân sự\n\n## Nghỉ phép\n\nMỗi năm 12 ngày.\n\n### Tang lễ\n\n3 ngày.\n"
	chunks, err := ChunkMarkdown(md, 512, 64, "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Heading != "Nghỉ phép" {
		t.Errorf("expected heading 'Nghỉ phép', got %q", chunks[0].Heading)
	}
	if chunks[1].Heading != "Tang lễ" {
		t.Errorf("expected heading 'Tang lễ', got %q", chunks[1].Heading)
	}
	if chunks[0].ContentHash == "" || chunks[1].ContentHash == "" {
		t.Errorf("expected non-empty content hashes")
	}
}

func TestChunkMarkdownOversizedBodySplit(t *testing.T) {
	var paras []string
	for i := 0; i < 10; i++ {
		paras = append(paras, "nội dung đoạn văn lặp đi lặp lại rất dài để kiểm tra cắt chunk.")
	}
	md := "## Mục dài\n\n" + strings.Join(paras, "\n\n")
	chunks, err := ChunkMarkdown(md, 20, 5, "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(chunks) <= 1 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		wordCount := len(strings.Fields(c.Content))
		if wordCount > 20 {
			t.Errorf("chunk %d exceeds chunkSize: %d words", i, wordCount)
		}
	}
}

func TestChunkMarkdownInvalidConfig(t *testing.T) {
	_, err := ChunkMarkdown("## A\n\nTest", 0, 0, "1")
	if err == nil {
		t.Errorf("expected error for chunkSize <= 0")
	}

	_, err = ChunkMarkdown("## A\n\nTest", 50, 50, "1")
	if err == nil {
		t.Errorf("expected error for chunkOverlap >= chunkSize")
	}

	_, err = ChunkMarkdown("## A\n\nTest", 50, -1, "1")
	if err == nil {
		t.Errorf("expected error for chunkOverlap < 0")
	}
}
