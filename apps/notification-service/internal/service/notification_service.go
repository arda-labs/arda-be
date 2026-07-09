package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/push"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
)

type NotificationService struct {
	repo       *repository.NotificationRepository
	pushSender *push.Sender
}

func NewNotificationService(repo *repository.NotificationRepository, pushSender *push.Sender) *NotificationService {
	return &NotificationService{repo: repo, pushSender: pushSender}
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
	Type           string             `json:"type"`
	TitleKey       string             `json:"title_key"`
	BodyKey        string             `json:"body_key"`
	Href           string             `json:"href"`
	Params         map[string]any     `json:"params"`
}

type PushSubscribeInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
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
	inboxItems := make([]domain.InboxItem, 0, len(in.Recipients))
	params := in.Params
	if params == nil {
		params = payload
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	for _, ch := range in.Channels {
		ch = strings.TrimSpace(ch)
		for _, r := range in.Recipients {
			if ch == domain.ChannelInApp {
				if strings.TrimSpace(r.UserID) == "" {
					continue
				}
				inboxItems = append(inboxItems, domain.InboxItem{
					PublicID: newInboxPublicID(),
					TenantID: n.TenantID,
					UserID:   strings.TrimSpace(r.UserID),
					Type:     notificationType(in.Type),
					TitleKey: notificationKey(in.TitleKey, n.TemplateKey, "title"),
					BodyKey:  notificationKey(in.BodyKey, n.TemplateKey, "body"),
					Params:   paramsJSON,
					Href:     strings.TrimSpace(in.Href),
				})
				continue
			}
			destination, err := json.Marshal(r)
			if err != nil {
				return nil, err
			}
			deliveries = append(deliveries, domain.Delivery{
				TenantID:    n.TenantID,
				Channel:     ch,
				Destination: destination,
				MaxAttempts: 6,
			})
		}
	}

	created, err := s.repo.CreateNotification(ctx, n, deliveries, inboxItems)
	if err != nil {
		return nil, err
	}

	// Best-effort Web Push for in-app recipients (Chrome OS banner even when tab closed).
	go s.dispatchWebPush(context.WithoutCancel(ctx), in, inboxItems)

	return created, nil
}

func (s *NotificationService) dispatchWebPush(ctx context.Context, in AcceptInput, inboxItems []domain.InboxItem) {
	if s.pushSender == nil || !s.pushSender.Enabled() || len(inboxItems) == 0 {
		return
	}
	for _, item := range inboxItems {
		subs, err := s.repo.ListPushSubscriptions(ctx, item.TenantID, item.UserID)
		if err != nil {
			slog.Warn("list push subscriptions failed", "userId", item.UserID, "err", err)
			continue
		}
		title := renderPushText(item.TitleKey, in.Params)
		body := renderPushText(item.BodyKey, in.Params)
		for _, sub := range subs {
			err := s.pushSender.Send(ctx, push.Subscription{
				Endpoint: sub.Endpoint,
				P256dh:   sub.P256dh,
				Auth:     sub.Auth,
			}, push.Payload{
				Title: title,
				Body:  body,
				Href:  item.Href,
				Tag:   item.PublicID,
			})
			if err != nil {
				slog.Warn("web push send failed", "userId", item.UserID, "err", err)
				_ = s.repo.DeletePushSubscriptionByEndpoint(ctx, sub.Endpoint)
			}
		}
	}
}

func renderPushText(key string, params map[string]any) string {
	key = strings.TrimSpace(key)
	caseCode, _ := params["caseCode"].(string)
	comment, _ := params["comment"].(string)
	switch {
	case strings.Contains(key, "request_changes.title"):
		return "Hồ sơ cần chỉnh sửa"
	case strings.Contains(key, "request_changes.body"):
		if caseCode != "" && comment != "" {
			return fmt.Sprintf("%s: %s", caseCode, comment)
		}
		if comment != "" {
			return comment
		}
		return "Vui lòng bổ sung hồ sơ"
	case strings.Contains(key, "rejected.title"):
		return "Đăng ký khách hàng bị từ chối"
	case strings.Contains(key, "rejected.body"):
		if caseCode != "" && comment != "" {
			return fmt.Sprintf("%s: %s", caseCode, comment)
		}
		if comment != "" {
			return comment
		}
		return "Hồ sơ đã bị từ chối"
	case strings.Contains(key, "approved.title"):
		return "Đăng ký khách hàng đã được duyệt"
	case strings.Contains(key, "approved.body"):
		if caseCode != "" {
			return caseCode + " đã kích hoạt"
		}
		return "Hồ sơ đã được kích hoạt"
	default:
		if comment != "" {
			return comment
		}
		if caseCode != "" {
			return caseCode
		}
		return "Thông báo Arda"
	}
}

