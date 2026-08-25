package knowledge

import (
	"context"
	"database/sql"
	"fmt"
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
	db *sql.DB
}

func NewSQLSearcher(db *sql.DB) *SQLSearcher {
	return &SQLSearcher{db: db}
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
		  AND (
			to_tsvector('simple', coalesce(c.heading, '') || ' ' || c.content)
			@@ plainto_tsquery('simple', $2)
			OR c.content ILIKE '%' || $2 || '%'
		  )
		ORDER BY ts_rank(
			to_tsvector('simple', coalesce(c.heading, '') || ' ' || c.content),
			plainto_tsquery('simple', $2)
		) DESC, s.updated_at DESC, c.chunk_index ASC
		LIMIT $3
	`, tenantID, query, limit)
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
