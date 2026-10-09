package handler

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// StreamHub fans notification outbox events to SSE connections on this service
// replica. Events are signals; clients reconcile the durable inbox over HTTP.
type StreamHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan StreamSignal]struct{}
}

const (
	maxStreamsPerUser = 3
	streamLeaseTTL    = 45 * time.Second
)

var ErrStreamLimit = errors.New("notification stream limit reached")

type StreamSignal struct {
	Type       string `json:"type"`
	EventSeq   int64  `json:"event_seq,omitempty"`
	InboxID    string `json:"inbox_id,omitempty"`
	EventID    string `json:"event_id,omitempty"`
	EntityType string `json:"entity_type,omitempty"`
	EntityID   string `json:"entity_id,omitempty"`
}

const UserInboxChangedSubject = "realtime.notification.user.inbox.changed"

func NewStreamHub() *StreamHub {
	return &StreamHub{subscribers: make(map[string]map[chan StreamSignal]struct{})}
}

func (h *StreamHub) Subscribe(tenantID, userID string) (<-chan StreamSignal, func(), error) {
	key := streamKey(tenantID, userID)
	ch := make(chan StreamSignal, 1)
	h.mu.Lock()
	if h.subscribers[key] == nil {
		h.subscribers[key] = make(map[chan StreamSignal]struct{})
	}
	if len(h.subscribers[key]) >= maxStreamsPerUser {
		h.mu.Unlock()
		return nil, nil, ErrStreamLimit
	}
	h.subscribers[key][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers[key], ch)
			if len(h.subscribers[key]) == 0 {
				delete(h.subscribers, key)
			}
			close(ch)
			h.mu.Unlock()
		})
	}, nil
}

func (h *StreamHub) PublishOutboxEvent(data []byte) {
	var event struct {
		TenantID string `json:"tenant_id"`
		Payload  struct {
			InboxID  string `json:"inbox_id"`
			UserID   string `json:"user_id"`
			EventSeq int64  `json:"event_seq"`
		} `json:"payload"`
	}
	if json.Unmarshal(data, &event) != nil || strings.TrimSpace(event.TenantID) == "" ||
		strings.TrimSpace(event.Payload.UserID) == "" || strings.TrimSpace(event.Payload.InboxID) == "" {
		return
	}
	h.publish(event.TenantID, event.Payload.UserID, StreamSignal{Type: "inbox_changed", EventSeq: event.Payload.EventSeq, InboxID: event.Payload.InboxID})
}

func (h *StreamHub) PublishUserChange(data []byte) {
	var event struct {
		TenantID   string `json:"tenant_id"`
		UserID     string `json:"user_id"`
		Type       string `json:"type"`
		EventID    string `json:"event_id"`
		EventSeq   int64  `json:"event_seq"`
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
	}
	if json.Unmarshal(data, &event) != nil || strings.TrimSpace(event.TenantID) == "" || strings.TrimSpace(event.UserID) == "" {
		return
	}
	typeName := strings.TrimSpace(event.Type)
	if typeName == "" {
		typeName = "state_changed"
	}
	h.publish(event.TenantID, event.UserID, StreamSignal{
		Type: typeName, EventID: event.EventID, EventSeq: event.EventSeq,
		EntityType: event.EntityType, EntityID: event.EntityID,
	})
}

func (h *StreamHub) publish(tenantID, userID string, event StreamSignal) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers[streamKey(tenantID, userID)] {
		// Coalesce bursts. The event is only a wake-up signal; the inbox API
		// supplies the authoritative list and count.
		select {
		case ch <- event:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- event:
			default:
			}
		}
	}
}

func streamKey(tenantID, userID string) string { return tenantID + "\x00" + userID }
