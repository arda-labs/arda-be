package knowledge

import (
	"context"
	"testing"
)

func TestSQLSearcherRequiresTenantAndQuery(t *testing.T) {
	searcher := &SQLSearcher{}
	if _, err := searcher.Search(context.Background(), "", "query", 5); err == nil {
		t.Fatal("expected missing tenant error")
	}
	if _, err := searcher.Search(context.Background(), "tenant", "", 5); err == nil {
		t.Fatal("expected missing query error")
	}
}
