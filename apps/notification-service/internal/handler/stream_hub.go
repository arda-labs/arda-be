package handler

import (
	"encoding/json"
	"strings"
	"sync"
)

// StreamHub fans notification outbox events to SSE connections on this service
// replica. Events are signals; clients reconcile the durable inbox over HTTP.
type StreamHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan string]struct{}
}

const UserInboxChangedSubject = "realtime.notification.user.inbox.changed"

func NewStreamHub() *StreamHub {
	return &StreamHub{subscribers: make(map[string]map[chan string]struct{})}
}

func (h *StreamHub) Subscribe(tenantID, userID string) (<-chan string, func()) {
	key := streamKey(tenantID, userID)
	ch := make(chan string, 1)
	h.mu.Lock()
	if h.subscribers[key] == nil {
		h.subscribers[key] = make(map[chan string]struct{})
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
	}
}

func (h *StreamHub) PublishOutboxEvent(data []byte) {
	var event struct {
		TenantID string `json:"tenant_id"`
		Payload  struct {
			InboxID string `json:"inbox_id"`
			UserID  string `json:"user_id"`
		} `json:"payload"`
	}
	if json.Unmarshal(data, &event) != nil || strings.TrimSpace(event.TenantID) == "" ||
		strings.TrimSpace(event.Payload.UserID) == "" || strings.TrimSpace(event.Payload.InboxID) == "" {
		return
	}
	h.publish(event.TenantID, event.Payload.UserID, event.Payload.InboxID)
}

func (h *StreamHub) PublishUserChange(data []byte) {
	var event struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
	}
	if json.Unmarshal(data, &event) != nil || strings.TrimSpace(event.TenantID) == "" || strings.TrimSpace(event.UserID) == "" {
		return
	}
	h.publish(event.TenantID, event.UserID, "state")
}

func (h *StreamHub) publish(tenantID, userID, eventID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers[streamKey(tenantID, userID)] {
		// Coalesce bursts. The event is only a wake-up signal; the inbox API
		// supplies the authoritative list and count.
		select {
		case ch <- eventID:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- eventID:
			default:
			}
		}
	}
}

func streamKey(tenantID, userID string) string { return tenantID + "\x00" + userID }
