package mail

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// sendFunc matches [smtp.SendMail]'s signature. Factoring it out lets tests
// substitute a fake instead of dialling a real relay.
type sendFunc func(addr string, a smtp.Auth, from string, to []string, msg []byte) error

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
		send:     smtp.SendMail,
	}
}

// Send delivers a single plain-text email through the configured relay.
//
// net/smtp has no context support, so ctx cannot cancel or time out the
// dial; it is accepted only to satisfy [Sender].
func (s *SMTPSender) Send(_ context.Context, to, subject, body string) error {
	addr := net.JoinHostPort(s.host, s.port)
	auth := smtp.PlainAuth("", s.username, s.password, s.host)

	if err := s.send(addr, auth, s.from, []string{to}, buildMessage(s.from, to, subject, body)); err != nil {
		return fmt.Errorf("send mail via %s: %w", addr, err)
	}
	return nil
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
