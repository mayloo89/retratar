package web

import _ "embed"

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
