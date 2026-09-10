package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"uuid"
)

type Service struct {
	repo     *Repository
	embedder Embedder
	reranker Reranker
	rewriter QueryRewriter
	logger   *slog.Logger
	workerID string
	// requireEmbedding prevents a published version from being reported as
	// indexed when its embedding provider is unavailable. Development may keep
	// FTS-only querying, but production ingestion must be explicit about this
	// failure.
	requireEmbedding bool
	// minSimilarity is the cosine evidence floor for hybrid retrieval;
	// 0 disables the gate.
	minSimilarity  float64
	eventPublisher EventPublisher
}

// QueryRewriter produces alternative retrieval queries for a user question.
// The original query is always searched too, so a failing or slow rewrite only
// costs latency, never correctness.
type QueryRewriter interface {
	Rewrite(ctx context.Context, tenantID, query string) ([]string, error)
}

type EventPublisher interface {
	Publish(ctx context.Context, subject string, envelope any) error
}

func NewService(repo *Repository, embedder Embedder, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	workerID := strings.TrimSpace(os.Getenv("HOSTNAME"))
	if workerID == "" {
		workerID = "ai-service"
	}
	workerID += "-" + uuid.New().String()
	return &Service{
		repo:     repo,
		embedder: embedder,
		logger:   logger,
		workerID: workerID,
	}
}

func (s *Service) SetRequireEmbedding(required bool) {
	s.requireEmbedding = required
}

// SetMinSimilarity sets the cosine-similarity evidence floor applied to both
// hybrid-search legs. 0 disables the gate (tests, keyword-only fallback).
func (s *Service) SetMinSimilarity(floor float64) {
	if s != nil {
		s.minSimilarity = floor
	}
}

func (s *Service) SetReranker(reranker Reranker) {
	if s != nil {
		s.reranker = reranker
	}
}

// SetQueryRewriter enables multi-query retrieval. Nil disables rewriting.
func (s *Service) SetQueryRewriter(rewriter QueryRewriter) {
	if s != nil {
		s.rewriter = rewriter
	}
}

func (s *Service) SetEventPublisher(pub EventPublisher) {
	if s != nil {
		s.eventPublisher = pub
	}
}

func (s *Service) Repo() *Repository {
	return s.repo
}

func (s *Service) Query(ctx context.Context, req QueryRequest, tenantID string) (*QueryResponse, error) {
	t0 := time.Now()
	queryText := strings.TrimSpace(req.Query)
	if queryText == "" {
		return nil, fmt.Errorf("query is required")
	}
	if len(queryText) > 2000 {
		return nil, fmt.Errorf("query exceeds 2000 characters")
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 8
	}
	if topK > 20 {
		topK = 20
	}

	queries := s.expandQueries(ctx, tenantID, queryText)
	rewritten := len(queries) > 1

	retrievalK := topK
	if s.reranker != nil {
		retrievalK = minInt(topK*3, 40)
	}
	ranked, embedded, err := s.multiQuerySearch(ctx, queries, tenantID, retrievalK)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
	}
	hits := make([]QueryHit, 0, len(ranked))
	for _, entry := range ranked {
		hits = append(hits, entry.hit)
		if len(hits) >= retrievalK {
			break
		}
	}

	retrievedCount := len(hits)
	rerankedCount := 0
	if s.reranker != nil && len(hits) > 0 {
		reranked, rerankErr := s.reranker.Rerank(ctx, queryText, hits, topK)
		if rerankErr != nil {
			s.logger.Warn("reranking failed; retaining hybrid order", "err", rerankErr)
		} else if len(reranked) > 0 {
			hits = reranked
			rerankedCount = len(reranked)
		}
	}
	latencyMs := int(time.Since(t0).Milliseconds())
	var hitIDs []int64
	for _, h := range hits {
		hitIDs = append(hitIDs, h.SourceID)
	}

	modelUsed := "fts-only"
	if embedded && s.embedder != nil {
		modelUsed = s.embedder.Model()
	}
	if rewritten {
		modelUsed += "+rewrite"
	}
	if s.reranker != nil && rerankedCount > 0 {
		modelUsed += "+rerank:" + s.reranker.Model()
	}

	runID, err := s.repo.SaveRun(ctx, tenantID, queryText, retrievedCount, rerankedCount, latencyMs, hitIDs, modelUsed)
	if err != nil {
		s.logger.Warn("failed to save RAG run", "err", err)
	}

	return &QueryResponse{
		RunID:          runID,
		Hits:           hits,
		LatencyMs:      latencyMs,
		Rewritten:      rewritten,
		RetrievedCount: retrievedCount,
		RerankedCount:  rerankedCount,
	}, nil
}

