package user

import "errors"

var (
	// ErrInvalidEmail means the address was empty, malformed, or too long to be
	// deliverable. It says nothing about whether an account exists.
	ErrInvalidEmail = errors.New("invalid email address")

	// ErrInvalidToken means a magic link cannot be used. It covers three
	// distinct facts deliberately: the token never existed, it has expired, or
	// it has already been spent.
	//
	// Keep them merged. Telling a caller which one applies turns the login
	// endpoint into an oracle that confirms which guessed tokens were once
	// real, and confirms that an address has a login in flight.
	ErrInvalidToken = errors.New("invalid or expired login token")

	// ErrUserNotFound means no account exists for the ID or handle looked up.
	// For a lookup by ID this carries no anti-enumeration weight of its own: a
	// session's user_id references users(id), so a caller resolving one only
	// ever sees this if the account was deleted out from under a live
	// session. For a lookup by handle it is the ordinary no-such-page case.
	ErrUserNotFound = errors.New("user not found")

	// ErrInvalidHandle means the string cannot be used as a handle; see
	// [ValidHandle].
	ErrInvalidHandle = errors.New("invalid handle")

	// ErrHandleTaken means another account already holds that handle.
	ErrHandleTaken = errors.New("handle already taken")

	// ErrHandleAlreadySet means the account already has a handle. Handles are
	// claimed once and never reassigned by this method.
	ErrHandleAlreadySet = errors.New("account already has a handle")
)
