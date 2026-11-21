package web_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTemplates_HaveViewportMeta guards every template, present and future: a
// page without the tag is laid out at about 980px on a phone and shrunk.
func TestTemplates_HaveViewportMeta(t *testing.T) {
	const tag = `<meta name="viewport" content="width=device-width, initial-scale=1">`

	files, err := filepath.Glob("templates/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("Glob(templates/*.html) = %v, %v, want the templates", files, err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(b), tag); n != 1 {
			t.Errorf("%s has the viewport tag %d times, want exactly once", f, n)
		}
	}
}

// TestTemplates_HaveIconLinks guards every template, present and future: the
// favicon and apple-touch-icon links must appear exactly once in each template.
func TestTemplates_HaveIconLinks(t *testing.T) {
	const favicon = `<link rel="icon" href="/favicon.svg" type="image/svg+xml">`
	const appleTouch = `<link rel="apple-touch-icon" href="/apple-touch-icon.png">`

	files, err := filepath.Glob("templates/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("Glob(templates/*.html) = %v, %v, want the templates", files, err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		content := string(b)
		if n := strings.Count(content, favicon); n != 1 {
			t.Errorf("%s has the favicon link %d times, want exactly once", f, n)
		}
		if n := strings.Count(content, appleTouch); n != 1 {
			t.Errorf("%s has the apple-touch-icon link %d times, want exactly once", f, n)
		}
	}
}
