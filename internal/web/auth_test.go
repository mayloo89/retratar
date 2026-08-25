package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mayloo89/retratar/internal/web"
)

// TestCurrentUser_FailsClosedOnLookupError guards against a real database
// failure being mistaken for an invalid cookie: a canceled context makes the
// session lookup return a genuine error that isn't session.ErrInvalidSession,
// and CurrentUser must fail the request instead of quietly continuing
// unauthenticated.
func TestCurrentUser_FailsClosedOnLookupError(t *testing.T) {
	srv, _ := newLoginServer(t)
	h := srv.Handler()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	req.Host = "retratar.com.ar"
	req.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: "deliberately-plausible-token"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
