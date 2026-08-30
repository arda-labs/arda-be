package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Indexer implements the docs-as-code ingestion pipeline (roadmap §4.6):
// markdown files → heading-based chunks → (changed chunks only) embedded →
// upserted into ai_knowledge_sources / ai_knowledge_chunks. Versioning and
// audit trail come free from git: version is the commit SHA, source_key the
// repository-relative path.
type Indexer struct {
	db       *sql.DB
	embedder Embedder
}

func NewIndexer(db *sql.DB, embedder Embedder) *Indexer {
	return &Indexer{db: db, embedder: embedder}
}

// maxChunkWords keeps chunks inside the 500–800 token target range.
const maxChunkWords = 600

// Chunk splits markdown into heading-scoped chunks. The first `# ` heading
// becomes the document title; `##`/`###` sections become chunks. Oversized
// sections are split on blank lines without breaking paragraphs.
func Chunk(markdown string) (title string, chunks []ChunkSection) {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	type section struct {
		heading string
		body    []string
	}
	var sections []section
	var current section
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "# ") && title == "":
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			continue
		case strings.HasPrefix(line, "## "), strings.HasPrefix(line, "### "):
			if strings.TrimSpace(current.heading) != "" || strings.TrimSpace(strings.Join(current.body, "\n")) != "" {
				sections = append(sections, current)
			}
			current = section{heading: strings.TrimSpace(strings.TrimLeft(line, "#"))}
		default:
			current.body = append(current.body, line)
		}
	}
	if strings.TrimSpace(current.heading) != "" || strings.TrimSpace(strings.Join(current.body, "\n")) != "" {
		sections = append(sections, current)
	}

	for _, sec := range sections {
		body := strings.TrimSpace(strings.Join(sec.body, "\n"))
		if body == "" {
			continue
		}
		for _, part := range splitOversized(body, maxChunkWords) {
			chunks = append(chunks, ChunkSection{Heading: sec.heading, Content: part})
		}
	}
	return title, chunks
}

// ChunkSection is one heading-scoped chunk of a document.
type ChunkSection struct {
	Heading string
	Content string
}

func splitOversized(body string, maxWords int) []string {
	if len(strings.Fields(body)) <= maxWords {
		return []string{body}
	}
	var parts []string
	var current []string
	currentWords := 0
	for _, paragraph := range strings.Split(body, "\n\n") {
		words := len(strings.Fields(paragraph))
		if currentWords > 0 && currentWords+words > maxWords {
			parts = append(parts, strings.TrimSpace(strings.Join(current, "\n\n")))
			current = nil
			currentWords = 0
		}
		current = append(current, paragraph)
		currentWords += words
	}
	if len(current) > 0 {
		parts = append(parts, strings.TrimSpace(strings.Join(current, "\n\n")))
	}
	return parts
}

