package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/mailer"
	"github.com/arda-labs/arda/apps/notification-service/internal/netguard"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
	ardacrypto "github.com/arda-labs/arda/libs/go/arda-crypto"
)

// MailTester sends one email synchronously so an operator can verify sender
// config + template without waiting for the outbox. It reuses the tenant's
// active sender and the template for the event code.
type MailTester struct {
	repo   *repository.NotificationRepository
	mailer mailer.Mailer
	secret string
}

func NewMailTester(repo *repository.NotificationRepository, m mailer.Mailer, secret string) *MailTester {
	if repo == nil {
		return nil
	}
	if m == nil {
		m = mailer.NewSMTP()
	}
	return &MailTester{repo: repo, mailer: m, secret: secret}
}

// SendTest renders the template for eventCode and sends it to recipient.
func (t *MailTester) SendTest(ctx context.Context, tenantID, eventCode, locale, recipient string, params map[string]any) error {
	if tenantID == "" {
		return ErrTenantScopeRequired
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return errors.New("recipient is required")
	}
	if strings.TrimSpace(eventCode) == "" {
		return errors.New("event_code is required")
	}
	if locale == "" {
		locale = "vi-VN"
	}

	sender, encrypted, err := t.repo.ActiveSender(ctx, tenantID, domain.ChannelEmail)
	if err != nil {
		return err
	}
	if sender == nil {
		return errors.New("email sender config is not configured")
	}
	if err := netguard.ValidateHost(sender.Host); err != nil {
		return errors.New("email sender host is not allowed: " + err.Error())
	}
	password := ""
	if encrypted != "" {
		decrypted, err := ardacrypto.Decrypt(encrypted, t.secret)
		if err != nil {
			return errors.New("decrypt sender password failed")
		}
		password = decrypted
	}

	subject, text, htmlBody := eventCode, "", ""
	template, err := t.repo.FindTemplate(ctx, tenantID, eventCode, domain.ChannelEmail, locale)
	if err != nil {
		return err
	}
	if template != nil {
		subject = renderTemplate(template.Subject, params)
		if subject == "" {
			subject = eventCode
		}
		text = renderTemplate(template.Body, params)
		htmlBody = renderTemplate(template.BodyHTML, params)
	}

	return t.mailer.Send(ctx, mailer.Config{
		Host:        sender.Host,
		Port:        sender.Port,
		Username:    sender.Username,
		Password:    password,
		FromAddress: sender.FromAddress,
		FromName:    sender.FromName,
		UseTLS:      sender.UseTLS,
	}, mailer.Message{To: recipient, Subject: subject, Body: text, HTML: htmlBody})
}

// renderTemplate substitutes {{key}} placeholders with params values.
func renderTemplate(text string, params map[string]any) string {
	if text == "" || len(params) == 0 {
		return text
	}
	out := text
	for key, value := range params {
		out = strings.ReplaceAll(out, "{{"+key+"}}", stringifyValue(value))
	}
	return out
}

func stringifyValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}
