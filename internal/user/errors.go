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
)
