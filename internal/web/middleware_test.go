package web_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/web"
)

func TestRequestLoggerRedactsLoginToken(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := web.RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const token = "super-secret-token"
	req := httptest.NewRequest(http.MethodGet, "/login/"+token, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	out := buf.String()
	if strings.Contains(out, token) {
		t.Fatalf("log contains raw token: %s", out)
	}
	if !strings.Contains(out, "/login/{token}") {
		t.Fatalf("log missing redacted path: %s", out)
	}
}

func TestRecoverRedactsLoginToken(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := web.Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	const token = "super-secret-token"
	req := httptest.NewRequest(http.MethodGet, "/login/"+token, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	out := buf.String()
	if strings.Contains(out, token) {
		t.Fatalf("panic log contains raw token: %s", out)
	}
	if !strings.Contains(out, "/login/{token}") {
		t.Fatalf("panic log missing redacted path: %s", out)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want 500", rec.Code)
	}
}

func TestRequestLoggerLeavesOtherPathsAlone(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := web.RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mood", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	out := buf.String()
	if !strings.Contains(out, "/mood") {
		t.Fatalf("log missing unredacted path: %s", out)
	}
}
