package mail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPSenderSendsThroughTheRelay(t *testing.T) {
	t.Parallel()

	var gotAddr, gotHost, gotFrom string
	var gotTo []string
	var gotMsg []byte
	var gotAuth smtp.Auth

	sender := NewSMTPSender("smtp.postmarkapp.com", "587", "smtp-user", "smtp-pass", "noreply@retratar.com.ar")
	sender.send = func(_ context.Context, _ time.Time, addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotHost, gotAuth, gotFrom, gotTo, gotMsg = addr, host, auth, from, to, msg
		return nil
	}

	err := sender.Send(t.Context(), "ana@example.com", "Sign in", "https://retratar.com.ar/login/abc")
	if err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}

	if want := "smtp.postmarkapp.com:587"; gotAddr != want {
		t.Errorf("addr = %q, want %q", gotAddr, want)
	}
	if want := "smtp.postmarkapp.com"; gotHost != want {
		t.Errorf("host = %q, want %q", gotHost, want)
	}
	if want := "noreply@retratar.com.ar"; gotFrom != want {
		t.Errorf("from = %q, want %q", gotFrom, want)
	}
	if len(gotTo) != 1 || gotTo[0] != "ana@example.com" {
		t.Errorf("to = %v, want [ana@example.com]", gotTo)
	}

	// PlainAuth.Start refuses to hand back credentials over a connection it
	// does not believe is encrypted — driving it through the real protocol
	// method, rather than reaching into unexported fields, is what actually
	// proves NewSMTPSender built auth from smtp-user/smtp-pass and not some
	// other pair.
	if gotAuth == nil {
		t.Fatal("auth is nil")
	}
	proto, toServer, err := gotAuth.Start(&smtp.ServerInfo{Name: "smtp.postmarkapp.com", TLS: true})
	if err != nil {
		t.Fatalf("auth.Start() error = %v, want nil", err)
	}
	if proto != "PLAIN" {
		t.Errorf("auth mechanism = %q, want %q", proto, "PLAIN")
	}
	if want := "\x00smtp-user\x00smtp-pass"; string(toServer) != want {
		t.Errorf("auth response = %q, want %q", toServer, want)
	}

	msg := string(gotMsg)
	for _, want := range []string{
		"From: noreply@retratar.com.ar",
		"To: ana@example.com",
		"Subject: Sign in",
		"https://retratar.com.ar/login/abc",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}

// TestSMTPSenderUsesContextDeadline proves a deadline already on ctx wins
// over defaultSendTimeout — the whole point of taking ctx seriously instead
// of discarding it the way [smtp.SendMail] does.
func TestSMTPSenderUsesContextDeadline(t *testing.T) {
	t.Parallel()

	want := time.Now().Add(3 * time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), want)
	defer cancel()

	var gotDeadline time.Time
	sender := NewSMTPSender("smtp.postmarkapp.com", "587", "smtp-user", "smtp-pass", "noreply@retratar.com.ar")
	sender.send = func(_ context.Context, deadline time.Time, _, _ string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
		gotDeadline = deadline
		return nil
	}

	if err := sender.Send(ctx, "ana@example.com", "Sign in", "body"); err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}
	if !gotDeadline.Equal(want) {
		t.Errorf("deadline = %v, want %v (ctx's own deadline)", gotDeadline, want)
	}
}

// TestSMTPSenderFallsBackToDefaultTimeout proves a ctx with no deadline of
// its own still bounds the send, rather than blocking forever.
func TestSMTPSenderFallsBackToDefaultTimeout(t *testing.T) {
	t.Parallel()

	before := time.Now()
	var gotDeadline time.Time
	sender := NewSMTPSender("smtp.postmarkapp.com", "587", "smtp-user", "smtp-pass", "noreply@retratar.com.ar")
	sender.send = func(_ context.Context, deadline time.Time, _, _ string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
		gotDeadline = deadline
		return nil
	}

	if err := sender.Send(t.Context(), "ana@example.com", "Sign in", "body"); err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}

	if gotDeadline.Before(before.Add(defaultSendTimeout)) || gotDeadline.After(time.Now().Add(defaultSendTimeout)) {
		t.Errorf("deadline = %v, want roughly %v after the call", gotDeadline, defaultSendTimeout)
	}
}

func TestSMTPSenderWrapsRelayError(t *testing.T) {
	t.Parallel()

	relayErr := errors.New("connection refused")
	sender := NewSMTPSender("smtp.postmarkapp.com", "587", "smtp-user", "smtp-pass", "noreply@retratar.com.ar")
	sender.send = func(context.Context, time.Time, string, string, smtp.Auth, string, []string, []byte) error {
		return relayErr
	}

	err := sender.Send(t.Context(), "ana@example.com", "Sign in", "https://retratar.com.ar/login/abc")
	if !errors.Is(err, relayErr) {
		t.Fatalf("Send() error = %v, want it to wrap %v", err, relayErr)
	}
}

