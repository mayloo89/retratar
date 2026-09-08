package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// defaultSendTimeout bounds a send when ctx carries no deadline of its own.
// A magic link is only useful for the next few minutes anyway; a relay that
// hasn't accepted the message by then is not going to before the link
// expires either.
const defaultSendTimeout = 20 * time.Second

// sendFunc drives one SMTP exchange under a hard deadline. Factoring it out
// lets tests substitute a fake instead of dialling a real relay.
type sendFunc func(ctx context.Context, deadline time.Time, addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error

// SMTPSender delivers mail through an SMTP relay authenticated with
// AUTH PLAIN over STARTTLS — the shape every transactional-email provider
// (Postmark, SES, etc.) speaks, so nothing here is provider-specific.
type SMTPSender struct {
	host, port, username, password, from string
	send                                 sendFunc
}

// NewSMTPSender wires an SMTPSender to a relay. host/port/username/password
// are the provider's SMTP credentials; from is the address mail is sent as.
//
// The recipient address is trusted to already be free of header-injection
// characters: it only ever reaches here after [user.NormaliseEmail], which
// runs every address through [net/mail.ParseAddress] and rejects anything
// that syntax does not accept.
func NewSMTPSender(host, port, username, password, from string) *SMTPSender {
	return &SMTPSender{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		send:     sendMailWithDeadline,
	}
}

// Send delivers a single plain-text email through the configured relay,
// bounded by ctx's deadline if it has one, [defaultSendTimeout] otherwise.
// [net/smtp.SendMail] has no context support at all — it can dial and then
// hang indefinitely on a relay that accepts the connection and then goes
// quiet, which would tie up the request goroutine that called Send for as
// long as the relay stays silent. Driving the exchange by hand instead, over
// a connection with an explicit deadline, is what actually bounds that.
func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	addr := net.JoinHostPort(s.host, s.port)

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultSendTimeout)
	}

	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	msg := buildMessage(s.from, to, subject, body)

	if err := s.send(ctx, deadline, addr, s.host, auth, s.from, []string{to}, msg); err != nil {
		return fmt.Errorf("send mail via %s: %w", addr, err)
	}
	return nil
}

// sendMailWithDeadline is the real [sendFunc]: dial, STARTTLS, authenticate,
// and deliver one message, all under one connection-wide deadline.
//
// This is the same sequence [smtp.SendMail] runs internally — it is not
// reimplemented here for different behaviour, only so a deadline can be
// attached to the connection before any of it starts.
func sendMailWithDeadline(ctx context.Context, deadline time.Time, addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err = conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	if err = client.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, rcpt := range to {
		if err = client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt to %s: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err = w.Write(msg); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("close message: %w", err)
	}

	return client.Quit()
}

// buildMessage assembles a minimal plain-text RFC 5322 message.
func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}
