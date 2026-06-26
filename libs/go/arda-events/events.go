package ardaevents

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

const (
	SubjectPlatformParameterChanged = "arda.platform.parameter.changed"
	SubjectPlatformLookupChanged    = "arda.platform.lookup.changed"
	SubjectFinanceTransactionPosted = "arda.finance.transaction.posted"
	SubjectIAMUserChanged           = "arda.iam.user.changed"
)

const (
	EventPlatformParameterChanged = "platform.parameter.changed"
	EventPlatformLookupChanged    = "platform.lookup.changed"
	EventFinanceTransactionPosted = "finance.transaction.posted"
	EventIAMUserChanged           = "iam.user.changed"
)

type Actor struct {
	UserID         string `json:"user_id,omitempty"`
	UserSubject    string `json:"user_subject,omitempty"`
	ServiceAccount string `json:"service_account,omitempty"`
}

type Envelope[T any] struct {
	ID            string    `json:"id"`
	EventCode     string    `json:"event_code"`
	SchemaVersion int       `json:"schema_version"`
	OccurredAt    time.Time `json:"occurred_at"`
	SourceService string    `json:"source_service"`
	TenantID      string    `json:"tenant_id,omitempty"`
	OrgID         string    `json:"org_id,omitempty"`
	RequestID     string    `json:"request_id,omitempty"`
	TraceID       string    `json:"trace_id,omitempty"`
	Locale        string    `json:"locale,omitempty"`
	Actor         Actor     `json:"actor,omitempty"`
	Payload       T         `json:"payload"`
}

type Options struct {
	ID            string
	SourceService string
	TenantID      string
	OrgID         string
	RequestID     string
	TraceID       string
	Locale        string
	Actor         Actor
	OccurredAt    time.Time
}

func NewEnvelope[T any](eventCode string, payload T, opts Options) (Envelope[T], error) {
	eventCode = strings.TrimSpace(eventCode)
	if eventCode == "" {
		return Envelope[T]{}, errors.New("event code is required")
	}
	sourceService := strings.TrimSpace(opts.SourceService)
	if sourceService == "" {
		return Envelope[T]{}, errors.New("source service is required")
	}
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = newID()
	}
	occurredAt := opts.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return Envelope[T]{
		ID:            id,
		EventCode:     eventCode,
		SchemaVersion: 1,
		OccurredAt:    occurredAt,
		SourceService: sourceService,
		TenantID:      strings.TrimSpace(opts.TenantID),
		OrgID:         strings.TrimSpace(opts.OrgID),
		RequestID:     strings.TrimSpace(opts.RequestID),
		TraceID:       strings.TrimSpace(opts.TraceID),
		Locale:        strings.TrimSpace(opts.Locale),
		Actor:         opts.Actor,
		Payload:       payload,
	}, nil
}

func (e Envelope[T]) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("event id is required")
	}
	if strings.TrimSpace(e.EventCode) == "" {
		return errors.New("event code is required")
	}
	if e.SchemaVersion <= 0 {
		return errors.New("schema version must be positive")
	}
	if e.OccurredAt.IsZero() {
		return errors.New("occurred_at is required")
	}
	if strings.TrimSpace(e.SourceService) == "" {
		return errors.New("source service is required")
	}
	return nil
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, strings.TrimSpace(requestID))
}

func RequestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

type requestIDKey struct{}

func newID() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
