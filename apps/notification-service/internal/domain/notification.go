package domain

import "time"

const (
	ChannelEmail   = "email"
	ChannelWebhook = "webhook"
	ChannelSMS     = "sms"
	ChannelPush    = "push"
	ChannelInApp   = "in_app"

	NotificationStatusAccepted = "accepted"

	DeliveryStatusQueued      = "queued"
	DeliveryStatusDispatching = "dispatching"
	DeliveryStatusRetrying    = "retrying"
	DeliveryStatusSent        = "sent"
	DeliveryStatusFailed      = "failed"
	DeliveryStatusDeadLetter  = "dead_lettered"
)

type Recipient struct {
	Type    string `json:"type"`
	Address string `json:"address,omitempty"`
	UserID  string `json:"user_id,omitempty"`
}

type Notification struct {
	ID              string
	PublicID        string
	TenantID        string
	SourceService   string
	SourceEventID   string
	EventType       string
	Recipients      []byte
	Channels        []byte
	TemplateKey     string
	TemplateVersion int
	Payload         []byte
	Status          string
	IdempotencyKey  string
	CorrelationID   string
	Priority        int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Delivery struct {
	ID               string
	NotificationID   string
	TenantID         string
	Channel          string
	Destination      []byte
	Provider         string
	Status           string
	AttemptCount     int
	MaxAttempts      int
	NextAttemptAt    time.Time
	LastErrorCode    string
	LastErrorMessage string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type InboxItem struct {
	ID        string
	PublicID  string
	TenantID  string
	UserID    string
	Type      string
	TitleKey  string
	BodyKey   string
	Params    []byte
	Href      string
	ReadAt    *time.Time
	CreatedAt time.Time
}

type OutboxEvent struct {
	ID        string
	Subject   string
	Payload   []byte
	Attempts  int
	CreatedAt time.Time
}
