package push

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

type Sender struct {
	vapidPublic  string
	vapidPrivate string
	subject      string
}

func NewSender(publicKey, privateKey, subject string) *Sender {
	publicKey = strings.TrimSpace(publicKey)
	privateKey = strings.TrimSpace(privateKey)
	subject = strings.TrimSpace(subject)
	if publicKey == "" || privateKey == "" {
		return nil
	}
	if subject == "" {
		subject = "mailto:ops@arda.io.vn"
	}
	return &Sender{
		vapidPublic:  publicKey,
		vapidPrivate: privateKey,
		subject:      subject,
	}
}

func (s *Sender) Enabled() bool {
	return s != nil && s.vapidPublic != "" && s.vapidPrivate != ""
}

func (s *Sender) PublicKey() string {
	if s == nil {
		return ""
	}
	return s.vapidPublic
}

type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Href  string `json:"href,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

func (s *Sender) Send(ctx context.Context, sub Subscription, payload Payload) error {
	if !s.Enabled() {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}, &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.vapidPublic,
		VAPIDPrivateKey: s.vapidPrivate,
		TTL:             60,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		slog.Warn("web push delivery rejected",
			"status", resp.StatusCode,
			"endpoint", truncate(sub.Endpoint, 80),
		)
	}
	return nil
}

func truncate(v string, n int) string {
	if len(v) <= n {
		return v
	}
	return v[:n] + "…"
}
