package guestbook

import "errors"

var (
	// ErrInvalidBody means the entry is empty, too long, or contains a
	// control character other than a line break; see [NormaliseBody].
	ErrInvalidBody = errors.New("invalid guestbook body")

	// ErrOwnPage means the author tried to sign their own guestbook.
	ErrOwnPage = errors.New("cannot sign own guestbook")

	// ErrEntryNotFound means no entry with that ID is on the page. An entry
	// on someone else's page is reported the same way.
	ErrEntryNotFound = errors.New("guestbook entry not found")
)
