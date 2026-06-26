package audit

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

// Logger writes structured audit events.
type Logger struct {
	slog     *slog.Logger
	dbWriter DBWriter
	service  string
}

// DBWriter persists audit events to PostgreSQL (optional).
type DBWriter interface {
	InsertAuditLog(ctx context.Context, e *domain.AuthEvent) error
}

// New creates an audit logger that writes to stdout and optionally to DB.
func New(service string, db DBWriter) *Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return &Logger{
		slog:     slog.New(handler),
		dbWriter: db,
		service:  service,
	}
}

// Event logs an authentication or authorization event.
func (l *Logger) Event(ctx context.Context, e *domain.AuthEvent) {
	e.Timestamp = time.Now()
	e.ServiceName = l.service

	l.slog.Info("audit",
		"event_type", e.EventType,
		"subject", e.Subject,
		"action", e.Action,
		"resource", e.Resource,
		"result", e.Result,
		"client_ip", e.ClientIP,
		"request_id", e.RequestID,
	)

	if l.dbWriter != nil {
		go func() {
			if err := l.dbWriter.InsertAuditLog(context.Background(), e); err != nil {
				l.slog.Warn("failed to persist audit log", "err", err)
			}
		}()
	}
}

// Shortcuts for common events

func (l *Logger) LoginAttempt(ctx context.Context, username string, success bool, providerID string, reason string, clientIP, userAgent, requestID string) {
	result := "success"
	if !success {
		result = "failure"
	}
	details := map[string]any{
		"provider_id": providerID,
	}
	if reason != "" {
		details["reason"] = reason
	}
	l.Event(ctx, &domain.AuthEvent{
		EventType: "login_attempt",
		Subject:   username,
		Action:    "login",
		Resource:  "auth",
		Result:    result,
		Details:   details,
		ClientIP:  clientIP,
		UserAgent: userAgent,
		RequestID: requestID,
	})
}

func (l *Logger) LoginBlocked(ctx context.Context, username string, reason string, clientIP, userAgent, requestID string) {
	l.Event(ctx, &domain.AuthEvent{
		EventType: "login_blocked",
		Subject:   username,
		Action:    "login",
		Resource:  "auth",
		Result:    "denied",
		Details:   map[string]any{"reason": reason},
		ClientIP:  clientIP,
		UserAgent: userAgent,
		RequestID: requestID,
	})
}

func (l *Logger) TokenIssued(ctx context.Context, subject, clientIP, requestID string) {
	l.Event(ctx, &domain.AuthEvent{
		EventType: "token_issued",
		Subject:   subject,
		Action:    "issue",
		Resource:  "token",
		Result:    "success",
		ClientIP:  clientIP,
		RequestID: requestID,
	})
}

func (l *Logger) TokenRefreshed(ctx context.Context, subject, clientIP, requestID string) {
	l.Event(ctx, &domain.AuthEvent{
		EventType: "token_refreshed",
		Subject:   subject,
		Action:    "refresh",
		Resource:  "token",
		Result:    "success",
		ClientIP:  clientIP,
		RequestID: requestID,
	})
}

func (l *Logger) TokenRevoked(ctx context.Context, subject, clientIP, requestID string) {
	l.Event(ctx, &domain.AuthEvent{
		EventType: "token_revoked",
		Subject:   subject,
		Action:    "revoke",
		Resource:  "token",
		Result:    "success",
		ClientIP:  clientIP,
		RequestID: requestID,
	})
}

func (l *Logger) ConsentGranted(ctx context.Context, subject, clientID string) {
	l.Event(ctx, &domain.AuthEvent{
		EventType: "consent_granted",
		Subject:   subject,
		Action:    "grant",
		Resource:  "consent",
		Result:    "success",
		Details:   map[string]any{"client_id": clientID},
	})
}

func (l *Logger) RateLimitHit(ctx context.Context, ip, requestID string) {
	l.Event(ctx, &domain.AuthEvent{
		EventType: "rate_limit_exceeded",
		Subject:   ip,
		Action:    "check",
		Resource:  "ratelimit",
		Result:    "denied",
		ClientIP:  ip,
		RequestID: requestID,
	})
}
