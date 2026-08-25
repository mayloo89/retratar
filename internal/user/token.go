package user

import (
	"crypto/rand"
	"crypto/sha256"
	"strings"
)

// maxTokenLength bounds what will be hashed. A magic link is short; anything
// longer than this is not a mistyped token, and there is no reason to run
// SHA-256 over an arbitrary amount of attacker-supplied input to find that out.
const maxTokenLength = 512

// newToken returns a magic link token and the digest to store for it.
//
// The token is the value that travels in the email. The digest is the only form
// that reaches the database, so a copy of the table cannot be replayed into
// anyone's account.
//
// crypto/rand.Text is the standard library's answer to exactly this: at least
// 128 bits of entropy, rendered in an alphabet that survives being pasted into
// a URL, with no encoding decision left to the caller. It cannot fail — the
// package panics rather than returning a weak value — so there is no error to
// handle and no branch that silently produces a guessable token.
func newToken() (raw string, digest []byte) {
	raw = rand.Text()
	return raw, hash(raw)
}

// hash digests a token exactly as newToken did, so that a lookup by digest
// finds the row that issued it.
//
// The string is hashed as it appears in the link, not decoded first. There is
// nothing to normalise: the only encoding a valid token can have is the one
// crypto/rand.Text produced, and a token that differs by so much as a space is
// one we did not issue.
func hash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// plausibleToken screens out input that cannot be a token before it reaches the
// database. It is a cheap filter, not a validation: passing it means only that
// the value is worth one indexed lookup.
func plausibleToken(raw string) bool {
	return raw != "" && len(raw) <= maxTokenLength && !strings.ContainsAny(raw, " \t\r\n")
}
