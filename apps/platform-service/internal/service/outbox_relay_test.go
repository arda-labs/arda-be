package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

func TestOutboxRelayPublishesAndMarks(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"failed"}`)
	_, _ = s.RunNow(context.Background(), "ai-eval", "u1", nil)
	s.Wait()
	var got []string
	relay := NewOutboxRelay(db, func(subject, id string, data []byte) error {
		got = append(got, subject)
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	n, err := relay.PublishOnce(context.Background())
	if err != nil || n != 1 || len(got) != 1 {
		t.Fatalf("n=%d got=%v err=%v", n, got, err)
	}
	if n, _ := relay.PublishOnce(context.Background()); n != 0 {
		t.Fatalf("already published rows must not be re-sent, got %d", n)
	}
}

func TestOutboxRelayRecordsFailure(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"failed"}`)
	_, _ = s.RunNow(context.Background(), "ai-eval", "u1", nil)
	s.Wait()
	relay := NewOutboxRelay(db, func(string, string, []byte) error { return errors.New("nats down") }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, _ = relay.PublishOnce(context.Background())
	var attempts int
	var lastErr string
	_ = db.QueryRow(`SELECT publish_attempts, last_error FROM plt_outbox`).Scan(&attempts, &lastErr)
	if attempts != 1 || lastErr != "nats down" {
		t.Fatalf("attempts=%d lastErr=%q", attempts, lastErr)
	}
}
