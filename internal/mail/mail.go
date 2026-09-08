// Package mail defines how the rest of the program sends email, without
// deciding how any message actually leaves the building.
//
// [LogSender] writes to the logger instead of sending. It exists for local
// development and is unsafe in production — a magic link in the log turns
// the log into a credential store, and logs get shipped, indexed and read by
// people who are not the account owner. Wiring in cmd/server refuses to start
// it there; production uses [SMTPSender] instead.
package mail

import "context"

// Sender delivers a single email.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}
