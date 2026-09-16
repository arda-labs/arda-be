package catalog

import (
	"strings"
	"testing"
	"time"
)

func TestFormatSignaturesBriefIsCompact(t *testing.T) {
	entry := CatalogEntry{
		MethodName: "knowledge.search",
		SDKPath:    "arda.knowledge.search",
		Domain:     "knowledge",
		Signature:  "arda.knowledge.search(args: { query: string }): Promise<KnowledgeSearchResult[]>;",
		JSDoc: `/**
 * Search published knowledge sources with citations.
 * @param args.query Natural language query
 * @returns results
 */`,
		Timeout: time.Second,
	}

	brief := FormatSignaturesBrief([]CatalogEntry{entry})
	if !strings.Contains(brief, "arda.knowledge.search(args:") {
		t.Fatalf("brief output must carry the signature: %q", brief)
	}
	if !strings.Contains(brief, "// Search published knowledge sources with citations.") {
		t.Fatalf("brief output must carry the summary line: %q", brief)
	}
	if strings.Contains(brief, "@param") || strings.Contains(brief, "/**") {
		t.Fatalf("brief output must not carry the full JSDoc: %q", brief)
	}

	full := FormatSignatures([]CatalogEntry{entry})
	if len(full) <= len(brief) {
		t.Fatalf("full output must be larger than brief: %d vs %d", len(full), len(brief))
	}
}

func TestFormatSignaturesBriefEmptyCatalog(t *testing.T) {
	if got := FormatSignaturesBrief(nil); !strings.Contains(got, "No matching") {
		t.Fatalf("empty catalog must explain itself, got %q", got)
	}
}

func TestJsDocSummarySkipsTagsAndMarkers(t *testing.T) {
	summary := jsDocSummary("/**\n * \n * @domain knowledge\n * Fallback description\n */")
	if summary != "Fallback description" {
		t.Fatalf("expected the first description line, got %q", summary)
	}
	if got := jsDocSummary(""); got != "" {
		t.Fatalf("empty jsdoc must yield empty summary, got %q", got)
	}
}
