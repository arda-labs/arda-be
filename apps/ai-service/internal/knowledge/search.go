package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

type Hit struct {
	SourceID   string `json:"sourceId"`
	SourceKey  string `json:"sourceKey"`
	Title      string `json:"title"`
	Version    string `json:"version"`
	Heading    string `json:"heading,omitempty"`
	Content    string `json:"content"`
	SourceType string `json:"sourceType"`
}

type Searcher interface {
	Search(ctx context.Context, tenantID, query string, limit int) ([]Hit, error)
}

type SQLSearcher struct {
	db       *sql.DB
	embedder Embedder
}

func NewSQLSearcher(db *sql.DB) *SQLSearcher {
	return &SQLSearcher{db: db}
}

// SetEmbedder enables hybrid retrieval (full-text + pgvector cosine). When
// nil, or when query embedding fails, search falls back to full-text only.
// Vectors are only compared against chunks embedded by the same model.
func (s *SQLSearcher) SetEmbedder(embedder Embedder) {
	if s == nil {
		return
	}
	s.embedder = embedder
}

func (s *SQLSearcher) Search(ctx context.Context, tenantID, query string, limit int) ([]Hit, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge store is not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	query = strings.TrimSpace(query)
	if tenantID == "" || query == "" {
		return nil, fmt.Errorf("tenant and query are required")
	}
	if len(query) > 512 {
		query = query[:512]
	}
	if limit <= 0 || limit > 10 {
		limit = 5
	}

	orderBy := `ts_rank(
			to_tsvector('simple', coalesce(c.heading, '') || ' ' || c.content),
			plainto_tsquery('simple', $2)
		) DESC, s.updated_at DESC, c.chunk_index ASC`
	// Text match (full-text or ILIKE) is the cheap pre-filter. When the
	// embedder is available it widens the gate with a semantic-similarity
	// window so natural-language questions that share no exact token with a
	// chunk still reach the ranking stage — vector order then decides what
	// the model sees. No embedder means text match only.
	matchFilter := `(
		to_tsvector('simple', coalesce(c.heading, '') || ' ' || c.content)
		@@ plainto_tsquery('simple', $2)
		OR c.content ILIKE '%' || $2 || '%'
	)`
	args := []any{tenantID, query, limit}

	if s.embedder != nil {
		queryVector, err := s.embedder.Embed(ctx, []string{query})
		if err != nil || len(queryVector) != 1 {
			// Hybrid retrieval degrades to full-text rather than failing the
			// assistant answer.
			slog.Warn("knowledge vector search unavailable, falling back to full-text", "err", err)
		} else if len(queryVector[0]) == s.embedder.Dimensions() {
			orderBy = `(ts_rank(
					to_tsvector('simple', coalesce(c.heading, '') || ' ' || c.content),
					plainto_tsquery('simple', $2)
				) * 0.4 + coalesce(
					case when c.embedding_model = $4
						then 1 - (c.embedding <=> $5::vector) end,
					0
				) * 0.6) DESC, s.updated_at DESC, c.chunk_index ASC`
			args = []any{tenantID, query, limit, s.embedder.Model(), vectorLiteral(queryVector[0])}
			matchFilter += ` OR (c.embedding_model = $4 AND c.embedding <=> $5::vector < 0.75)`
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id::text, s.source_key, s.title, s.version, c.heading,
		       left(c.content, 2400), s.source_type
		FROM public.ai_knowledge_sources s
		JOIN public.ai_knowledge_chunks c ON c.source_id = s.id
		WHERE s.status = 'PUBLISHED'
		  AND (s.effective_from IS NULL OR s.effective_from <= now())
		  AND (s.effective_to IS NULL OR s.effective_to > now())
		  AND ((s.scope IN ('global', 'system') AND s.tenant_id IS NULL)
		       OR (s.scope = 'tenant' AND s.tenant_id = $1))
		  AND (c.tenant_id IS NULL OR c.tenant_id = $1)
		  AND ` + matchFilter + `
		ORDER BY ` + orderBy + `
		LIMIT $3
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}
	defer rows.Close()

	hits := make([]Hit, 0, limit)
	for rows.Next() {
		var hit Hit
		if err := rows.Scan(&hit.SourceID, &hit.SourceKey, &hit.Title, &hit.Version, &hit.Heading, &hit.Content, &hit.SourceType); err != nil {
			return nil, fmt.Errorf("scan knowledge hit: %w", err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate knowledge hits: %w", err)
	}
	return hits, nil
}

// vectorLiteral renders a float slice as pgvector's text input format.
func vectorLiteral(vector []float32) string {
	var builder strings.Builder
	builder.WriteByte('[')
	for i, value := range vector {
		if i > 0 {
			builder.WriteByte(',')
		}
		fmt.Fprintf(&builder, "%g", value)
	}
	builder.WriteByte(']')
	return builder.String()
}
