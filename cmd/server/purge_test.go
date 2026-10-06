package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mayloo89/retratar/internal/session"
	"github.com/mayloo89/retratar/internal/testdb"
	"github.com/mayloo89/retratar/internal/user"
)

func TestPurgeRetentionOutlastsLoginBudget(t *testing.T) {
	t.Parallel()

	if purgeRetention <= user.LoginBudgetWindow {
		t.Errorf("purgeRetention = %s, want more than LoginBudgetWindow (%s): a shorter retention would delete rows the sign-in budget still counts",
			purgeRetention, user.LoginBudgetWindow)
	}
}

func TestPurgeLoopStopsOnCancel(t *testing.T) {
	t.Parallel()

	pool := testdb.New(t)
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		defer close(done)
		purgeLoop(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), 10*time.Millisecond, purgeRetention,
			user.NewService(pool), session.NewService(pool))
	}()

	time.Sleep(50 * time.Millisecond) // let a few ticks run
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("purgeLoop did not return within 1s of cancel")
	}
}
