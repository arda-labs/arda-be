package worker

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/arda-labs/arda/apps/media-service/internal/domain"
)

// WarmPreviewer converts and caches the PDF preview of a single file. It is
// implemented by the media service; the worker only schedules calls so the
// package stays framework-free.
type WarmPreviewer interface {
	WarmPreview(ctx context.Context, scope domain.FileScope, publicID string) error
}

type previewJob struct {
	tenantID string
	orgID    string
	publicID string
}

// PreviewWarmer pre-converts attached office documents in the background.
// Conversions run on a single goroutine because LibreOffice serializes them
// anyway; the queue is bounded and never blocks the attach request.
type PreviewWarmer struct {
	warmer WarmPreviewer
	logger *slog.Logger
	jobs   chan previewJob

	mu     sync.Mutex
	queued map[string]struct{}
}

func NewPreviewWarmer(warmer WarmPreviewer, queueSize int) *PreviewWarmer {
	if queueSize <= 0 {
		queueSize = 256
	}
	return &PreviewWarmer{
		warmer: warmer,
		logger: slog.Default(),
		jobs:   make(chan previewJob, queueSize),
		queued: make(map[string]struct{}),
	}
}

// Enqueue never blocks: previews are best-effort, so a full queue drops the
// job (the on-demand /preview request still converts it later) and duplicates
// are coalesced while a job is queued or running.
func (w *PreviewWarmer) Enqueue(tenantID, orgID string, publicIDs []string) {
	if w == nil || w.warmer == nil {
		return
	}
	for _, publicID := range publicIDs {
		publicID = strings.TrimSpace(publicID)
		if publicID == "" {
			continue
		}
		key := tenantID + "|" + orgID + "|" + publicID
		w.mu.Lock()
		if _, exists := w.queued[key]; exists {
			w.mu.Unlock()
			continue
		}
		select {
		case w.jobs <- previewJob{tenantID: tenantID, orgID: orgID, publicID: publicID}:
			w.queued[key] = struct{}{}
		default:
			w.logger.Warn("preview warmup queue full; dropping job", "public_id", publicID)
		}
		w.mu.Unlock()
	}
}

// Run consumes the queue until the context is cancelled.
func (w *PreviewWarmer) Run(ctx context.Context) {
	if w == nil || w.warmer == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-w.jobs:
			key := job.tenantID + "|" + job.orgID + "|" + job.publicID
			scope := domain.FileScope{TenantID: job.tenantID, OrgID: job.orgID}
			if err := w.warmer.WarmPreview(ctx, scope, job.publicID); err != nil {
				w.logger.Warn("preview warmup failed", "public_id", job.publicID, "err", err)
			}
			w.mu.Lock()
			delete(w.queued, key)
			w.mu.Unlock()
		}
	}
}
