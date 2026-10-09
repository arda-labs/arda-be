package handler

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (r *flushRecorder) Flush() { r.flushes++ }

func TestParseLastEventID(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		present bool
		wantErr bool
	}{
		{input: "", present: false},
		{input: "42", want: 42, present: true},
		{input: "0", want: 0, present: true},
		{input: "-1", wantErr: true},
		{input: "not-a-sequence", wantErr: true},
	}
	for _, tt := range tests {
		got, present, err := parseLastEventID(tt.input)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseLastEventID(%q) err = %v", tt.input, err)
		}
		if err == nil && (got != tt.want || present != tt.present) {
			t.Fatalf("parseLastEventID(%q) = (%d,%t), want (%d,%t)", tt.input, got, present, tt.want, tt.present)
		}
	}
}

func TestStreamHubLimitsPerUserAndPublishesSequence(t *testing.T) {
	hub := NewStreamHub()
	var closeFirst func()
	for i := 0; i < maxStreamsPerUser; i++ {
		ch, closeStream, err := hub.Subscribe("tenant", "user")
		if err != nil {
			t.Fatalf("Subscribe %d: %v", i, err)
		}
		if i == 0 {
			closeFirst = closeStream
		}
		_ = ch
	}
	if _, _, err := hub.Subscribe("tenant", "user"); !errors.Is(err, ErrStreamLimit) {
		t.Fatalf("overflow err = %v, want ErrStreamLimit", err)
	}
	closeFirst()
	if _, closeStream, err := hub.Subscribe("tenant", "user"); err != nil {
		t.Fatalf("Subscribe after close: %v", err)
	} else {
		closeStream()
	}

	ch, closeStream, err := hub.Subscribe("tenant-2", "user-2")
	if err != nil {
		t.Fatal(err)
	}
	defer closeStream()
	hub.PublishOutboxEvent([]byte(`{"tenant_id":"tenant-2","payload":{"user_id":"user-2","inbox_id":"inbox-1","event_seq":17}}`))
	signal := <-ch
	if signal.EventSeq != 17 || signal.InboxID != "inbox-1" {
		t.Fatalf("signal = %+v", signal)
	}
}

func TestWriteSSEIncludesSequenceAndFlushes(t *testing.T) {
	recorder := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	if !writeSSE(recorder, recorder, 42, "inbox_changed", map[string]string{"id": "n-1"}) {
		t.Fatal("writeSSE returned false")
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "id: 42\nevent: inbox_changed\ndata: {\"id\":\"n-1\"}\n\n") {
		t.Fatalf("unexpected SSE frame: %q", body)
	}
	if recorder.flushes != 1 {
		t.Fatalf("flushes = %d, want 1", recorder.flushes)
	}
}

func TestStreamHubPublishesResolvedSignal(t *testing.T) {
	hub := NewStreamHub()
	ch, closeStream, err := hub.Subscribe("tenant", "user")
	if err != nil {
		t.Fatal(err)
	}
	defer closeStream()
	hub.PublishUserChange([]byte(`{"tenant_id":"tenant","user_id":"user","type":"resolved","event_id":"evt-1","entity_type":"task","entity_id":"task-1"}`))
	signal := <-ch
	if signal.Type != "resolved" || signal.EventID != "evt-1" || signal.EntityType != "task" || signal.EntityID != "task-1" {
		t.Fatalf("signal = %+v", signal)
	}
}

func TestCatchupUsesExclusiveCursorWithoutDuplicates(t *testing.T) {
	items := []domain.InboxItem{
		{PublicID: "n-11", EventSeq: 11, CreatedAt: time.Unix(11, 0)},
		{PublicID: "n-12", EventSeq: 12, CreatedAt: time.Unix(12, 0)},
	}
	recorder := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	cursor, ok := writeCatchupItems(recorder, recorder, items, 10)
	if !ok || cursor != 12 {
		t.Fatalf("first catchup = (%d,%t), want (12,true)", cursor, ok)
	}
	if got := recorder.Body.String(); strings.Count(got, "id: 11\n") != 1 || strings.Count(got, "id: 12\n") != 1 {
		t.Fatalf("first catchup frames have missing or duplicate sequence IDs: %q", got)
	}

	reconnect := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	items = []domain.InboxItem{{PublicID: "n-12", EventSeq: 12, CreatedAt: time.Unix(12, 0)}}
	cursor, ok = writeCatchupItems(reconnect, reconnect, items, 11)
	if !ok || cursor != 12 || strings.Count(reconnect.Body.String(), "id: 12\n") != 1 || strings.Contains(reconnect.Body.String(), "id: 11\n") {
		t.Fatalf("reconnect catchup = (%d,%t,%q), want only sequence 12", cursor, ok, reconnect.Body.String())
	}
}
