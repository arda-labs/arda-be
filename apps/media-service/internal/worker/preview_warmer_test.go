package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/media-service/internal/domain"
)

type recordingWarmer struct {
	mu    sync.Mutex
	calls []string
	block chan struct{}
}

func (r *recordingWarmer) WarmPreview(_ context.Context, _ domain.FileScope, publicID string) error {
	if r.block != nil {
		<-r.block
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, publicID)
	return nil
}

func (r *recordingWarmer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func TestPreviewWarmerProcessesQueuedJobs(t *testing.T) {
	warmer := &recordingWarmer{}
	queue := NewPreviewWarmer(warmer, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)

	queue.Enqueue("t1", "o1", []string{"mf_1", "mf_2"})
	deadline := time.Now().Add(2 * time.Second)
	for warmer.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := warmer.count(); got != 2 {
		t.Fatalf("processed %d jobs, want 2", got)
	}
}

func TestPreviewWarmerCoalescesDuplicates(t *testing.T) {
	release := make(chan struct{})
	warmer := &recordingWarmer{block: release}
	queue := NewPreviewWarmer(warmer, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)

	queue.Enqueue("t1", "o1", []string{"mf_1"})
	// Wait until the first job is being processed, then enqueue duplicates.
	deadline := time.Now().Add(2 * time.Second)
	for warmer.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	queue.Enqueue("t1", "o1", []string{"mf_1", "mf_1"})
	close(release)

	time.Sleep(100 * time.Millisecond)
	if got := warmer.count(); got != 1 {
		t.Fatalf("processed %d jobs, want 1 (duplicates coalesced)", got)
	}
}

func TestPreviewWarmerDropsWhenQueueFull(t *testing.T) {
	release := make(chan struct{})
	warmer := &recordingWarmer{block: release}
	queue := NewPreviewWarmer(warmer, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)

	// First job occupies the single worker slot, the second fills the queue,
	// the rest must be dropped without blocking the caller.
	done := make(chan struct{})
	go func() {
		queue.Enqueue("t1", "o1", []string{"mf_1", "mf_2", "mf_3", "mf_4"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Enqueue blocked on a full queue")
	}
	close(release)
}

func TestPreviewWarmerNilSafety(t *testing.T) {
	var queue *PreviewWarmer
	queue.Enqueue("t1", "o1", []string{"mf_1"})
	queue.Run(context.Background())
}
