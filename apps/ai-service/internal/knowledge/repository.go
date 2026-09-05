package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListSources(ctx context.Context, tenantID string, includeDeleted bool) ([]Source, error) {
	query := `
		SELECT s.id, s.tenant_id, s.title, s.description, s.source_type, s.scope,
		       s.classification, s.language, s.tags, s.owner_id, s.effective_from,
		       s.effective_to, s.active_version_id, s.deleted_at, s.created_by,
		       s.created_at, s.updated_at, v.status, v.version
		  FROM public.ai_knowledge_sources s
		  LEFT JOIN public.ai_knowledge_source_versions v ON v.id = s.active_version_id
	`
	query += " WHERE (s.tenant_id = $1 OR s.tenant_id IS NULL)"
	if !includeDeleted {
		query += " AND s.deleted_at IS NULL"
	}
	query += " ORDER BY s.created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()

	var sources []Source
	for rows.Next() {
		var s Source
		var tags pq.StringArray
		err := rows.Scan(
			&s.ID, &s.TenantID, &s.Title, &s.Description, &s.SourceType, &s.Scope,
			&s.Classification, &s.Language, &tags, &s.OwnerID, &s.EffectiveFrom,
			&s.EffectiveTo, &s.ActiveVersionID, &s.DeletedAt, &s.CreatedBy,
			&s.CreatedAt, &s.UpdatedAt, &s.Status, &s.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		s.Tags = []string(tags)
		sources = append(sources, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sources: %w", err)
	}
	return sources, nil
}

func (r *Repository) GetSource(ctx context.Context, id int64, tenantID string) (*Source, error) {
	query := `
		SELECT s.id, s.tenant_id, s.title, s.description, s.source_type, s.scope,
		       s.classification, s.language, s.tags, s.owner_id, s.effective_from,
		       s.effective_to, s.active_version_id, s.deleted_at, s.created_by,
		       s.created_at, s.updated_at, v.status, v.version
		  FROM public.ai_knowledge_sources s
		  LEFT JOIN public.ai_knowledge_source_versions v ON v.id = s.active_version_id
		 WHERE s.id = $1 AND (s.tenant_id = $2 OR s.tenant_id IS NULL) AND s.deleted_at IS NULL
	`
	var s Source
	var tags pq.StringArray
	err := r.db.QueryRowContext(ctx, query, id, tenantID).Scan(
		&s.ID, &s.TenantID, &s.Title, &s.Description, &s.SourceType, &s.Scope,
		&s.Classification, &s.Language, &tags, &s.OwnerID, &s.EffectiveFrom,
		&s.EffectiveTo, &s.ActiveVersionID, &s.DeletedAt, &s.CreatedBy,
		&s.CreatedAt, &s.UpdatedAt, &s.Status, &s.Version,
	)
	if err != nil {
		return nil, err
	}
	s.Tags = []string(tags)
	return &s, nil
}

func (r *Repository) CreateSource(ctx context.Context, data SourceCreate, tenantID, createdBy string) (*Source, error) {
	query := `
		INSERT INTO public.ai_knowledge_sources
		       (tenant_id, title, description, source_type, scope, classification, language, tags, owner_id, effective_from, effective_to, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, tenant_id, title, description, source_type, scope, classification, language, tags, owner_id, effective_from, effective_to, active_version_id, deleted_at, created_by, created_at, updated_at
	`
	var s Source
	var tags pq.StringArray
	var tID *string
	if tenantID != "" {
		tID = &tenantID
	}
	err := r.db.QueryRowContext(ctx, query,
		tID, data.Title, data.Description, data.SourceType, data.Scope,
		data.Classification, data.Language, pq.Array(data.Tags), data.OwnerID,
		data.EffectiveFrom, data.EffectiveTo, createdBy,
	).Scan(
		&s.ID, &s.TenantID, &s.Title, &s.Description, &s.SourceType, &s.Scope,
		&s.Classification, &s.Language, &tags, &s.OwnerID, &s.EffectiveFrom,
		&s.EffectiveTo, &s.ActiveVersionID, &s.DeletedAt, &s.CreatedBy,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create source: %w", err)
	}
	s.Tags = []string(tags)
	return &s, nil
}

func (r *Repository) SoftDeleteSource(ctx context.Context, id int64, tenantID string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE public.ai_knowledge_sources SET deleted_at = now(), updated_at = now() WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`, id, tenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) ListVersions(ctx context.Context, sourceID int64, tenantID string) ([]Version, error) {
	query := `
		SELECT v.id, v.source_id, v.version, v.status, v.content_type, v.content, v.content_url,
		       v.chunker_version, v.chunk_size, v.chunk_overlap, v.content_hash, v.status_history,
		       v.created_by, v.created_at, v.updated_at
		 FROM public.ai_knowledge_source_versions v
		 JOIN public.ai_knowledge_sources s ON s.id = v.source_id
		 WHERE v.source_id = $1 AND (s.tenant_id = $2 OR s.tenant_id IS NULL)
		 ORDER BY v.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, sourceID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()

	var versions []Version
	for rows.Next() {
		var v Version
		var rawHistory []byte
		err := rows.Scan(
			&v.ID, &v.SourceID, &v.Version, &v.Status, &v.ContentType, &v.Content,
			&v.ContentURL, &v.ChunkerVersion, &v.ChunkSize, &v.ChunkOverlap,
			&v.ContentHash, &rawHistory, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		if len(rawHistory) > 0 {
			_ = json.Unmarshal(rawHistory, &v.StatusHistory)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate versions: %w", err)
	}
	return versions, nil
}

func (r *Repository) GetVersion(ctx context.Context, sourceID, versionID int64, tenantID string) (*Version, error) {
	query := `
		SELECT v.id, v.source_id, v.version, v.status, v.content_type, v.content, v.content_url,
		       v.chunker_version, v.chunk_size, v.chunk_overlap, v.content_hash, v.status_history,
		       v.created_by, v.created_at, v.updated_at
		 FROM public.ai_knowledge_source_versions v
		 JOIN public.ai_knowledge_sources s ON s.id = v.source_id
		 WHERE v.source_id = $1 AND v.id = $2 AND (s.tenant_id = $3 OR s.tenant_id IS NULL)
	`
	var v Version
	var rawHistory []byte
	err := r.db.QueryRowContext(ctx, query, sourceID, versionID, tenantID).Scan(
		&v.ID, &v.SourceID, &v.Version, &v.Status, &v.ContentType, &v.Content,
		&v.ContentURL, &v.ChunkerVersion, &v.ChunkSize, &v.ChunkOverlap,
		&v.ContentHash, &rawHistory, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(rawHistory) > 0 {
		_ = json.Unmarshal(rawHistory, &v.StatusHistory)
	}
	return &v, nil
}

func (r *Repository) CreateVersion(ctx context.Context, sourceID int64, tenantID string, data VersionCreate, createdBy string) (*Version, error) {
	var contentHash *string
	if data.Content != nil && *data.Content != "" {
		h := sha256Hash(*data.Content)
		contentHash = &h
	}

	var strategy *string
	var chunkSize, chunkOverlap *int
	if data.ChunkerConfig != nil {
		if data.ChunkerConfig.Strategy != "" {
			strategy = &data.ChunkerConfig.Strategy
		}
		if data.ChunkerConfig.ChunkSize > 0 {
			chunkSize = &data.ChunkerConfig.ChunkSize
		}
		if data.ChunkerConfig.ChunkOverlap > 0 {
			chunkOverlap = &data.ChunkerConfig.ChunkOverlap
		}
	}

	query := `
		INSERT INTO public.ai_knowledge_source_versions
		       (source_id, version, status, content_type, content, content_url, chunker_version, chunk_size, chunk_overlap, content_hash, created_by)
		SELECT $1, $2, 'DRAFT', $3, $4, $5, $6, $7, $8, $9, $10
		 WHERE EXISTS (SELECT 1 FROM public.ai_knowledge_sources s WHERE s.id = $1 AND (s.tenant_id = $11 OR s.tenant_id IS NULL))
		RETURNING id, source_id, version, status, content_type, content, content_url, chunker_version, chunk_size, chunk_overlap, content_hash, status_history, created_by, created_at, updated_at
	`
	var v Version
	var rawHistory []byte
	err := r.db.QueryRowContext(ctx, query,
		sourceID, data.Version, data.ContentType, data.Content, data.ContentURL,
		strategy, chunkSize, chunkOverlap, contentHash, createdBy, tenantID,
	).Scan(
		&v.ID, &v.SourceID, &v.Version, &v.Status, &v.ContentType, &v.Content,
		&v.ContentURL, &v.ChunkerVersion, &v.ChunkSize, &v.ChunkOverlap,
		&v.ContentHash, &rawHistory, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create version: %w", err)
	}
	if len(rawHistory) > 0 {
		_ = json.Unmarshal(rawHistory, &v.StatusHistory)
	}
	return &v, nil
}

func (r *Repository) ReviewVersion(ctx context.Context, sourceID, versionID int64, tenantID string, req ReviewRequest, actor string) (*Version, error) {
	status := "APPROVED"
	if strings.ToLower(req.Decision) == "reject" {
		status = "REJECTED"
	}

	query := `
		UPDATE public.ai_knowledge_source_versions
		   SET status = $1,
		       status_history = status_history || jsonb_build_object(
		           'transition', $1::text,
		           'at', now(),
		           'by', $2::text,
		           'reason', $3::text
		       )::jsonb,
		       updated_at = now()
		 WHERE source_id = $4 AND id = $5 AND status IN ('DRAFT', 'PENDING_REVIEW')
		   AND EXISTS (SELECT 1 FROM public.ai_knowledge_sources s WHERE s.id = $4 AND (s.tenant_id = $6 OR s.tenant_id IS NULL))
		 RETURNING id, source_id, version, status, content_type, content, content_url, chunker_version, chunk_size, chunk_overlap, content_hash, status_history, created_by, created_at, updated_at
	`
	var v Version
	var rawHistory []byte
	err := r.db.QueryRowContext(ctx, query, status, actor, req.Reason, sourceID, versionID, tenantID).Scan(
		&v.ID, &v.SourceID, &v.Version, &v.Status, &v.ContentType, &v.Content,
		&v.ContentURL, &v.ChunkerVersion, &v.ChunkSize, &v.ChunkOverlap,
		&v.ContentHash, &rawHistory, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(rawHistory) > 0 {
		_ = json.Unmarshal(rawHistory, &v.StatusHistory)
	}
	return &v, nil
}

func (r *Repository) PublishVersion(ctx context.Context, sourceID, versionID int64, tenantID string, actor string) (*PublishResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1. Create ingestion job
	var jobID string
	err = tx.QueryRowContext(ctx, `
	INSERT INTO public.ai_ingestion_jobs (source_version_id, status)
		SELECT $1, 'pending'
		 WHERE EXISTS (SELECT 1 FROM public.ai_knowledge_source_versions v JOIN public.ai_knowledge_sources s ON s.id = v.source_id WHERE v.id = $1 AND v.source_id = $2 AND v.status = 'APPROVED' AND (s.tenant_id = $3 OR s.tenant_id IS NULL))
		RETURNING id::text
	`, versionID, sourceID, tenantID).Scan(&jobID)
	if err != nil {
		return nil, fmt.Errorf("create ingestion job: %w", err)
	}

	// 2. Update version status to PUBLISHED
	_, err = tx.ExecContext(ctx, `
		UPDATE public.ai_knowledge_source_versions
		   SET status = 'PUBLISHED',
		       status_history = status_history || jsonb_build_object('transition', 'PUBLISHED', 'at', now(), 'by', $1::text)::jsonb,
		       updated_at = now()
		 WHERE source_id = $2 AND id = $3 AND EXISTS (SELECT 1 FROM public.ai_knowledge_sources s WHERE s.id = $2 AND (s.tenant_id = $4 OR s.tenant_id IS NULL))
	`, actor, sourceID, versionID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("update version status: %w", err)
	}

	// 3. Set active_version_id on source
	_, err = tx.ExecContext(ctx, `
		UPDATE public.ai_knowledge_sources
		   SET active_version_id = $1, updated_at = now()
		 WHERE id = $2 AND (tenant_id = $3 OR tenant_id IS NULL)
	`, versionID, sourceID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("update source active version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &PublishResult{
		JobID:     jobID,
		VersionID: versionID,
		Status:    "pending",
	}, nil
}

func (r *Repository) GetJob(ctx context.Context, jobID, tenantID string) (*Job, error) {
	query := `
		SELECT id::text, source_version_id, status, locked_by, locked_at,
		       attempts, max_attempts, error_message, total_chunks, embedded_chunks,
		       next_retry_at, created_at, updated_at
		 FROM public.ai_ingestion_jobs j
		 JOIN public.ai_knowledge_source_versions v ON v.id = j.source_version_id
		 JOIN public.ai_knowledge_sources s ON s.id = v.source_id
		 WHERE j.id = $1 AND (s.tenant_id = $2 OR s.tenant_id IS NULL)
	`
	var j Job
	err := r.db.QueryRowContext(ctx, query, jobID, tenantID).Scan(
		&j.ID, &j.SourceVersionID, &j.Status, &j.LockedBy, &j.LockedAt,
		&j.Attempts, &j.MaxAttempts, &j.ErrorMessage, &j.TotalChunks,
		&j.EmbeddedChunks, &j.NextRetryAt, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *Repository) SaveRun(ctx context.Context, tenantID, query string, retrievedCount, rerankedCount, latencyMs int, hitIDs []int64, modelUsed string) (string, error) {
	var runID string
	var tID *string
	if tenantID != "" {
		tID = &tenantID
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO public.ai_rag_runs (tenant_id, query, retrieved_count, reranked_count, hit_ids, latency_ms, model_used)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text
	`, tID, query, retrievedCount, rerankedCount, pq.Array(hitIDs), latencyMs, modelUsed).Scan(&runID)
	return runID, err
}

func (r *Repository) SaveFeedback(ctx context.Context, tenantID, runID string, helpful bool, comment *string) (*FeedbackOut, error) {
	var out FeedbackOut
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO public.ai_rag_feedback (run_id, helpful, comment)
		SELECT $1, $2, $3
		 WHERE EXISTS (
			SELECT 1 FROM public.ai_rag_runs r
			 WHERE r.id = $1 AND (r.tenant_id = $4 OR r.tenant_id IS NULL)
		 )
		RETURNING id::text, run_id::text, helpful, comment, created_at
	`, runID, helpful, comment, tenantID).Scan(&out.ID, &out.RunID, &out.Helpful, &out.Comment, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type candidateHit struct {
	SourceID        int64
	SourceKey       string
	SourceVersionID int64
	Version         string
	Title           string
	Heading         string
	Content         string
	ChunkID         string
	EffectiveFrom   *time.Time
	EffectiveTo     *time.Time
	URL             *string
}

func (r *Repository) HybridSearch(ctx context.Context, queryText string, queryVector []float32, tenantID string, topK int) ([]QueryHit, error) {
	if topK <= 0 {
		topK = 5
	}
	if topK > 10 {
		topK = 10
	}

	var tID *string
	if tenantID != "" {
		tID = &tenantID
	}

	// 1. Vector leg
	type rankedID struct {
		chunkID string
		rank    int
	}
	var vectorRanks []rankedID
	candidateMap := make(map[string]candidateHit)

	if len(queryVector) > 0 {
		vecStr := floatVectorToString(queryVector)
		vecQuery := `
			SELECT c.chunk_id, c.source_version_id, s.id AS source_id, s.source_key, v.version, s.title, COALESCE(c.heading, ''), c.content,
			       s.effective_from, s.effective_to, v.content_url
			  FROM public.ai_knowledge_chunks c
			  JOIN public.ai_knowledge_source_versions v ON v.id = c.source_version_id
			  JOIN public.ai_knowledge_sources s ON s.id = v.source_id
			 WHERE c.embedding IS NOT NULL
			   AND (s.tenant_id IS NOT DISTINCT FROM $1 OR s.tenant_id IS NULL)
			   AND s.scope IN ('tenant', 'global')
			   AND s.active_version_id = v.id
			   AND v.status = 'PUBLISHED'
			   AND (s.effective_from IS NULL OR s.effective_from <= now())
			   AND (s.effective_to IS NULL OR s.effective_to > now())
			   AND s.deleted_at IS NULL
			 ORDER BY c.embedding <=> $2::vector
			 LIMIT $3
		`
		rows, err := r.db.QueryContext(ctx, vecQuery, tID, vecStr, topK*2)
		if err != nil {
			return nil, fmt.Errorf("vector knowledge search: %w", err)
		}
		defer rows.Close()
		rank := 1
		for rows.Next() {
			var c candidateHit
			if err := rows.Scan(&c.ChunkID, &c.SourceVersionID, &c.SourceID, &c.SourceKey, &c.Version, &c.Title, &c.Heading, &c.Content, &c.EffectiveFrom, &c.EffectiveTo, &c.URL); err != nil {
				return nil, fmt.Errorf("scan vector knowledge result: %w", err)
			}
			vectorRanks = append(vectorRanks, rankedID{chunkID: c.ChunkID, rank: rank})
			candidateMap[c.ChunkID] = c
			rank++
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate vector knowledge results: %w", err)
		}
	}

	// 2. FTS leg
	var ftsRanks []rankedID
	ftsQuery := `
		SELECT c.chunk_id, c.source_version_id, s.id AS source_id, s.source_key, v.version, s.title, COALESCE(c.heading, ''), c.content,
		       s.effective_from, s.effective_to, v.content_url
		  FROM public.ai_knowledge_chunks c
		  JOIN public.ai_knowledge_source_versions v ON v.id = c.source_version_id
		  JOIN public.ai_knowledge_sources s ON s.id = v.source_id
		 WHERE (to_tsvector('simple', c.content) @@ plainto_tsquery('simple', $2)
		        OR to_tsvector('simple', s.title || ' ' || COALESCE(c.heading, '')) @@ plainto_tsquery('simple', $2))
		   AND (s.tenant_id IS NOT DISTINCT FROM $1 OR s.tenant_id IS NULL)
		   AND s.scope IN ('tenant', 'global')
		   AND s.active_version_id = v.id
		   AND v.status = 'PUBLISHED'
		   AND (s.effective_from IS NULL OR s.effective_from <= now())
		   AND (s.effective_to IS NULL OR s.effective_to > now())
		   AND s.deleted_at IS NULL
		 LIMIT $3
	`
	rows, err := r.db.QueryContext(ctx, ftsQuery, tID, queryText, topK*2)
	if err != nil {
		return nil, fmt.Errorf("full-text knowledge search: %w", err)
	}
	defer rows.Close()
	rank := 1
	for rows.Next() {
		var c candidateHit
		if err := rows.Scan(&c.ChunkID, &c.SourceVersionID, &c.SourceID, &c.SourceKey, &c.Version, &c.Title, &c.Heading, &c.Content, &c.EffectiveFrom, &c.EffectiveTo, &c.URL); err != nil {
			return nil, fmt.Errorf("scan full-text knowledge result: %w", err)
		}
		ftsRanks = append(ftsRanks, rankedID{chunkID: c.ChunkID, rank: rank})
		candidateMap[c.ChunkID] = c
		rank++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate full-text knowledge results: %w", err)
	}

	// 3. RRF Fusion
	rrfScores := make(map[string]float64)
	const k = 60.0
	for _, vr := range vectorRanks {
		rrfScores[vr.chunkID] += 1.0 / (k + float64(vr.rank))
	}
	for _, fr := range ftsRanks {
		rrfScores[fr.chunkID] += 1.0 / (k + float64(fr.rank))
	}

	type scoredHit struct {
		chunkID string
		score   float64
	}
	var scored []scoredHit
	for chunkID, score := range rrfScores {
		scored = append(scored, scoredHit{chunkID: chunkID, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if len(scored) > topK {
		scored = scored[:topK]
	}

	var results []QueryHit
	for _, s := range scored {
		c := candidateMap[s.chunkID]
		citation := fmt.Sprintf("[%d:%s]", c.SourceID, c.Heading)
		if c.Heading == "" {
			citation = fmt.Sprintf("[%d]", c.SourceID)
		}
		results = append(results, QueryHit{
			SourceID:        c.SourceID,
			SourceKey:       c.SourceKey,
			SourceVersionID: c.SourceVersionID,
			Version:         c.Version,
			Title:           c.Title,
			Heading:         c.Heading,
			Content:         c.Content,
			Score:           s.score,
			Citation:        citation,
			CitationRef: CitationRef{
				SourceID: c.SourceID, SourceVersionID: c.SourceVersionID,
				Title: c.Title, Version: c.Version, Heading: c.Heading,
				EffectiveFrom: c.EffectiveFrom, EffectiveTo: c.EffectiveTo,
				URL: c.URL, Locator: citation,
			},
		})
	}

	return results, nil
}

func floatVectorToString(vec []float32) string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, v := range vec {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%f", v))
	}
	sb.WriteString("]")
	return sb.String()
}
