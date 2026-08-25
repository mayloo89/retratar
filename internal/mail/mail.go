// Package mail defines how the rest of the program sends email, without
// deciding how any message actually leaves the building.
//
// The only implementation here writes to the logger. It exists for local
// development and is unsafe in production — a magic link in the log turns
// the log into a credential store, and logs get shipped, indexed and read by
// people who are not the account owner. Wiring in cmd/server refuses to start
// it there; see [LogSender].
package mail

import "context"

// Sender delivers a single email. A real provider (SMTP, an HTTP API) is a
// later task; this interface is what that provider will implement.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}
