package mail_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/mail"
)

func TestLogSenderWritesTheMessage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sender := mail.NewLogSender(slog.New(slog.NewTextHandler(&buf, nil)))

	if err := sender.Send(t.Context(), "ana@example.com", "Sign in", "https://retratar.com.ar/login/abc"); err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}

	out := buf.String()
	for _, want := range []string{"ana@example.com", "Sign in", "https://retratar.com.ar/login/abc"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output %q does not contain %q", out, want)
		}
	}
}