// fakeSMTPServer speaks just enough SMTP to drive sendMailWithDeadline
// through a real connection, rather than through the substituted sendFunc
// the tests above use. It handles exactly one connection.
//
//   - offerSTARTTLS: whether EHLO advertises the extension. Kept false in
//     every test here: a real STARTTLS handshake needs a TLS server, which
//     these tests don't need to prove the plaintext-refusal or the
//     recipient-redaction behaviour.
//   - rcptCode/rcptMsg: the response RCPT TO gets. A non-2xx code stops the
//     exchange there, which is all these tests need.
func fakeSMTPServer(t *testing.T, offerSTARTTLS bool, rcptCode int, rcptMsg string) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		r := bufio.NewReader(conn)
		reply := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

		reply("220 fake.example ESMTP")
		if _, err = r.ReadString('\n'); err != nil { // EHLO
			return
		}
		if offerSTARTTLS {
			reply("250-fake.example")
			reply("250-STARTTLS")
			reply("250 AUTH PLAIN")
		} else {
			reply("250-fake.example")
			reply("250 AUTH PLAIN")
		}

		line, err := r.ReadString('\n') // AUTH PLAIN ... (absent if the caller aborts after EHLO)
		if err != nil {
			return
		}
		if !strings.HasPrefix(strings.ToUpper(line), "AUTH PLAIN") {
			return
		}
		reply("235 2.7.0 Authentication successful")

		if line, err = r.ReadString('\n'); err != nil || !strings.HasPrefix(strings.ToUpper(line), "MAIL FROM") { // MAIL FROM
			return
		}
		reply("250 OK")

		if line, err = r.ReadString('\n'); err != nil || !strings.HasPrefix(strings.ToUpper(line), "RCPT TO") { // RCPT TO
			return
		}
		reply(strconv.Itoa(rcptCode) + " " + rcptMsg)
		if rcptCode/100 != 2 {
			return
		}

		if line, err = r.ReadString('\n'); err != nil || !strings.HasPrefix(strings.ToUpper(line), "DATA") { // DATA
			return
		}
		reply("354 go ahead")
		for {
			line, err = r.ReadString('\n')
			if err != nil || line == ".\r\n" {
				break
			}
		}
		reply("250 message accepted")

		if line, err = r.ReadString('\n'); err != nil || !strings.HasPrefix(strings.ToUpper(line), "QUIT") { // QUIT
			return
		}
		reply("221 bye")
	}()

	return ln.Addr().String()
}

// TestSendMailWithDeadlineRequiresSTARTTLSForRemoteHost proves a relay that
// does not offer STARTTLS is refused when the host is not loopback, instead
// of silently sending credentials and mail in clear (which smtp.PlainAuth
// already blocks) or failing with no explanation.
func TestSendMailWithDeadlineRequiresSTARTTLSForRemoteHost(t *testing.T) {
	t.Parallel()

	addr := fakeSMTPServer(t, false, 0, "")

	err := sendMailWithDeadline(t.Context(), time.Now().Add(5*time.Second), addr, "mail.example.com",
		smtp.PlainAuth("", "user", "pass", "mail.example.com"),
		"from@retratar.com.ar", []string{"ana@example.com"}, []byte("body"))
	if err == nil {
		t.Fatal("sendMailWithDeadline() error = nil, want a STARTTLS-required error")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("error = %v, want it to mention STARTTLS", err)
	}
	if strings.Contains(err.Error(), "ana@example.com") {
		t.Errorf("error = %v, must not contain the recipient address", err)
	}
}

// TestSendMailWithDeadlineAllowsPlaintextForLoopbackHost proves a loopback
// relay (the local dev fallback) is not held to the STARTTLS requirement.
func TestSendMailWithDeadlineAllowsPlaintextForLoopbackHost(t *testing.T) {
	t.Parallel()

	addr := fakeSMTPServer(t, false, 250, "OK")

	err := sendMailWithDeadline(t.Context(), time.Now().Add(5*time.Second), addr, "localhost",
		smtp.PlainAuth("", "user", "pass", "localhost"),
		"from@retratar.com.ar", []string{"ana@example.com"}, []byte("body"))
	if err != nil {
		t.Fatalf("sendMailWithDeadline() error = %v, want nil", err)
	}
}

// TestSendMailWithDeadlineRedactsRecipientInRcptError proves a RCPT failure
// never puts the full recipient address in the returned error: only the
// domain, per F7d.
func TestSendMailWithDeadlineRedactsRecipientInRcptError(t *testing.T) {
	t.Parallel()

	addr := fakeSMTPServer(t, false, 550, "no such user")

	err := sendMailWithDeadline(t.Context(), time.Now().Add(5*time.Second), addr, "localhost",
		smtp.PlainAuth("", "user", "pass", "localhost"),
		"from@retratar.com.ar", []string{"ana@example.com"}, []byte("body"))
	if err == nil {
		t.Fatal("sendMailWithDeadline() error = nil, want the RCPT failure")
	}
	if strings.Contains(err.Error(), "ana@example.com") || strings.Contains(err.Error(), "ana@") {
		t.Errorf("error = %v, must not contain the recipient's local part", err)
	}
	if !strings.Contains(err.Error(), "example.com") {
		t.Errorf("error = %v, want it to name the recipient's domain", err)
	}
}
