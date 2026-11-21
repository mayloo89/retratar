package web

import (
	"net/http"
)

// handleFaviconSVG serves the SVG favicon.
func (s *Server) handleFaviconSVG(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(faviconSVG)
}

// handleAppleTouchIconPNG serves the apple-touch-icon PNG (180x180).
func (s *Server) handleAppleTouchIconPNG(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(appleTouchIconPNG)
}

// handleFaviconICO serves the same PNG bytes as favicon.ico.
// Browsers and crawlers ask for it whatever the HTML says; a PNG under that
// name works in every browser and keeps it out of the 404 logs.
func (s *Server) handleFaviconICO(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(appleTouchIconPNG)
}