func contentChecksum(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// IndexDocument upserts one markdown document: source row keyed by
// (tenantID, sourceKey, version), then per-chunk diff — only chunks whose
// content checksum changed are re-embedded. Unchanged chunks keep their
// vectors. Rows are PUBLISHED; draft/review flows live in the admin API
// (roadmap §4.6, not part of docs-as-code ingestion). Empty tenantID means a
// global/system document.
func (ix *Indexer) IndexDocument(ctx context.Context, tenantID, sourceKey, version, title, markdown, sourceType string) (embedded, skipped int, err error) {
	if ix == nil || ix.db == nil {
		return 0, 0, fmt.Errorf("knowledge indexer is not configured")
	}
	sourceKey = strings.TrimSpace(sourceKey)
	version = strings.TrimSpace(version)
	if sourceKey == "" || version == "" {
		return 0, 0, fmt.Errorf("source key and version are required")
	}
	var tenantColumn any
	scope := "global"
	if strings.TrimSpace(tenantID) != "" {
		tenantColumn = strings.TrimSpace(tenantID)
		scope = "tenant"
	}

	var sourceID string
	err = ix.db.QueryRowContext(ctx, `
		SELECT id FROM public.ai_knowledge_sources
		WHERE tenant_id IS NOT DISTINCT FROM $1 AND source_key = $2 AND version = $3
	`, tenantColumn, sourceKey, version).Scan(&sourceID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		err = ix.db.QueryRowContext(ctx, `
			INSERT INTO public.ai_knowledge_sources
				(tenant_id, source_key, title, version, status, scope, source_type, effective_from)
			VALUES ($1, $2, $3, $4, 'PUBLISHED', $5, $6, now())
			RETURNING id
		`, tenantColumn, sourceKey, title, version, scope, sourceType).Scan(&sourceID)
		if err != nil {
			return 0, 0, fmt.Errorf("insert knowledge source: %w", err)
		}
	case err != nil:
		return 0, 0, fmt.Errorf("find knowledge source: %w", err)
	default:
		if _, err := ix.db.ExecContext(ctx, `
			UPDATE public.ai_knowledge_sources
			SET title = $3, status = 'PUBLISHED', updated_at = now()
			WHERE id = $1 AND version = $2
		`, sourceID, version, title); err != nil {
			return 0, 0, fmt.Errorf("update knowledge source: %w", err)
		}
	}

	_, chunks := Chunk(markdown)
	for i := range chunks {
		chunks[i].Content = strings.TrimSpace(chunks[i].Content)
	}

	type existingChunk struct {
		index    int
		checksum string
	}
	rows, err := ix.db.QueryContext(ctx, `
		SELECT chunk_index, content_checksum FROM public.ai_knowledge_chunks
		WHERE source_id = $1
	`, sourceID)
	if err != nil {
		return 0, 0, fmt.Errorf("load existing chunks: %w", err)
	}
	existing := map[int]string{}
	for rows.Next() {
		var item existingChunk
		if err := rows.Scan(&item.index, &item.checksum); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("scan existing chunk: %w", err)
		}
		existing[item.index] = item.checksum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate existing chunks: %w", err)
	}

	// Remove chunks beyond the current chunking (document shrank or moved).
	for index := range existing {
		if index >= len(chunks) {
			if _, err := ix.db.ExecContext(ctx, `DELETE FROM public.ai_knowledge_chunks WHERE source_id = $1 AND chunk_index = $2`, sourceID, index); err != nil {
				return 0, 0, fmt.Errorf("delete stale chunk: %w", err)
			}
		}
	}

	var toEmbed []ChunkSection
	var toEmbedIndexes []int
	for index, chunk := range chunks {
		checksum := contentChecksum(chunk.Content)
		if old, ok := existing[index]; ok && old == checksum {
			skipped++
			continue
		}
		toEmbed = append(toEmbed, chunk)
		toEmbedIndexes = append(toEmbedIndexes, index)
	}

	var vectors [][]float32
	if len(toEmbed) > 0 {
		if ix.embedder == nil {
			// Store text chunks without vectors; search still finds them via
			// full-text. Embedding runs once an embedder is configured.
			vectors = make([][]float32, len(toEmbed))
		} else {
			texts := make([]string, len(toEmbed))
			for i, chunk := range toEmbed {
				texts[i] = chunk.Content
			}
			vectors, err = ix.embedder.Embed(ctx, texts)
			if err != nil {
				return 0, 0, fmt.Errorf("embed chunks: %w", err)
			}
		}
	}

	for i, chunk := range toEmbed {
		index := toEmbedIndexes[i]
		checksum := contentChecksum(chunk.Content)
		var vector any
		var embeddingModel any
		if vectors != nil && vectors[i] != nil {
			vector = vectorLiteral(vectors[i])
			embeddingModel = ix.embedder.Model()
		}
		if _, ok := existing[index]; ok {
			_, err = ix.db.ExecContext(ctx, `
				UPDATE public.ai_knowledge_chunks
				SET heading = $3, content = $4, content_checksum = $5,
				    embedding = $6, embedding_model = $7, embedding_dimensions = $8
				WHERE source_id = $1 AND chunk_index = $2
			`, sourceID, index, chunk.Heading, chunk.Content, checksum,
				vector, embeddingModel, ix.embedderDimensionsPtr())
		} else {
			_, err = ix.db.ExecContext(ctx, `
				INSERT INTO public.ai_knowledge_chunks
					(source_id, tenant_id, chunk_index, heading, content,
					 content_checksum, embedding, embedding_model, embedding_dimensions)
				VALUES ($1, $9, $2, $3, $4, $5, $6, $7, $8)
			`, sourceID, index, chunk.Heading, chunk.Content, checksum,
				vector, embeddingModel, ix.embedderDimensionsPtr(), tenantColumn)
		}
		if err != nil {
			return 0, 0, fmt.Errorf("upsert chunk %d: %w", index, err)
		}
		embedded++
	}
	return embedded, skipped, nil
}

func (ix *Indexer) embedderDimensionsPtr() any {
	if ix.embedder == nil {
		return nil
	}
	return ix.embedder.Dimensions()
}
