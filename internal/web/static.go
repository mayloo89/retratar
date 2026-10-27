package web

import (
	"embed"
	"io/fs"
)

// themeCSS is the only stylesheet a public page loads. It is not a theme
// system — that is Phase 2's block editor — just enough to prove that mood
// tints the page. See internal/web/static/theme.css.
//
//go:embed static/theme.css
var themeCSS []byte

// appCSS is the one stylesheet the app surface loads: the back of the
// notebook, in the same family as themeCSS but quieter. Its .mood-<key> rules
// are a copy of theme.css's, and a test keeps the two in sync. See
// internal/web/static/app.css.
//
//go:embed static/app.css
var appCSS []byte

// fontsFS holds the self-hosted fonts theme.css declares. Each file name
// carries a hash of its content, so GET /fonts/{file} can be cached forever.
// internal/style lists the files, and a test keeps the list, the directory and
// theme.css's @font-face rules in sync. See static/fonts/README.md.
//
//go:embed static/fonts/*.woff2
var fontsFS embed.FS

// fontFiles maps a file name to its bytes, so a request is a map lookup and
// never a path into the filesystem.
var fontFiles = func() map[string][]byte {
	entries, err := fs.ReadDir(fontsFS, "static/fonts")
	if err != nil {
		panic("web: read embedded fonts: " + err.Error())
	}
	files := make(map[string][]byte, len(entries))
	for _, e := range entries {
		b, err := fontsFS.ReadFile("static/fonts/" + e.Name())
		if err != nil {
			panic("web: read embedded font: " + err.Error())
		}
		files[e.Name()] = b
	}
	return files
}()
