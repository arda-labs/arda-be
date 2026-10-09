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
	ScheduleAt       *time.Time
	LastErrorCode    string
	LastErrorMessage string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type InboxItem struct {
	ID             string
	PublicID       string
	TenantID       string
	UserID         string
	Type           string
	TitleKey       string
	BodyKey        string
	Params         []byte
	Href           string
	ReadAt         *time.Time
	EntityType     string
	EntityID       string
	DedupeKey      string
	ResolvedAt     *time.Time
	ResolvedReason string
	SupersededAt   *time.Time
	ExpiresAt      *time.Time
	Locale         string
	Priority       int
	EventSeq       int64
	CreatedAt      time.Time
}

type NotificationPreference struct {
	EventGroup string  `json:"event_group"`
	Channel    string  `json:"channel"`
	Enabled    bool    `json:"enabled"`
	QuietStart *string `json:"quiet_start,omitempty"`
	QuietEnd   *string `json:"quiet_end,omitempty"`
	Timezone   string  `json:"timezone"`
	DigestMode string  `json:"digest_mode"`
	Locale     string  `json:"locale"`
}

type OutboxEvent struct {
	ID        string
	Subject   string
	Payload   []byte
	Attempts  int
	CreatedAt time.Time
}
