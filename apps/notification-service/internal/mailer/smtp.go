// Package mailer provides outbound email sending (X2). The SMTP
// implementation is intentionally thin so tests can substitute a fake.
package mailer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
)

// maxSubjectLength bounds the raw subject before RFC 2047 encoding. An
// unbounded subject would decode to a header line beyond the RFC 5322 limit,
// which some MTAs reject or mangle.
const maxSubjectLength = 400

// ErrStartTLSUnavailable is returned when a server does not advertise STARTTLS.
// Delivery fails closed rather than falling back to cleartext, so credentials
// and message bodies are never sent unencrypted.
var ErrStartTLSUnavailable = errors.New("smtp server does not support STARTTLS")

// Config is one sender configuration (password already decrypted).
//
// UseTLS is retained for configuration compatibility only: Send always requires
// STARTTLS, so this flag can no longer enable a cleartext fallback.
type Config struct {
	Host        string
	Port        int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	UseTLS      bool
}

// Message is one outbound email. HTML is optional: when set the message is sent
// as multipart/alternative with Body as the text/plain part.
type Message struct {
	To      string
	Subject string
	Body    string
	HTML    string
}

// Mailer sends one message with the given config.
type Mailer interface {
	Send(ctx context.Context, cfg Config, msg Message) error
}

// SMTP sends via net/smtp (STARTTLS when advertised).
type SMTP struct{}

func NewSMTP() *SMTP { return &SMTP{} }

func (s *SMTP) Send(ctx context.Context, cfg Config, msg Message) error {
	if cfg.Host == "" || strings.TrimSpace(msg.To) == "" {
		return fmt.Errorf("smtp host and recipient are required")
	}
	data, envelopeFrom, envelopeTo, err := composeMessage(cfg, msg)
	if err != nil {
		return err
	}
	port := cfg.Port
	if port <= 0 {
		port = 587
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", port))

	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	// STARTTLS is mandatory: the legacy net/smtp plaintext branch is gone so a
	// public or attacker-controlled SMTP host can never receive cleartext
	// credentials or message bodies.
	if err := sendWithStartTLS(ctx, addr, cfg.Host, auth, envelopeFrom, envelopeTo, data); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

// composeMessage validates the envelope addresses and renders the RFC 5322
// message. Values that reach the header block are either rejected (recipient
// and sender addresses) or neutralised (subject and display name), so a
// template placeholder such as "\r\nBcc: attacker@example.com" can never
// introduce a new header. The body is text/plain, so CRLF inside it stays body
// content and net/smtp dot-stuffs lines that begin with a period.
func composeMessage(cfg Config, msg Message) (data []byte, fromAddress, toAddress string, err error) {
	from := cfg.FromAddress
	if from == "" {
		from = cfg.Username
	}
	fromMailbox, err := parseMailbox(from)
	if err != nil {
		return nil, "", "", fmt.Errorf("smtp from address: %w", err)
	}
	toMailbox, err := parseMailbox(msg.To)
	if err != nil {
		return nil, "", "", fmt.Errorf("smtp to address: %w", err)
	}

	subject := sanitizeHeaderValue(msg.Subject)
	if runes := []rune(subject); len(runes) > maxSubjectLength {
		subject = string(runes[:maxSubjectLength])
	}
	fromHeader := *fromMailbox
	if name := sanitizeHeaderValue(cfg.FromName); name != "" {
		fromHeader.Name = name
	}

	header := strings.Builder{}
	header.WriteString("From: " + fromHeader.String() + "\r\n")
	header.WriteString("To: " + toMailbox.String() + "\r\n")
	header.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	header.WriteString("MIME-Version: 1.0\r\n")

	if strings.TrimSpace(msg.HTML) == "" {
		header.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		return []byte(header.String() + msg.Body), fromMailbox.Address, toMailbox.Address, nil
	}

	boundary, err := newBoundary()
	if err != nil {
		return nil, "", "", err
	}
	var body strings.Builder
	body.WriteString(header.String())
	body.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
	body.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	body.WriteString(msg.Body)
	body.WriteString("\r\n--" + boundary + "\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n")
	body.WriteString(msg.HTML)
	body.WriteString("\r\n--" + boundary + "--\r\n")
	return []byte(body.String()), fromMailbox.Address, toMailbox.Address, nil
}

// newBoundary returns a random MIME boundary for multipart messages.
func newBoundary() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "arda-" + hex.EncodeToString(buf), nil
}

// parseMailbox accepts one RFC 5322 address and returns its parsed form. CR/LF
// is rejected explicitly so a crafted address can never split the header.
func parseMailbox(raw string) (*mail.Address, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("address is required")
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return nil, errors.New("address must not contain CR or LF")
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}
	return addr, nil
}

// sanitizeHeaderValue replaces CR/LF with spaces and drops the remaining
// control characters so the value cannot escape its header line.
func sanitizeHeaderValue(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\r' || r == '\n':
			return ' '
		case r < 0x20 && r != '\t':
			return -1
		default:
			return r
		}
	}, value)
}

// sendWithStartTLS performs an explicit STARTTLS handshake before AUTH so plain
// credentials are never sent over an unencrypted connection. A server that does
// not advertise STARTTLS is rejected with ErrStartTLSUnavailable.
func sendWithStartTLS(ctx context.Context, addr, host string, auth smtp.Auth, from, to string, data []byte) error {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	ok, _ := client.Extension("STARTTLS")
	if !ok {
		return ErrStartTLSUnavailable
	}
	if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
