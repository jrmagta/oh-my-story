// Package mail sends transactional email.
package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type Message struct {
	To      string
	Subject string
	Body    string // plain text
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTP sends through a relay. With Username set it uses PLAIN auth, which
// net/smtp only allows over TLS or to localhost.
type SMTP struct {
	Addr, Username, Password, From string
	Timeout                        time.Duration // whole conversation; zero means 30s
}

func (s SMTP) Send(ctx context.Context, m Message) error {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("mail to: %w", err)
	}
	msg := strings.Join([]string{
		"From: " + from.String(),
		"To: " + to.String(),
		"Subject: " + mime.QEncoding.Encode("utf-8", m.Subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		strings.ReplaceAll(m.Body, "\n", "\r\n"),
	}, "\r\n")

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	// A relay that stalls mid-conversation fails the read or write at the deadline.
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	defer context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })()

	host, _, _ := net.SplitHostPort(s.Addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	// The same steps as smtp.SendMail, which has no way to set a deadline.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(to.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Log writes messages to the log instead of sending them. Used when no SMTP relay is configured.
type Log struct{}

func (Log) Send(_ context.Context, m Message) error {
	slog.Info("email not sent (OMS_SMTP_ADDR unset)", "to", m.To, "subject", m.Subject, "body", m.Body)
	return nil
}
