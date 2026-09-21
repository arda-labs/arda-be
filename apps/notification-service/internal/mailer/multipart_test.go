package mailer

import (
	"strings"
	"testing"
)

func TestComposeMessageMultipartWhenHTMLPresent(t *testing.T) {
	data, from, to, err := composeMessage(
		Config{FromAddress: "ops@arda.io.vn", FromName: "Arda Ops"},
		Message{
			To:      "user@example.com",
			Subject: "Hồ sơ bị từ chối",
			Body:    "Hồ sơ X bị từ chối",
			HTML:    "<p>Hồ sơ <b>X</b> bị từ chối</p>",
		},
	)
	if err != nil {
		t.Fatalf("composeMessage: %v", err)
	}
	if from != "ops@arda.io.vn" || to != "user@example.com" {
		t.Fatalf("unexpected envelope from=%q to=%q", from, to)
	}
	msg := string(data)
	if !strings.Contains(msg, `multipart/alternative; boundary="arda-`) {
		t.Fatalf("expected multipart/alternative with boundary:\n%s", msg)
	}
	if !strings.Contains(msg, "Content-Type: text/plain; charset=UTF-8") {
		t.Fatal("missing text/plain part")
	}
	if !strings.Contains(msg, "Content-Type: text/html; charset=UTF-8") {
		t.Fatal("missing text/html part")
	}
	if !strings.Contains(msg, "Hồ sơ X bị từ chối") || !strings.Contains(msg, "<b>X</b>") {
		t.Fatal("missing rendered parts")
	}
}

func TestComposeMessagePlainWhenNoHTML(t *testing.T) {
	data, _, _, err := composeMessage(
		Config{FromAddress: "ops@arda.io.vn"},
		Message{To: "user@example.com", Subject: "Hi", Body: "plain"},
	)
	if err != nil {
		t.Fatalf("composeMessage: %v", err)
	}
	msg := string(data)
	if strings.Contains(msg, "multipart/alternative") {
		t.Fatalf("plain message must not be multipart:\n%s", msg)
	}
	if !strings.Contains(msg, "Content-Type: text/plain; charset=UTF-8") {
		t.Fatal("expected text/plain content type")
	}
}
