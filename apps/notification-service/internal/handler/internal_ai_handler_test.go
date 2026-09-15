package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
)

func TestToAIInboxItems_RedactsParams(t *testing.T) {
	readAt := time.Now().Add(-time.Hour)
	items := []domain.InboxItem{{
		PublicID:  "n1",
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Type:      "warning",
		TitleKey:  "notif.title.approval",
		BodyKey:   "notif.body.approval",
		Params:    []byte(`{"requester":"Nguyen Van A","amount":1500000}`),
		Href:      "/approvals/abc",
		ReadAt:    &readAt,
		CreatedAt: time.Now(),
	}}

	raw, err := json.Marshal(toAIInboxItems(items))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	allowed := map[string]bool{"id": true, "type": true, "titleKey": true, "bodyKey": true, "href": true, "readAt": true, "createdAt": true}
	for key := range out[0] {
		if !allowed[key] {
			t.Errorf("field %q leaked into the AI shape (allowlist violation)", key)
		}
	}
	if _, ok := out[0]["params"]; ok {
		t.Errorf("params must never be exposed to the AI surface")
	}
	if out[0]["titleKey"] != "notif.title.approval" {
		t.Errorf("unexpected redacted payload: %v", out[0])
	}
}

func TestInternalAIListInbox_RejectsOutOfRangeLimit(t *testing.T) {
	h := &NotificationHandler{} // svc is nil: validation must fire before any store call

	for _, limit := range []string{"0", "21", "-3", "abc"} {
		req := httptest.NewRequest(http.MethodGet, "/internal/ai/notifications?limit="+limit, nil)
		rec := httptest.NewRecorder()
		h.InternalAIListInbox(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit %q: expected 400, got %d", limit, rec.Code)
		}
	}
}

func TestInternalAIHandlers_RejectNonGET(t *testing.T) {
	h := &NotificationHandler{} // svc is nil: method check must fire before any repo call

	rec := httptest.NewRecorder()
	h.InternalAIListInbox(rec, httptest.NewRequest(http.MethodPost, "/internal/ai/notifications", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.InternalAIUnreadCount(rec, httptest.NewRequest(http.MethodPost, "/internal/ai/notifications/unread-count", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rec.Code)
	}
}
