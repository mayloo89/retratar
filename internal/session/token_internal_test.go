package session

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
)

func TestNewTokenReturnsAFreshValueAndItsDigest(t *testing.T) {
	t.Parallel()

	raw, digest := newToken()

	if raw == "" {
		t.Fatal("newToken() returned an empty token")
	}

	want := sha256.Sum256([]byte(raw))
	if !bytes.Equal(digest, want[:]) {
		t.Error("digest is not SHA-256 of the token")
	}
	if bytes.Contains(digest, []byte(raw)) {
		t.Error("digest contains the token")
	}
}

func TestNewTokenDoesNotRepeat(t *testing.T) {
	t.Parallel()

	const draws = 1000
	seen := make(map[string]struct{}, draws)

	for range draws {
		raw, _ := newToken()
		if _, dup := seen[raw]; dup {
			t.Fatalf("newToken() repeated a token within %d draws", draws)
		}
		seen[raw] = struct{}{}
	}
}

func TestPlausibleToken(t *testing.T) {
	t.Parallel()

	issued, _ := newToken()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "one we issued", raw: issued, want: true},
		{name: "empty", raw: ""},
		{name: "a space", raw: " "},
		{name: "wrapped by a mail client", raw: "abc\r\ndef"},
		{name: "longer than any token", raw: strings.Repeat("a", maxTokenLength+1)},
		{name: "the right shape but never issued", raw: "AAAAAAAAAAAAAAAAAAAAAAAAAA", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := plausibleToken(tt.raw); got != tt.want {
				t.Errorf("plausibleToken(%.20q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