// expandQueries returns the original query plus up to two rewrite variants.
// Rewrite failures and empty/duplicate variants are ignored.
func (s *Service) expandQueries(ctx context.Context, tenantID, queryText string) []string {
	queries := []string{queryText}
	if s.rewriter == nil {
		return queries
	}
	rewriteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	variants, err := s.rewriter.Rewrite(rewriteCtx, tenantID, queryText)
	if err != nil {
		s.logger.Warn("query rewrite failed; using the original query", "err", err)
		return queries
	}
	for _, variant := range variants {
		variant = strings.TrimSpace(variant)
		if variant == "" || variant == queryText || len(variant) > 2000 {
			continue
		}
		duplicate := false
		for _, existing := range queries {
			if existing == variant {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		queries = append(queries, variant)
		if len(queries) >= 3 {
			break
		}
	}
	return queries
}

type rankedHit struct {
	hit QueryHit
	rrf float64
}

// multiQuerySearch runs hybrid search for every query and fuses the results
// with Reciprocal Rank Fusion (1/(60+rank)). The reported Score stays the best
// cosine similarity seen for the chunk, so the evidence floor keeps meaning.
func (s *Service) multiQuerySearch(ctx context.Context, queries []string, tenantID string, limit int) ([]rankedHit, bool, error) {
	merged := make(map[string]*rankedHit)
	order := make([]string, 0)
	embedded := false
	for queryIndex, query := range queries {
		vector, err := s.embedQuery(ctx, query, queryIndex == 0)
		if err != nil {
			return nil, false, err
		}
		if len(vector) > 0 {
			embedded = true
		}
		hits, err := s.repo.HybridSearch(ctx, query, vector, tenantID, limit, s.minSimilarity)
		if err != nil {
			return nil, false, err
		}
		for rank, hit := range hits {
			key := fmt.Sprintf("%d|%s|%s", hit.SourceVersionID, hit.Heading, hit.Content)
			entry, ok := merged[key]
			if !ok {
				copied := hit
				entry = &rankedHit{hit: copied}
				merged[key] = entry
				order = append(order, key)
			}
			entry.rrf += 1.0 / (60.0 + float64(rank+1))
			if hit.Score > entry.hit.Score {
				entry.hit.Score = hit.Score
			}
		}
	}
	ranked := make([]rankedHit, 0, len(order))
	for _, key := range order {
		ranked = append(ranked, *merged[key])
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].rrf > ranked[j].rrf })
	return ranked, embedded, nil
}

// embedQuery embeds one query. required controls fail-closed behavior for the
// original query; variant queries degrade to FTS instead of failing the run.
func (s *Service) embedQuery(ctx context.Context, query string, required bool) ([]float32, error) {
	if s.embedder == nil {
		if required && s.requireEmbedding {
			return nil, fmt.Errorf("embedding provider is required but not configured")
		}
		return nil, nil
	}
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		if required && s.requireEmbedding {
			return nil, fmt.Errorf("embedding query: %w", err)
		}
		s.logger.Warn("embedding failed, falling back to FTS-only", "err", err)
		return nil, nil
	}
	if len(vectors) > 0 {
		return vectors[0], nil
	}
	return nil, nil
}

