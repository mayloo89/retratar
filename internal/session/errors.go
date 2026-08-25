package session

import "errors"

// ErrInvalidSession means a session token does not authenticate anyone. It
// covers three distinct facts deliberately: the session never existed, it has
// expired, or it has been revoked.
//
// Keep them merged, for the same reason user.ErrInvalidToken does: telling a
// caller which one applies turns session lookup into an oracle over both
// guessed tokens and other people's logout history.
var ErrInvalidSession = errors.New("invalid or expired session")
