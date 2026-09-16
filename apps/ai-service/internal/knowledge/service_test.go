package knowledge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type fakeRewriter struct {
	variants []string
	err      error
	delay    time.Duration
}

func (f *fakeRewriter) Rewrite(ctx context.Context, tenantID, query string) ([]string, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.variants, f.err
}

type deadlineRecorder struct {
	deadline time.Time
	has      bool
}

func (r *deadlineRecorder) Rewrite(ctx context.Context, tenantID, query string) ([]string, error) {
	r.deadline, r.has = ctx.Deadline()
	return nil, nil
}

func testService(rewriter QueryRewriter) *Service {
	svc := NewService(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if rewriter != nil {
		svc.SetQueryRewriter(rewriter)
	}
	return svc
}

func TestStartQueryRewriteReturnsVariants(t *testing.T) {
	svc := testService(&fakeRewriter{variants: []string{"v1", "v2"}})

	select {
	case got := <-svc.startQueryRewrite(context.Background(), "tenant-1", "query"):
		if len(got) != 2 || got[0] != "v1" || got[1] != "v2" {
			t.Fatalf("expected [v1 v2], got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("rewrite channel never resolved")
	}
}

func TestStartQueryRewriteDisabledYieldsNoVariants(t *testing.T) {
	svc := testService(nil)

	select {
	case got := <-svc.startQueryRewrite(context.Background(), "tenant-1", "query"):
		if got != nil {
			t.Fatalf("expected nil variants without a rewriter, got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("rewrite channel never resolved")
	}
}

func TestStartQueryRewriteFailureYieldsNoVariants(t *testing.T) {
	svc := testService(&fakeRewriter{err: errors.New("model unavailable")})

	select {
	case got := <-svc.startQueryRewrite(context.Background(), "tenant-1", "query"):
		if got != nil {
			t.Fatalf("expected nil variants on rewrite failure, got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("rewrite channel never resolved")
	}
}

// The rewrite must never run longer than its budget: a slow model has to be
// cancelled by the deadline instead of consuming the retrieval budget.
func TestStartQueryRewriteBudgetIsBounded(t *testing.T) {
	recorder := &deadlineRecorder{}
	svc := testService(recorder)

	<-svc.startQueryRewrite(context.Background(), "tenant-1", "query")

	if !recorder.has {
		t.Fatal("expected the rewrite context to carry a deadline")
	}
	remaining := time.Until(recorder.deadline)
	if remaining <= 0 || remaining > rewriteBudget {
		t.Fatalf("expected a deadline within %v, got %v from now", rewriteBudget, remaining)
	}
}

func TestCollectVariantsFiltersAndCaps(t *testing.T) {
	ch := make(chan []string, 1)
	ch <- []string{" v1 ", "query", "v1", "", "v2", "v3", strings.Repeat("x", 2001)}

	got := collectVariants(context.Background(), "query", ch)
	want := []string{"v1", "v2"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestCollectVariantsStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := collectVariants(ctx, "query", make(chan []string)); got != nil {
		t.Fatalf("expected nil variants on cancelled context, got %v", got)
	}
}

func TestHasRetrievalBudget(t *testing.T) {
	if !hasRetrievalBudget(context.Background(), variantMinBudget) {
		t.Error("a request without a deadline must report budget available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), variantMinBudget/2)
	defer cancel()
	if hasRetrievalBudget(ctx, variantMinBudget) {
		t.Error("a nearly expired deadline must not report budget available")
	}
}

// A slow primary search must raise the bar for accepting variants instead of
// letting the next variant cross the deadline.
func TestVariantRequirementScalesWithPrimaryCost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if !hasRetrievalBudget(ctx, variantMinBudget) {
		t.Fatal("expected budget available right after the deadline is set")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel2()
	if hasRetrievalBudget(ctx2, 1800*time.Millisecond) {
		t.Error("expected an 1.8s variant requirement to be rejected with 1.5s left")
	}
}
