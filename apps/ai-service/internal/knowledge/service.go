package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"uuid"
)

type Service struct {
	repo     *Repository
	embedder Embedder
	reranker Reranker
	logger   *slog.Logger
	workerID string
	// requireEmbedding prevents a published version from being reported as
	// indexed when its embedding provider is unavailable. Development may keep
	// FTS-only querying, but production ingestion must be explicit about this
	// failure.
	requireEmbedding bool
	eventPublisher   EventPublisher
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

func (s *Service) SetReranker(reranker Reranker) {
	if s != nil {
		s.reranker = reranker
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
		topK = 5
	}
	if topK > 10 {
		topK = 10
	}

	var queryVector []float32
	if s.embedder != nil {
		vecs, err := s.embedder.Embed(ctx, []string{queryText})
		if err != nil {
			if s.requireEmbedding {
				return nil, fmt.Errorf("embedding query: %w", err)
			}
			s.logger.Warn("embedding failed, falling back to FTS-only", "err", err)
		} else if len(vecs) > 0 {
			queryVector = vecs[0]
		}
	} else if s.requireEmbedding {
		return nil, fmt.Errorf("embedding provider is required but not configured")
	}

	retrievalK := topK
	if s.reranker != nil {
		retrievalK = minInt(topK*3, 30)
	}
	hits, err := s.repo.HybridSearch(ctx, queryText, queryVector, tenantID, retrievalK)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
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
	if len(queryVector) > 0 && s.embedder != nil {
		modelUsed = s.embedder.Model()
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
		Rewritten:      false,
		RetrievedCount: retrievedCount,
		RerankedCount:  rerankedCount,
	}, nil
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
