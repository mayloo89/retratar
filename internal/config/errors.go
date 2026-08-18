package config

import "errors"

var (
	// ErrInvalidConfig means a value was missing or malformed.
	ErrInvalidConfig = errors.New("invalid config")

	// ErrInsecureHosts means APP_HOST and PAGES_HOST share a registrable
	// domain, which would let a user-controlled page reach the session cookie.
	ErrInsecureHosts = errors.New("insecure host configuration")
)