func (s *Service) PreviewChunks(req ChunkPreviewRequest) (*ChunkPreviewResponse, error) {
	chunkSize := 512
	chunkOverlap := 64
	chunkerVersion := "1"

	if req.ChunkerConfig != nil {
		if req.ChunkerConfig.ChunkSize > 0 {
			chunkSize = req.ChunkerConfig.ChunkSize
		}
		if req.ChunkerConfig.ChunkOverlap >= 0 {
			chunkOverlap = req.ChunkerConfig.ChunkOverlap
		}
		if req.ChunkerConfig.ChunkerVersion != "" {
			chunkerVersion = req.ChunkerConfig.ChunkerVersion
		}
	}

	items, err := ChunkMarkdown(req.Content, chunkSize, chunkOverlap, chunkerVersion)
	if err != nil {
		return nil, err
	}

	var previews []ChunkPreview
	for i, it := range items {
		previews = append(previews, ChunkPreview{
			Index:       i,
			Heading:     it.Heading,
			Content:     it.Content,
			ContentHash: it.ContentHash,
			WordCount:   len(strings.Fields(it.Content)),
			CharCount:   len(it.Content),
		})
	}

	return &ChunkPreviewResponse{
		TotalChunks:   len(previews),
		ExtractedText: &req.Content,
		Chunks:        previews,
	}, nil
}

func (s *Service) ParseAndPreviewFile(fileBytes []byte, filename string, chunkSize, chunkOverlap int) (*ChunkPreviewResponse, error) {
	if chunkSize <= 0 {
		chunkSize = 512
	}
	if chunkOverlap < 0 {
		chunkOverlap = 64
	}

	extractedText := ParseDocument(fileBytes, filename)
	items, err := ChunkMarkdown(extractedText, chunkSize, chunkOverlap, "1")
	if err != nil {
		return nil, err
	}

	var previews []ChunkPreview
	for i, it := range items {
		previews = append(previews, ChunkPreview{
			Index:       i,
			Heading:     it.Heading,
			Content:     it.Content,
			ContentHash: it.ContentHash,
			WordCount:   len(strings.Fields(it.Content)),
			CharCount:   len(it.Content),
		})
	}

	return &ChunkPreviewResponse{
		TotalChunks:   len(previews),
		ExtractedText: &extractedText,
		Chunks:        previews,
	}, nil
}

// StartWorker runs the background ingestion queue processor.
func (s *Service) StartWorker(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processNextJob(ctx)
		}
	}
}

