package mood

import "errors"

var (
	// ErrInvalidMood means the key is not one of the fixed vocabulary.
	ErrInvalidMood = errors.New("invalid mood")

	// ErrInvalidNote means the note is too long or contains a control
	// character; see [NormaliseNote].
	ErrInvalidNote = errors.New("invalid mood note")

	// ErrNoMood means the user has never set a mood. It is not a failure —
	// callers render it as an empty state, the same way a fresh account with
	// no photo yet is not an error.
	ErrNoMood = errors.New("no mood set")
)
