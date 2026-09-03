package knowledge

import (
	"context"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

type InProcessRAGAdapter struct {
	svc *Service
}

func NewInProcessRAGAdapter(svc *Service) *InProcessRAGAdapter {
	return &InProcessRAGAdapter{svc: svc}
}

func (a *InProcessRAGAdapter) Search(ctx context.Context, md metadata.Context, query string, topK int) (*svcclient.RAGResponse, error) {
	res, err := a.svc.Query(ctx, QueryRequest{Query: query, TopK: topK}, md.TenantID)
	if err != nil {
		return nil, err
	}
	var hits []svcclient.RAGHit
	for _, h := range res.Hits {
		hits = append(hits, svcclient.RAGHit{
			SourceID:        int(h.SourceID),
			SourceVersionID: int(h.SourceVersionID),
			Version:         h.Version,
			Title:           h.Title,
			Heading:         h.Heading,
			Content:         h.Content,
			Score:           h.Score,
			Citation:        h.Citation,
		})
	}
	return &svcclient.RAGResponse{
		RunID:          res.RunID,
		Hits:           hits,
		LatencyMs:      res.LatencyMs,
		Rewritten:      res.Rewritten,
		RetrievedCount: res.RetrievedCount,
		RerankedCount:  res.RerankedCount,
	}, nil
}

func (a *InProcessRAGAdapter) Feedback(ctx context.Context, md metadata.Context, runID string, helpful bool, comment string) (*svcclient.FeedbackOut, error) {
	var c *string
	if comment != "" {
		c = &comment
	}
	fb, err := a.svc.Repo().SaveFeedback(ctx, runID, helpful, c)
	if err != nil {
		return nil, err
	}
	return &svcclient.FeedbackOut{
		ID:        fb.ID,
		RunID:     fb.RunID,
		Helpful:   fb.Helpful,
		Comment:   comment,
		CreatedAt: fb.CreatedAt.Format(time.RFC3339),
	}, nil
}
