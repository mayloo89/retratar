package mail

import (
	"errors"
	"net/smtp"
	"strings"
	"testing"
)

func TestSMTPSenderSendsThroughTheRelay(t *testing.T) {
	t.Parallel()

	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte

	sender := NewSMTPSender("smtp.postmarkapp.com", "587", "token", "token", "noreply@retratar.com.ar")
	sender.send = func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotFrom, gotTo, gotMsg = addr, from, to, msg
		return nil
	}

	err := sender.Send(t.Context(), "ana@example.com", "Sign in", "https://retratar.com.ar/login/abc")
	if err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}

	if want := "smtp.postmarkapp.com:587"; gotAddr != want {
		t.Errorf("addr = %q, want %q", gotAddr, want)
	}
	if want := "noreply@retratar.com.ar"; gotFrom != want {
		t.Errorf("from = %q, want %q", gotFrom, want)
	}
	if len(gotTo) != 1 || gotTo[0] != "ana@example.com" {
		t.Errorf("to = %v, want [ana@example.com]", gotTo)
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

func TestSMTPSenderWrapsRelayError(t *testing.T) {
	t.Parallel()

	relayErr := errors.New("connection refused")
	sender := NewSMTPSender("smtp.postmarkapp.com", "587", "token", "token", "noreply@retratar.com.ar")
	sender.send = func(string, smtp.Auth, string, []string, []byte) error {
		return relayErr
	}

	err := sender.Send(t.Context(), "ana@example.com", "Sign in", "https://retratar.com.ar/login/abc")
	if !errors.Is(err, relayErr) {
		t.Fatalf("Send() error = %v, want it to wrap %v", err, relayErr)
	}
}
