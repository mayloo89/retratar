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
