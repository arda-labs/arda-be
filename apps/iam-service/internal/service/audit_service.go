package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
)

// RetentionRule defines how long to keep each event type.
type RetentionRule struct {
	EventType string
	Duration  time.Duration
}

// AuditServiceConfig controls audit behavior.
type AuditServiceConfig struct {
	RetentionRules []RetentionRule
	CheckInterval  time.Duration
	DefaultRetention time.Duration
}

var DefaultAuditConfig = AuditServiceConfig{
	RetentionRules: []RetentionRule{
		{EventType: "login_attempt", Duration: 365 * 24 * time.Hour},
		{EventType: "login_blocked", Duration: 365 * 24 * time.Hour},
		{EventType: "session_created", Duration: 90 * 24 * time.Hour},
		{EventType: "session_revoked", Duration: 90 * 24 * time.Hour},
		{EventType: "token_issued", Duration: 90 * 24 * time.Hour},
		{EventType: "permission_denied", Duration: 180 * 24 * time.Hour},
	},
	CheckInterval:    24 * time.Hour,
	DefaultRetention: 365 * 24 * time.Hour,
}

// AuditService handles audit log queries and retention.
type AuditService struct {
	repo *repository.AuditRepository
	cfg  AuditServiceConfig
	logger  *slog.Logger
	stopCh  chan struct{}
	stopped sync.Once
}

// NewAuditService creates an audit service.
func NewAuditService(repo *repository.AuditRepository, cfg AuditServiceConfig) *AuditService {
	s := &AuditService{
		repo:   repo,
		cfg:    cfg,
		logger: slog.Default(),
		stopCh: make(chan struct{}),
	}
	go s.retentionLoop()
	return s
}

// Stop gracefully stops the retention background job.
func (s *AuditService) Stop() {
	s.stopped.Do(func() { close(s.stopCh) })
}

// Query returns paginated audit logs.
func (s *AuditService) Query(ctx context.Context, params repository.QueryParams) ([]domain.AuthEvent, int, error) {
	return s.repo.Query(ctx, params)
}

// Stats returns audit statistics.
func (s *AuditService) Stats(ctx context.Context, from, to time.Time) (*repository.AuditStats, error) {
	return s.repo.Stats(ctx, from, to)
}

// VerifyChain checks audit hash chain integrity.
func (s *AuditService) VerifyChain(ctx context.Context, from, to time.Time) (*repository.ChainVerification, error) {
	return s.repo.VerifyChain(ctx, from, to)
}

// retentionLoop periodically purges expired logs.
func (s *AuditService) retentionLoop() {
	ticker := time.NewTicker(s.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runRetention(context.Background())
		}
	}
}

func (s *AuditService) runRetention(ctx context.Context) {
	for _, rule := range s.cfg.RetentionRules {
		before := time.Now().Add(-rule.Duration)
		n, err := s.repo.PurgeByEventType(ctx, rule.EventType, before)
		if err != nil {
			s.logger.Warn("retention purge failed", "event_type", rule.EventType, "err", err)
		} else if n > 0 {
			s.logger.Info("audit retention purged", "event_type", rule.EventType, "count", n)
		}
	}

	// Purge everything else by default retention
	before := time.Now().Add(-s.cfg.DefaultRetention)
	n, err := s.repo.PurgeOlderThan(ctx, before)
	if err != nil {
		s.logger.Warn("retention purge default failed", "err", err)
	} else if n > 0 {
		s.logger.Info("audit default retention purged", "count", n)
	}
}
