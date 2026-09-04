package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type StrategyDTO struct {
	Strategy            string  `json:"strategy"`
	ParentChunkSize     int     `json:"parentChunkSize"`
	ChildChunkSize      int     `json:"childChunkSize"`
	SimilarityThreshold float32 `json:"similarityThreshold"`
	RerankerModel       string  `json:"rerankerModel"`
	TopK                int     `json:"topK"`
	TopN                int     `json:"topN"`
}

func handleGetStrategies(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	stStore, ok := store.(repository.RAGStrategyStore)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result": StrategyDTO{
				Strategy:            "hierarchical",
				ParentChunkSize:     1024,
				ChildChunkSize:      256,
				SimilarityThreshold: 0.82,
				RerankerModel:       "cohere-rerank-v3.5",
				TopK:                20,
				TopN:                5,
			},
		})
		return
	}
	st, err := stStore.GetRAGStrategy(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.strategies_fetch_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": StrategyDTO{
			Strategy:            st.Strategy,
			ParentChunkSize:     st.ParentChunkSize,
			ChildChunkSize:      st.ChildChunkSize,
			SimilarityThreshold: st.SimilarityThreshold,
			RerankerModel:       st.RerankerModel,
			TopK:                st.TopK,
			TopN:                st.TopN,
		},
	})
}

func handleUpdateStrategies(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	stStore, ok := store.(repository.RAGStrategyStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	var req StrategyDTO
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}

	updated, err := stStore.SaveRAGStrategy(r.Context(), repository.TenantRAGStrategy{
		TenantID:            scope.TenantID,
		Strategy:            strings.TrimSpace(req.Strategy),
		ParentChunkSize:     req.ParentChunkSize,
		ChildChunkSize:      req.ChildChunkSize,
		SimilarityThreshold: req.SimilarityThreshold,
		RerankerModel:       strings.TrimSpace(req.RerankerModel),
		TopK:                req.TopK,
		TopN:                req.TopN,
	})
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.strategies_save_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": StrategyDTO{
			Strategy:            updated.Strategy,
			ParentChunkSize:     updated.ParentChunkSize,
			ChildChunkSize:      updated.ChildChunkSize,
			SimilarityThreshold: updated.SimilarityThreshold,
			RerankerModel:       updated.RerankerModel,
			TopK:                updated.TopK,
			TopN:                updated.TopN,
		},
	})
}
