package web

import (
	"bytes"
	"net/http"
)

// errorText is what the error page says for one status.
type errorText struct {
	Title, Message string
}

// errorCopy is the error page's copy for each status. Every word comes from
// here: nothing from the request (path, query, handle) reaches the page.
var errorCopy = map[int]errorText{
	http.StatusBadRequest:            {"Algo no salió bien", "No pudimos entender ese pedido. Volvé atrás y probá de nuevo."},
	http.StatusForbidden:             {"No se puede hacer eso", "Volvé a la página e intentá de nuevo desde ahí."},
	http.StatusNotFound:              {"Esta página no existe", "Puede que el link esté mal escrito o que la página ya no esté."},
	http.StatusRequestEntityTooLarge: {"Es demasiado grande", "Lo que mandaste pesa más de lo que aceptamos. Probá con algo más chico."},
	http.StatusTooManyRequests:       {"Demasiados intentos", "Esperá un rato y probá de nuevo."},
	http.StatusServiceUnavailable:    {"Estamos con mucho trabajo", "Probá de nuevo en un minuto."},
}

// errorFallback is the copy for any status errorCopy does not list.
var errorFallback = errorText{"Se nos rompió algo", "No es tu culpa. Probá de nuevo en un rato."}

// errorTextFor returns the copy for status.
func errorTextFor(status int) errorText {
	if t, ok := errorCopy[status]; ok {
		return t
	}
	return errorFallback
}

// errorData is what error.html renders.
type errorData struct {
	errorText
	Status int
	// AppURL is the app's home: "/" on the app surface, absolute on pages.
	AppURL, RandomURL, PrivacyURL, TermsURL string
	// HomeText labels the link to AppURL. On a page it invites the visitor
	// to make their own.
	HomeText string
}

// errorTemplate is the template writeErrorPage executes. Tests swap it to
// reach the plain-text fallback.
var errorTemplate = "error.html"

// writeErrorPage writes the styled error page for status. appBase is the
// app's absolute base URL when the request is on the pages surface, and ""
// on the app surface, where links stay relative.
//
// Some responses stay plain text on purpose: an unknown host (hostSplit), a
// recovered panic (Recover), and the handlers that serve CSS, fonts, images
// and icons, whose bodies no one reads as a page.
func writeErrorPage(w http.ResponseWriter, r *http.Request, status int, appBase string) {
	data := errorData{
		errorText:  errorTextFor(status),
		Status:     status,
		AppURL:     appBase + "/",
		RandomURL:  appBase + "/random",
		PrivacyURL: appBase + "/privacy",
		TermsURL:   appBase + "/terms",
		HomeText:   "Volver al inicio",
	}
	if appBase != "" && status == http.StatusNotFound {
		data.HomeText = "Hacé tu página en retratar"
	}

	// Rendered into a buffer so a template error never leaves half a page
	// behind a status that has already gone out.
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, errorTemplate, data); err != nil {
		http.Error(w, http.StatusText(status), status)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A 404 for a handle claimed a minute later must not stick in a cache.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// renderError is writeErrorPage for handlers, which know their surface from
// the request context.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int) {
	appBase := ""
	if _, onPages := HandleFrom(r.Context()); onPages {
		appBase = s.Config.BaseURL()
	}
	writeErrorPage(w, r, status, appBase)
}

// handleNotFound answers any path no other route claims, on both surfaces.
// Being a catch-all, it also answers a known path with the wrong method, so
// PUT /privacy is a 404 rather than a 405. Nothing relies on the 405.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound)
}
