// Package mailer provides outbound email sending (X2). The SMTP
// implementation is intentionally thin so tests can substitute a fake.
package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// Config is one sender configuration (password already decrypted).
type Config struct {
	Host        string
	Port        int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	UseTLS      bool
}

// Message is one outbound email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Mailer sends one message with the given config.
type Mailer interface {
	Send(ctx context.Context, cfg Config, msg Message) error
}

// SMTP sends via net/smtp (STARTTLS when advertised).
type SMTP struct{}

func NewSMTP() *SMTP { return &SMTP{} }

func (s *SMTP) Send(ctx context.Context, cfg Config, msg Message) error {
	if cfg.Host == "" || msg.To == "" {
		return fmt.Errorf("smtp host and recipient are required")
	}
	port := cfg.Port
	if port <= 0 {
		port = 587
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", port))
	from := cfg.FromAddress
	if from == "" {
		from = cfg.Username
	}
	header := strings.Builder{}
	header.WriteString("From: ")
	if cfg.FromName != "" {
		header.WriteString(fmt.Sprintf("%s <%s>", cfg.FromName, from))
	} else {
		header.WriteString(from)
	}
	header.WriteString("\r\nTo: " + msg.To + "\r\n")
	header.WriteString("Subject: " + msg.Subject + "\r\n")
	header.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")

	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	var err error
	if cfg.UseTLS {
		err = sendWithStartTLS(ctx, addr, cfg.Host, auth, from, msg.To, []byte(header.String()+msg.Body))
	} else {
		err = smtp.SendMail(addr, auth, from, []string{msg.To}, []byte(header.String()+msg.Body))
	}
	if err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

// sendWithStartTLS performs an explicit STARTTLS handshake before AUTH so
// plain credentials are never sent over an unencrypted connection.
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
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
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
