package evaluation

import (
	"context"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

// Querier is the in-process retrieval surface (knowledge.Service) used by the
// scheduled evaluation job.
type Querier interface {
	Query(ctx context.Context, req knowledge.QueryRequest, tenantID string) (*knowledge.QueryResponse, error)
}

// DirectQuery runs retrieval in-process. It mirrors HTTPQuery's tenant rule:
// the case's tenant wins, otherwise defaultTenant.
func DirectQuery(q Querier, defaultTenant string) QueryFunc {
	return func(ctx context.Context, c Case) (knowledge.QueryResponse, error) {
		tenant := strings.TrimSpace(c.Tenant)
		if tenant == "" {
			tenant = defaultTenant
		}
		resp, err := q.Query(ctx, knowledge.QueryRequest{Query: c.Query, TopK: 10}, tenant)
		if err != nil || resp == nil {
			return knowledge.QueryResponse{}, err
		}
		return *resp, nil
	}
}
