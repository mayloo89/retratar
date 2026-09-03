package web

import _ "embed"

// themeCSS is the only stylesheet a public page loads. It is not a theme
// system — that is Phase 2's block editor — just enough to prove that mood
// tints the page. See internal/web/static/theme.css.
//
//go:embed static/theme.css
var themeCSS []byte
