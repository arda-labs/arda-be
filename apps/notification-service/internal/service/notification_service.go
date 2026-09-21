package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/netguard"
	"github.com/arda-labs/arda/apps/notification-service/internal/push"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
)

// EmailResolver maps user IDs to email addresses for non-in-app channels. It
// is optional: without it, email deliveries need an explicit recipient address.
type EmailResolver interface {
	ResolveEmails(ctx context.Context, userIDs []string) (map[string]string, error)
}

type NotificationService struct {
	repo          *repository.NotificationRepository
	pushSender    *push.Sender
	emailResolver EmailResolver
}

var (
	ErrTenantScopeRequired     = errors.New("tenant scope is required")
	ErrTenantMigrationRequired = errors.New("tenant migration is required")
	ErrUserContextRequired     = errors.New("authenticated user context is required")
	ErrPushEndpointOwned       = errors.New("push endpoint is already registered to another account")
)

func NewNotificationService(repo *repository.NotificationRepository, pushSender *push.Sender, resolvers ...EmailResolver) *NotificationService {
	svc := &NotificationService{repo: repo, pushSender: pushSender}
	if len(resolvers) > 0 {
		svc.emailResolver = resolvers[0]
	}
	return svc
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
	// Resolve user → email once for every email recipient without an explicit
	// address, so the delivery row carries a usable destination.
	resolvedEmails := map[string]string{}
	if resolvesEmail(in.Channels) {
		ids := emailRecipientIDs(in.Channels, in.Recipients)
		if len(ids) > 0 && s.emailResolver != nil {
			if m, err := s.emailResolver.ResolveEmails(ctx, ids); err != nil {
				slog.Warn("email recipient resolution failed", "err", err)
			} else {
				resolvedEmails = m
			}
		}
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
			dest := r
			if strings.EqualFold(ch, domain.ChannelEmail) {
				if strings.TrimSpace(dest.Address) == "" && strings.TrimSpace(dest.UserID) != "" {
					dest.Address = resolvedEmails[strings.TrimSpace(dest.UserID)]
				}
				if strings.TrimSpace(dest.Address) == "" {
					slog.Warn("skip email delivery: recipient has no address", "userId", dest.UserID)
					continue
				}
			}
			destination, err := json.Marshal(dest)
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

func resolvesEmail(channels []string) bool {
	for _, ch := range channels {
		if strings.EqualFold(strings.TrimSpace(ch), domain.ChannelEmail) {
			return true
		}
	}
	return false
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// plainTextFromHTML derives a rough text/plain alternative from an HTML body so
// multipart mail always has both parts. Not a full HTML parser.
func plainTextFromHTML(htmlBody string) string {
	text := htmlTagRe.ReplaceAllString(htmlBody, " ")
	replacer := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`)
	text = replacer.Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

// emailRecipientIDs returns the deduplicated user IDs that still need an email
// address (email channel requested, no explicit address on the recipient).
func emailRecipientIDs(channels []string, recipients []domain.Recipient) []string {
	if !resolvesEmail(channels) {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(recipients))
	for _, r := range recipients {
		if strings.TrimSpace(r.Address) != "" {
			continue
		}
		id := strings.TrimSpace(r.UserID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
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
				_ = s.repo.DeletePushSubscriptionByEndpoint(ctx, item.TenantID, item.UserID, sub.Endpoint)
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
	if err := validateUserContext(tenantID, userID); err != nil {
		return errors.New("user context is required")
	}
	if endpoint == "" || p256dh == "" || auth == "" {
		return errors.New("endpoint and keys are required")
	}
	if err := push.ValidateEndpoint(endpoint); err != nil {
		return fmt.Errorf("invalid push endpoint: %w", err)
	}
	err := s.repo.UpsertPushSubscription(ctx, repository.PushSubscription{
		TenantID:  tenantID,
		UserID:    userID,
		Endpoint:  endpoint,
		P256dh:    p256dh,
		Auth:      auth,
		UserAgent: strings.TrimSpace(userAgent),
	})
	if errors.Is(err, repository.ErrPushSubscriptionOwnedByAnotherUser) {
		return ErrPushEndpointOwned
	}
	return err
}

func (s *NotificationService) UnsubscribePush(ctx context.Context, tenantID, userID, endpoint string) error {
	tenantID, userID, endpoint = strings.TrimSpace(tenantID), strings.TrimSpace(userID), strings.TrimSpace(endpoint)
	if err := validateUserContext(tenantID, userID); err != nil || endpoint == "" {
		return errors.New("user context and endpoint are required")
	}
	return s.repo.DeletePushSubscription(ctx, tenantID, userID, endpoint)
}

func (s *NotificationService) GetByPublicID(ctx context.Context, tenantID, publicID string) (*domain.Notification, error) {
	tenantID = strings.TrimSpace(tenantID)
	publicID = strings.TrimSpace(publicID)
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if publicID == "" {
		return nil, errors.New("notification id is required")
	}
	return s.repo.GetNotificationByPublicID(ctx, tenantID, publicID)
}

func (s *NotificationService) ListInbox(ctx context.Context, tenantID, userID string, limit int) ([]domain.InboxItem, error) {
	tenantID, userID = strings.TrimSpace(tenantID), strings.TrimSpace(userID)
	if err := validateUserContext(tenantID, userID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.ListInbox(ctx, tenantID, userID, limit)
}

func (s *NotificationService) UnreadCount(ctx context.Context, tenantID, userID string) (int, error) {
	tenantID, userID = strings.TrimSpace(tenantID), strings.TrimSpace(userID)
	if err := validateUserContext(tenantID, userID); err != nil {
		return 0, err
	}
	return s.repo.UnreadCount(ctx, tenantID, userID)
}

func (s *NotificationService) MarkRead(ctx context.Context, tenantID, userID, publicID string) error {
	tenantID, userID, publicID = strings.TrimSpace(tenantID), strings.TrimSpace(userID), strings.TrimSpace(publicID)
	if err := validateUserContext(tenantID, userID); err != nil || publicID == "" {
		return errors.New("user context and notification id are required")
	}
	return s.repo.MarkInboxRead(ctx, tenantID, userID, publicID)
}

func (s *NotificationService) MarkAllRead(ctx context.Context, tenantID, userID string) error {
	tenantID, userID = strings.TrimSpace(tenantID), strings.TrimSpace(userID)
	if err := validateUserContext(tenantID, userID); err != nil {
		return errors.New("user context is required")
	}
	return s.repo.MarkAllInboxRead(ctx, tenantID, userID)
}

func validateAccept(in AcceptInput) error {
	if err := validateTenantID(in.TenantID); err != nil {
		return err
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

func validateTenantID(tenantID string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ErrTenantScopeRequired
	}
	if strings.EqualFold(tenantID, "default") {
		return ErrTenantMigrationRequired
	}
	return nil
}

func validateUserContext(tenantID, userID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return ErrUserContextRequired
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

// ListTemplates returns the notification template catalog (X2).
func (s *NotificationService) ListTemplates(ctx context.Context, tenantID string) ([]repository.NotificationTemplate, error) {
	if tenantID == "" {
		return nil, ErrTenantScopeRequired
	}
	return s.repo.ListTemplates(ctx, tenantID)
}

// UpsertTemplate creates or updates one template.
func (s *NotificationService) UpsertTemplate(ctx context.Context, tenantID, actor string, in *repository.NotificationTemplate) (*repository.NotificationTemplate, error) {
	if tenantID == "" {
		return nil, ErrTenantScopeRequired
	}
	if in.EventCode == "" || in.Channel == "" || (in.Body == "" && in.BodyHTML == "") {
		return nil, errors.New("event_code, channel and body (or body_html) are required")
	}
	// Keep a plain-text alternative when only HTML was provided (multipart mail).
	if in.Body == "" && in.BodyHTML != "" {
		in.Body = plainTextFromHTML(in.BodyHTML)
	}
	in.TenantID = tenantID
	if in.Locale == "" {
		in.Locale = "vi-VN"
	}
	in.CreatedBy = actor
	created, err := s.repo.UpsertTemplate(ctx, in)
	if err != nil {
		return nil, errors.New("could not save the template")
	}
	return created, nil
}

// DeleteTemplate removes one template.
func (s *NotificationService) DeleteTemplate(ctx context.Context, tenantID, id string) error {
	if tenantID == "" {
		return ErrTenantScopeRequired
	}
	if err := s.repo.DeleteTemplate(ctx, tenantID, id); err != nil {
		return errors.New("template not found")
	}
	return nil
}

// ListEmailDesigns returns the reusable email designs of a tenant.
func (s *NotificationService) ListEmailDesigns(ctx context.Context, tenantID string) ([]repository.EmailDesign, error) {
	if tenantID == "" {
		return nil, ErrTenantScopeRequired
	}
	return s.repo.ListEmailDesigns(ctx, tenantID)
}

// UpsertEmailDesign creates or updates a reusable email design.
func (s *NotificationService) UpsertEmailDesign(ctx context.Context, tenantID, actor string, in *repository.EmailDesign) (*repository.EmailDesign, error) {
	if tenantID == "" {
		return nil, ErrTenantScopeRequired
	}
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.BodyHTML) == "" {
		return nil, errors.New("code and body_html are required")
	}
	in.TenantID = tenantID
	in.CreatedBy = actor
	created, err := s.repo.UpsertEmailDesign(ctx, in)
	if err != nil {
		return nil, errors.New("could not save the email design")
	}
	return created, nil
}

// DeleteEmailDesign removes one email design.
func (s *NotificationService) DeleteEmailDesign(ctx context.Context, tenantID, id string) error {
	if tenantID == "" {
		return ErrTenantScopeRequired
	}
	if err := s.repo.DeleteEmailDesign(ctx, tenantID, id); err != nil {
		return errors.New("email design not found")
	}
	return nil
}

// ListSenders returns the sender configs (password masked).
func (s *NotificationService) ListSenders(ctx context.Context, tenantID string) ([]repository.SenderConfig, error) {
	if tenantID == "" {
		return nil, ErrTenantScopeRequired
	}
	return s.repo.ListSenders(ctx, tenantID)
}

// UpsertSender creates or updates one sender config.
func (s *NotificationService) UpsertSender(ctx context.Context, tenantID, actor string, in *repository.SenderConfig) (*repository.SenderConfig, error) {
	if tenantID == "" {
		return nil, ErrTenantScopeRequired
	}
	in.Host = strings.TrimSpace(in.Host)
	if in.Host == "" || in.FromAddress == "" {
		return nil, errors.New("host and from_address are required")
	}
	// A tenant-supplied SMTP host is dialled by the delivery worker, so it must
	// be a public destination (no loopback, private, link-local, CGNAT or
	// intranet/metadata host).
	if err := netguard.ValidateHost(in.Host); err != nil {
		return nil, fmt.Errorf("sender host is not allowed: %w", err)
	}
	in.TenantID = tenantID
	if in.Channel == "" {
		in.Channel = "email"
	}
	// SMTP delivery always requires STARTTLS (see internal/mailer); persist the
	// flag as enabled so the stored config matches the enforced behaviour.
	in.UseTLS = true
	created, err := s.repo.UpsertSender(ctx, in)
	if err != nil {
		return nil, errors.New("could not save the sender config")
	}
	return created, nil
}
