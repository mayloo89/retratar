package mail

import (
	"context"
	"errors"
	"net/smtp"
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
