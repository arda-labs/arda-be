package svcclient

import "time"

// Knowledge retrieval DTOs shared by the in-process RAG service
// (knowledge.InProcessRAGAdapter) and the arda.knowledge.search tool contract.
// The retired Python rag-service HTTP client that originally defined them is
// gone — see docs/ai/audit-2026-09.md item A5.

// RAGHit is one retrieved knowledge chunk. SourceID/SourceVersionID are the
// ai_knowledge_sources / ai_knowledge_source_versions row ids.
type RAGHit struct {
	SourceID        int         `json:"source_id"`
	SourceKey       string      `json:"source_key"`
	SourceVersionID int         `json:"source_version_id"`
	Version         string      `json:"version"`
	Title           string      `json:"title"`
	Heading         string      `json:"heading"`
	Content         string      `json:"content"`
	Score           float64     `json:"score"`
	Citation        string      `json:"citation"`
	CitationRef     CitationRef `json:"citation_ref"`
}

type CitationRef struct {
	SourceID        int        `json:"source_id"`
	SourceVersionID int        `json:"source_version_id"`
	Title           string     `json:"title"`
	Version         string     `json:"version"`
	Heading         string     `json:"heading,omitempty"`
	EffectiveFrom   *time.Time `json:"effective_from,omitempty"`
	EffectiveTo     *time.Time `json:"effective_to,omitempty"`
	URL             *string    `json:"url,omitempty"`
	Locator         string     `json:"locator"`
}

// RAGResponse is the full knowledge query response.
type RAGResponse struct {
	RunID          string   `json:"run_id"`
	Hits           []RAGHit `json:"hits"`
	LatencyMs      int      `json:"latency_ms"`
	Rewritten      bool     `json:"rewritten"`
	RetrievedCount int      `json:"retrieved_count"`
	RerankedCount  int      `json:"reranked_count"`
}

// FeedbackOut is a stored knowledge feedback row.
type FeedbackOut struct {
	ID        string `json:"id"`
	RunID     string `json:"run_id"`
	Helpful   bool   `json:"helpful"`
	Comment   string `json:"comment,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}
