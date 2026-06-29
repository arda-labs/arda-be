package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
)

type NotificationService struct {
	repo *repository.NotificationRepository
}

func NewNotificationService(repo *repository.NotificationRepository) *NotificationService {
	return &NotificationService{repo: repo}
}

type AcceptInput struct {
	TenantID       string             `json:"tenant_id"`
	IdempotencyKey string             `json:"idempotency_key"`
	SourceService  string             `json:"source_service"`
	SourceEventID  string             `json:"source_event_id"`
	EventType      string             `json:"event_type"`
	TemplateKey    string             `json:"template_key"`
	Channels       []string           `json:"channels"`
	Recipients     []domain.Recipient `json:"recipients"`
	Payload        map[string]any     `json:"payload"`
	CorrelationID  string             `json:"correlation_id"`
	Priority       int                `json:"priority"`
}

func (s *NotificationService) Accept(ctx context.Context, in AcceptInput) (*domain.Notification, error) {
	if err := validateAccept(in); err != nil {
		return nil, err
	}

	recipientsJSON, err := json.Marshal(in.Recipients)
	if err != nil {
		return nil, err
	}
	channelsJSON, err := json.Marshal(in.Channels)
	if err != nil {
		return nil, err
	}
	payload := in.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	n := &domain.Notification{
		PublicID:       newPublicID(),
		TenantID:       strings.TrimSpace(in.TenantID),
		SourceService:  strings.TrimSpace(in.SourceService),
		SourceEventID:  strings.TrimSpace(in.SourceEventID),
		EventType:      strings.TrimSpace(in.EventType),
		Recipients:     recipientsJSON,
		Channels:       channelsJSON,
		TemplateKey:    strings.TrimSpace(in.TemplateKey),
		Payload:        payloadJSON,
		Status:         domain.NotificationStatusAccepted,
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		CorrelationID:  strings.TrimSpace(in.CorrelationID),
		Priority:       in.Priority,
	}

	deliveries := make([]domain.Delivery, 0, len(in.Channels)*len(in.Recipients))
	for _, ch := range in.Channels {
		for _, r := range in.Recipients {
			destination, err := json.Marshal(r)
			if err != nil {
				return nil, err
			}
			deliveries = append(deliveries, domain.Delivery{
				TenantID:    n.TenantID,
				Channel:     strings.TrimSpace(ch),
				Destination: destination,
				MaxAttempts: 6,
			})
		}
	}

	return s.repo.CreateNotificationWithDeliveries(ctx, n, deliveries)
}

func (s *NotificationService) GetByPublicID(ctx context.Context, publicID string) (*domain.Notification, error) {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		return nil, errors.New("notification id is required")
	}
	return s.repo.GetNotificationByPublicID(ctx, publicID)
}

func validateAccept(in AcceptInput) error {
	if strings.TrimSpace(in.TenantID) == "" {
		return errors.New("tenant_id is required")
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return errors.New("idempotency_key is required")
	}
	if strings.TrimSpace(in.TemplateKey) == "" {
		return errors.New("template_key is required")
	}
	if len(in.Channels) == 0 {
		return errors.New("channels is required")
	}
	if len(in.Recipients) == 0 {
		return errors.New("recipients is required")
	}
	for _, ch := range in.Channels {
		switch strings.TrimSpace(ch) {
		case domain.ChannelEmail, domain.ChannelWebhook, domain.ChannelSMS, domain.ChannelPush:
		default:
			return errors.New("unsupported channel: " + ch)
		}
	}
	for _, r := range in.Recipients {
		if strings.TrimSpace(r.Type) == "" {
			return errors.New("recipient type is required")
		}
		if strings.TrimSpace(r.Address) == "" && strings.TrimSpace(r.UserID) == "" {
			return errors.New("recipient address or user_id is required")
		}
	}
	return nil
}

func newPublicID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "ntf_" + hex.EncodeToString(b[:])
}
