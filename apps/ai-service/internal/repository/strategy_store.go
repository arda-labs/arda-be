package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type TenantRAGStrategy struct {
	TenantID            string    `json:"tenantId"`
	Strategy            string    `json:"strategy"`
	ParentChunkSize     int       `json:"parentChunkSize"`
	ChildChunkSize      int       `json:"childChunkSize"`
	SimilarityThreshold float32   `json:"similarityThreshold"`
	RerankerModel       string    `json:"rerankerModel"`
	TopK                int       `json:"topK"`
	TopN                int       `json:"topN"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type RAGStrategyStore interface {
	GetRAGStrategy(ctx context.Context, tenantID string) (*TenantRAGStrategy, error)
	SaveRAGStrategy(ctx context.Context, s TenantRAGStrategy) (*TenantRAGStrategy, error)
}

func defaultStrategy(tenantID string) TenantRAGStrategy {
	return TenantRAGStrategy{
		TenantID:            tenantID,
		Strategy:            "hierarchical",
		ParentChunkSize:     1024,
		ChildChunkSize:      256,
		SimilarityThreshold: 0.82,
		RerankerModel:       "cohere-rerank-v3.5",
		TopK:                20,
		TopN:                5,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
}

func (s *SQLRunStore) GetRAGStrategy(ctx context.Context, tenantID string) (*TenantRAGStrategy, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	var res TenantRAGStrategy
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, strategy, parent_chunk_size, child_chunk_size,
		       similarity_threshold, reranker_model, top_k, top_n,
		       created_at, updated_at
		FROM public.ai_tenant_rag_strategies
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&res.TenantID, &res.Strategy, &res.ParentChunkSize, &res.ChildChunkSize,
		&res.SimilarityThreshold, &res.RerankerModel, &res.TopK, &res.TopN,
		&res.CreatedAt, &res.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		def := defaultStrategy(tenantID)
		return &def, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant rag strategy: %w", err)
	}
	return &res, nil
}

func (s *SQLRunStore) SaveRAGStrategy(ctx context.Context, st TenantRAGStrategy) (*TenantRAGStrategy, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	var res TenantRAGStrategy
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO public.ai_tenant_rag_strategies (
			tenant_id, strategy, parent_chunk_size, child_chunk_size,
			similarity_threshold, reranker_model, top_k, top_n,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (tenant_id) DO UPDATE SET
			strategy = EXCLUDED.strategy,
			parent_chunk_size = EXCLUDED.parent_chunk_size,
			child_chunk_size = EXCLUDED.child_chunk_size,
			similarity_threshold = EXCLUDED.similarity_threshold,
			reranker_model = EXCLUDED.reranker_model,
			top_k = EXCLUDED.top_k,
			top_n = EXCLUDED.top_n,
			updated_at = now()
		RETURNING tenant_id, strategy, parent_chunk_size, child_chunk_size,
		          similarity_threshold, reranker_model, top_k, top_n,
		          created_at, updated_at
	`,
		st.TenantID, st.Strategy, st.ParentChunkSize, st.ChildChunkSize,
		st.SimilarityThreshold, st.RerankerModel, st.TopK, st.TopN,
	).Scan(
		&res.TenantID, &res.Strategy, &res.ParentChunkSize, &res.ChildChunkSize,
		&res.SimilarityThreshold, &res.RerankerModel, &res.TopK, &res.TopN,
		&res.CreatedAt, &res.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("save tenant rag strategy: %w", err)
	}
	return &res, nil
}
