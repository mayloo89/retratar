package mail

import (
	"context"
	"log/slog"
)

// LogSender writes the message to the logger instead of sending it. It is
// meant only for local development, where there is no mailbox to check and
// the terminal is faster than clicking through a fake inbox; see the package
// doc for why cmd/server refuses to construct one in production.
type LogSender struct {
	logger *slog.Logger
}

// NewLogSender wires a LogSender to a logger.
func NewLogSender(logger *slog.Logger) *LogSender {
	return &LogSender{logger: logger}
}

// Send logs the message and returns nil: writing to the logger is not an
// operation that fails the way a network send can.
func (s *LogSender) Send(ctx context.Context, to, subject, body string) error {
	s.logger.InfoContext(ctx, "mail",
		slog.String("to", to),
		slog.String("subject", subject),
		slog.String("body", body))
	return nil
}