func (s *Service) processNextJob(ctx context.Context) {
	s.recoverStaleJobs(ctx)
	// Claim one pending job
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()

	query := `
		SELECT j.id::text, j.source_version_id, v.content, COALESCE(v.chunk_size, 512),
		       COALESCE(v.chunk_overlap, 64), COALESCE(v.chunker_version, '1'),
		       j.attempts, j.max_attempts
		  FROM public.ai_ingestion_jobs j
		  JOIN public.ai_knowledge_source_versions v ON v.id = j.source_version_id
		 WHERE j.status = 'pending'
		   AND (j.next_retry_at IS NULL OR j.next_retry_at <= now())
		 ORDER BY j.created_at ASC
		 LIMIT 1
		   FOR UPDATE OF j SKIP LOCKED
	`
	var jobID string
	var versionID int64
	var rawContent *string
	var chunkSize, chunkOverlap int
	var chunkerVersion string
	var attempts, maxAttempts int

	err = tx.QueryRowContext(ctx, query).Scan(
		&jobID, &versionID, &rawContent, &chunkSize, &chunkOverlap, &chunkerVersion,
		&attempts, &maxAttempts,
	)
	if err != nil {
		return // No job available
	}

	// Mark as running
	_, err = tx.ExecContext(ctx, `UPDATE public.ai_ingestion_jobs
		SET status = 'running', locked_by = $2, locked_at = now(), attempts = attempts + 1,
		    next_retry_at = NULL, updated_at = now()
		WHERE id = $1`, jobID, s.workerID)
	if err != nil {
		return
	}
	if err := tx.Commit(); err != nil {
		return
	}
	_ = attempts
	_ = maxAttempts

	content := ""
	if rawContent != nil {
		content = *rawContent
	}

	chunks, err := ChunkMarkdown(content, chunkSize, chunkOverlap, chunkerVersion)
	if err != nil {
		s.failJob(ctx, jobID, fmt.Sprintf("chunking error: %v", err))
		return
	}
	if s.requireEmbedding && s.embedder == nil && len(chunks) > 0 {
		s.failJob(ctx, jobID, "embedding provider is required but not configured")
		return
	}

	// Save chunks
	for i, c := range chunks {
		h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s:%s", versionID, i, c.ContentHash, chunkerVersion)))
		chunkID := hex.EncodeToString(h[:])

		_, err := s.repo.db.ExecContext(ctx, `
			INSERT INTO public.ai_knowledge_chunks
			       (source_version_id, chunk_index, heading, content, chunk_id, content_hash)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (chunk_id) DO NOTHING
		`, versionID, i, c.Heading, c.Content, chunkID, c.ContentHash)
		if err != nil {
			s.failJob(ctx, jobID, fmt.Sprintf("save chunk error: %v", err))
			return
		}
	}

	_, _ = s.repo.db.ExecContext(ctx, `
		UPDATE public.ai_ingestion_jobs
		   SET total_chunks = $1, updated_at = now()
		 WHERE id = $2
	`, len(chunks), jobID)

	// Embed chunks if embedder is configured
	if s.embedder != nil && len(chunks) > 0 {
		var texts []string
		for _, c := range chunks {
			texts = append(texts, c.Content)
		}

		vectors, err := s.embedder.Embed(ctx, texts)
		if err != nil {
			s.failJob(ctx, jobID, fmt.Sprintf("embedding error: %v", err))
			return
		}
		if len(vectors) != len(chunks) {
			s.failJob(ctx, jobID, fmt.Sprintf("embedding error: expected %d vectors, got %d", len(chunks), len(vectors)))
			return
		}
		if len(vectors) == len(chunks) {
			for i, c := range chunks {
				h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s:%s", versionID, i, c.ContentHash, chunkerVersion)))
				chunkID := hex.EncodeToString(h[:])
				vecStr := floatVectorToString(vectors[i])

				if _, err := s.repo.db.ExecContext(ctx, `
					UPDATE public.ai_knowledge_chunks
					   SET embedding = $1::vector,
					       embedding_model = $2,
					       embedding_dimensions = $3
					 WHERE chunk_id = $4
				`, vecStr, s.embedder.Model(), s.embedder.Dimensions(), chunkID); err != nil {
					s.failJob(ctx, jobID, fmt.Sprintf("save embedding error: %v", err))
					return
				}
			}
			_, _ = s.repo.db.ExecContext(ctx, `
				UPDATE public.ai_ingestion_jobs
				   SET embedded_chunks = $1, updated_at = now()
				 WHERE id = $2
			`, len(chunks), jobID)
		}
	}

	// Mark completed
	_, _ = s.repo.db.ExecContext(ctx, `
		UPDATE public.ai_ingestion_jobs
		   SET status = 'completed', locked_by = NULL, locked_at = NULL, next_retry_at = NULL, updated_at = now()
		 WHERE id = $1
	`, jobID)
}

func (s *Service) failJob(ctx context.Context, jobID, errMsg string) {
	_, _ = s.repo.db.ExecContext(ctx, `
		UPDATE public.ai_ingestion_jobs
		   SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
		       error_message = $1,
		       next_retry_at = CASE WHEN attempts >= max_attempts THEN NULL
		                         ELSE now() + make_interval(secs => LEAST(900, (30 * power(2, GREATEST(attempts - 1, 0)))::int)) END,
		       locked_by = NULL, locked_at = NULL, updated_at = now()
		 WHERE id = $2
	`, errMsg, jobID)
}

func (s *Service) recoverStaleJobs(ctx context.Context) {
	_, _ = s.repo.db.ExecContext(ctx, `
		UPDATE public.ai_ingestion_jobs
		   SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
		       error_message = CASE WHEN attempts >= max_attempts THEN 'worker lease expired after maximum attempts' ELSE error_message END,
		       next_retry_at = CASE WHEN attempts >= max_attempts THEN NULL ELSE now() END,
		       locked_by = NULL, locked_at = NULL, updated_at = now()
		 WHERE status = 'running' AND locked_at < now() - interval '10 minutes'
	`)
}
