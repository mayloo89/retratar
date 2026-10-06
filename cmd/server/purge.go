package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/mayloo89/retratar/internal/session"
	"github.com/mayloo89/retratar/internal/user"
)

// purgeRetention is how long an expired or revoked row is kept before the purge
// deletes it. Long enough to debug a login problem someone reports days later;
// short enough that the addresses of people who only typed into the sign-in
// form are not held longer than that. It must outlast [user.LoginBudgetWindow].
const purgeRetention = 7 * 24 * time.Hour

// purgeInterval is how often the purge runs. Hourly is plenty against a
// retention measured in days.
const purgeInterval = time.Hour

// purgeLoop deletes expired login tokens and sessions: once immediately, then
// every interval, until ctx is done.
//
// A failed run is logged and the loop carries on; one bad purge must not stop
// the process or the next attempt. Logs carry counts only, never addresses.
//
// It is not waited on at shutdown. Cancelling ctx stops it, and a delete cut
// short by the cancellation fails harmlessly: the rows are simply purged by a
// later run.
func purgeLoop(ctx context.Context, logger *slog.Logger, interval, retention time.Duration, users *user.Service, sessions *session.Service) {
	purge := func() {
		tokens, err := users.PurgeExpiredLoginTokens(ctx, retention)
		if err != nil {
			logger.Error("purge login tokens failed", slog.Any("error", err))
			return
		}
		expired, err := sessions.PurgeExpired(ctx, retention)
		if err != nil {
			logger.Error("purge sessions failed", slog.Any("error", err))
			return
		}
		logger.Info("purged expired rows",
			slog.Int64("login_tokens", tokens),
			slog.Int64("sessions", expired),
		)
	}

	purge()

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			purge()
		}
	}
}
