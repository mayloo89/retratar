package session

import (
	"crypto/rand"
	"crypto/sha256"
	"strings"
)

// maxTokenLength bounds what will be hashed, for the same reason
// user.maxTokenLength does: a session cookie value is short, and anything
// longer is not worth running SHA-256 over to find that out.
const maxTokenLength = 512

// newToken returns a session token and the digest to store for it. Mirrors
// user.newToken: the token is the value that goes in the cookie, the digest
// is the only form that reaches the database.
func newToken() (raw string, digest []byte) {
	raw = rand.Text()
	return raw, hash(raw)
}

// hash digests a token exactly as newToken did, so that a lookup by digest
// finds the row that issued it.
func hash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// plausibleToken screens out input that cannot be a token before it reaches
// the database. It is a cheap filter, not a validation.
func plausibleToken(raw string) bool {
	return raw != "" && len(raw) <= maxTokenLength && !strings.ContainsAny(raw, " \t\r\n")
}
