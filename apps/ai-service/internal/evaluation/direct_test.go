package evaluation

import (
	"context"
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

type fakeQuerier struct {
	tenant string
	err    error
}

func (f *fakeQuerier) Query(_ context.Context, req knowledge.QueryRequest, tenant string) (*knowledge.QueryResponse, error) {
	f.tenant = tenant
	if f.err != nil {
		return nil, f.err
	}
	return &knowledge.QueryResponse{Hits: []knowledge.QueryHit{{SourceKey: "doc-a"}}}, nil
}

func TestDirectQueryUsesCaseTenantThenDefault(t *testing.T) {
	q := &fakeQuerier{}
	fn := DirectQuery(q, "default-tenant")
	if _, err := fn(context.Background(), Case{Query: "x", Tenant: "case-tenant"}); err != nil || q.tenant != "case-tenant" {
		t.Fatalf("tenant=%s err=%v", q.tenant, err)
	}
	if _, err := fn(context.Background(), Case{Query: "x"}); err != nil || q.tenant != "default-tenant" {
		t.Fatalf("tenant=%s err=%v", q.tenant, err)
	}
}

func TestDirectQueryNilResponseOnError(t *testing.T) {
	fn := DirectQuery(&fakeQuerier{err: errors.New("embedding API error (status 401)")}, "t")
	resp, err := fn(context.Background(), Case{Query: "x"})
	if err == nil || len(resp.Hits) != 0 {
		t.Fatalf("want error and empty response, got %+v %v", resp, err)
	}
}
