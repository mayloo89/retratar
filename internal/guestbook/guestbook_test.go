package guestbook_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/guestbook"
)

func TestNormaliseBody(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"trims whitespace", "  hola  ", "hola", nil},
		{"keeps newline", "hola\nchau", "hola\nchau", nil},
		{"normalises CRLF", "hola\r\nchau", "hola\nchau", nil},
		{"exactly 280 runes", strings.Repeat("a", 280), strings.Repeat("a", 280), nil},
		{"281 runes", strings.Repeat("a", 281), "", guestbook.ErrInvalidBody},
		{"empty", "", "", guestbook.ErrInvalidBody},
		{"whitespace only", " \n\t ", "", guestbook.ErrInvalidBody},
		{"tab", "hola\tchau", "", guestbook.ErrInvalidBody},
		{"NUL", "hola\x00chau", "", guestbook.ErrInvalidBody},
		{"invalid UTF-8", "ok\xffok", "", guestbook.ErrInvalidBody},
		{"lone carriage return", "hola\rchau", "", guestbook.ErrInvalidBody},
		{"counts runes not bytes", strings.Repeat("ñ", 280), strings.Repeat("ñ", 280), nil},
		{"281 multibyte runes", strings.Repeat("ñ", 281), "", guestbook.ErrInvalidBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := guestbook.NormaliseBody(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NormaliseBody() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("NormaliseBody() = %q, want %q", got, tt.want)
			}
		})
	}
}