func (s *NotificationService) VAPIDPublicKey() string {
	if s.pushSender == nil {
		return ""
	}
	return s.pushSender.PublicKey()
}

func (s *NotificationService) SubscribePush(ctx context.Context, tenantID, userID, userAgent string, in PushSubscribeInput) error {
	tenantID, userID = strings.TrimSpace(tenantID), strings.TrimSpace(userID)
	endpoint := strings.TrimSpace(in.Endpoint)
	p256dh := strings.TrimSpace(in.Keys.P256dh)
	auth := strings.TrimSpace(in.Keys.Auth)
	if tenantID == "" || userID == "" {
		return errors.New("user context is required")
	}
	if endpoint == "" || p256dh == "" || auth == "" {
		return errors.New("endpoint and keys are required")
	}
	return s.repo.UpsertPushSubscription(ctx, repository.PushSubscription{
		TenantID:  tenantID,
		UserID:    userID,
		Endpoint:  endpoint,
		P256dh:    p256dh,
		Auth:      auth,
		UserAgent: strings.TrimSpace(userAgent),
	})
}

func (s *NotificationService) UnsubscribePush(ctx context.Context, tenantID, userID, endpoint string) error {
	tenantID, userID, endpoint = strings.TrimSpace(tenantID), strings.TrimSpace(userID), strings.TrimSpace(endpoint)
	if tenantID == "" || userID == "" || endpoint == "" {
		return errors.New("user context and endpoint are required")
	}
	return s.repo.DeletePushSubscription(ctx, tenantID, userID, endpoint)
}

func (s *NotificationService) GetByPublicID(ctx context.Context, publicID string) (*domain.Notification, error) {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		return nil, errors.New("notification id is required")
	}
	return s.repo.GetNotificationByPublicID(ctx, publicID)
}

func (s *NotificationService) ListInbox(ctx context.Context, tenantID, userID string, limit int) ([]domain.InboxItem, error) {
	tenantID, userID = strings.TrimSpace(tenantID), strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return nil, errors.New("user context is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.ListInbox(ctx, tenantID, userID, limit)
}

func (s *NotificationService) UnreadCount(ctx context.Context, tenantID, userID string) (int, error) {
	tenantID, userID = strings.TrimSpace(tenantID), strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return 0, errors.New("user context is required")
	}
	return s.repo.UnreadCount(ctx, tenantID, userID)
}

func (s *NotificationService) MarkRead(ctx context.Context, tenantID, userID, publicID string) error {
	tenantID, userID, publicID = strings.TrimSpace(tenantID), strings.TrimSpace(userID), strings.TrimSpace(publicID)
	if tenantID == "" || userID == "" || publicID == "" {
		return errors.New("user context and notification id are required")
	}
	return s.repo.MarkInboxRead(ctx, tenantID, userID, publicID)
}

func (s *NotificationService) MarkAllRead(ctx context.Context, tenantID, userID string) error {
	tenantID, userID = strings.TrimSpace(tenantID), strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		return errors.New("user context is required")
	}
	return s.repo.MarkAllInboxRead(ctx, tenantID, userID)
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
	hasInApp := false
	for _, ch := range in.Channels {
		switch strings.TrimSpace(ch) {
		case domain.ChannelEmail, domain.ChannelWebhook, domain.ChannelSMS, domain.ChannelPush, domain.ChannelInApp:
			hasInApp = hasInApp || strings.TrimSpace(ch) == domain.ChannelInApp
		default:
			return errors.New("unsupported channel: " + ch)
		}
	}
	hasInAppRecipient := false
	for _, r := range in.Recipients {
		if strings.TrimSpace(r.Type) == "" {
			return errors.New("recipient type is required")
		}
		if strings.TrimSpace(r.Address) == "" && strings.TrimSpace(r.UserID) == "" {
			return errors.New("recipient address or user_id is required")
		}
		hasInAppRecipient = hasInAppRecipient || strings.TrimSpace(r.UserID) != ""
	}
	if hasInApp && !hasInAppRecipient {
		return errors.New("in_app channel requires at least one recipient user_id")
	}
	return nil
}

func notificationType(value string) string {
	switch strings.TrimSpace(value) {
	case "warning", "success", "error":
		return strings.TrimSpace(value)
	default:
		return "info"
	}
}

func notificationKey(value, templateKey, suffix string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "notifications:" + strings.TrimSpace(templateKey) + "." + suffix
}

func newPublicID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "ntf_" + hex.EncodeToString(b[:])
}

func newInboxPublicID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "nib_" + hex.EncodeToString(b[:])
}
