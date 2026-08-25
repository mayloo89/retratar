package web

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templateFS embed.FS

// templates is parsed once at package init. A malformed template is a
// build-time mistake, not a runtime one, so it fails loudly here rather than
// on the first request that reaches it.
var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))
