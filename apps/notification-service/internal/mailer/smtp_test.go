package mailer

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
)

func testConfig() Config {
	return Config{
		Host:        "smtp.example.com",
		Port:        587,
		FromAddress: "ops@arda.io.vn",
		FromName:    "Arda Ops",
	}
}

func TestComposeMessageStripsSubjectHeaderInjection(t *testing.T) {
	data, _, to, err := composeMessage(testConfig(), Message{
		To:      "user@example.com",
		Subject: "Hello\r\nBcc: evil@example.com",
		Body:    "Xin chào",
	})
	if err != nil {
		t.Fatalf("composeMessage: %v", err)
	}
	message := string(data)

	if strings.Contains(message, "\r\nBcc:") {
		t.Fatalf("subject CRLF injected a Bcc header:\n%q", message)
	}
	if !strings.Contains(message, "Subject: Hello") {
		t.Fatalf("subject header missing:\n%s", message)
	}
	if got := strings.Count(message, "\r\n\r\n"); got != 1 {
		t.Fatalf("expected exactly one header/body separator, got %d:\n%s", got, message)
	}
	if !strings.HasSuffix(message, "Xin chào") {
		t.Fatalf("body was altered:\n%s", message)
	}
	if to != "user@example.com" {
		t.Fatalf("envelope recipient = %q", to)
	}
}

func TestComposeMessageEncodesVietnameseSubject(t *testing.T) {
	raw := "Hồ sơ đã được duyệt"
	data, _, _, err := composeMessage(testConfig(), Message{
		To:      "user@example.com",
		Subject: raw,
		Body:    "Nội dung",
	})
	if err != nil {
		t.Fatalf("composeMessage: %v", err)
	}
	message := string(data)

	if !strings.Contains(message, "Subject: =?utf-8?q?") {
		t.Fatalf("subject was not RFC 2047 encoded:\n%s", message)
	}
	if strings.Contains(message, raw) {
		t.Fatalf("subject header still contains raw non-ASCII bytes:\n%s", message)
	}
}

func TestComposeMessageRejectsAddressHeaderInjection(t *testing.T) {
	_, _, _, err := composeMessage(testConfig(), Message{
		To:      "user@example.com\r\nBcc: evil@example.com",
		Subject: "hello",
	})
	if err == nil {
		t.Fatal("expected CRLF in recipient to be rejected")
	}

	cfg := testConfig()
	cfg.FromAddress = "ops@arda.io.vn\r\nBcc: evil@example.com"
	if _, _, _, err := composeMessage(cfg, Message{To: "user@example.com", Subject: "hello"}); err == nil {
		t.Fatal("expected CRLF in sender to be rejected")
	}
}

func TestComposeMessageSanitizesFromNameAndTruncatesSubject(t *testing.T) {
	cfg := testConfig()
	cfg.FromName = "Ops\r\nX-Injected: yes"
	long := strings.Repeat("a", 1000)
	data, _, _, err := composeMessage(cfg, Message{To: "user@example.com", Subject: long})
	if err != nil {
		t.Fatalf("composeMessage: %v", err)
	}
	message := string(data)
	if strings.Contains(message, "\r\nX-Injected:") {
		t.Fatalf("display name CRLF injected a header:\n%q", message)
	}
	for _, line := range strings.Split(message, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("header line exceeds RFC 5322 limit (%d bytes): %q", len(line), line)
		}
	}
}

// TestSendRequiresStartTLS drives a plaintext-only SMTP server: even with
// UseTLS=false the mailer must refuse to send, so credentials and bodies can
// never leave unencrypted.
func TestSendRequiresStartTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		_, _ = conn.Write([]byte("220 test.local ESMTP\r\n"))
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				// No STARTTLS in the extension list.
				_, _ = conn.Write([]byte("250-test.local\r\n250 OK\r\n"))
			case strings.HasPrefix(line, "QUIT"):
				_, _ = conn.Write([]byte("221 bye\r\n"))
				return
			default:
				_, _ = conn.Write([]byte("250 OK\r\n"))
			}
		}
	}()

	host, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	err = NewSMTP().Send(context.Background(), Config{
		Host:        host,
		Port:        port,
		Username:    "ops@example.com",
		Password:    "secret",
		FromAddress: "ops@example.com",
	}, Message{To: "user@example.com", Subject: "hello", Body: "body"})
	if !errors.Is(err, ErrStartTLSUnavailable) {
		t.Fatalf("Send over a plaintext-only server = %v, want %v", err, ErrStartTLSUnavailable)
	}
}
