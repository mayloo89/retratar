package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorCopy(t *testing.T) {
	fallback := errorText{"Se nos rompió algo", "No es tu culpa. Probá de nuevo en un rato."}
	for status, want := range map[int]errorText{
		400: {"Algo no salió bien", "No pudimos entender ese pedido. Volvé atrás y probá de nuevo."},
		403: {"No se puede hacer eso", "Volvé a la página e intentá de nuevo desde ahí."},
		404: {"Esta página no existe", "Puede que el link esté mal escrito o que la página ya no esté."},
		413: {"Es demasiado grande", "Lo que mandaste pesa más de lo que aceptamos. Probá con algo más chico."},
		429: {"Demasiados intentos", "Esperá un rato y probá de nuevo."},
		503: {"Estamos con mucho trabajo", "Probá de nuevo en un minuto."},
		418: fallback,
		500: fallback,
	} {
		if got := errorTextFor(status); got != want {
			t.Errorf("errorTextFor(%d) = %+v, want %+v", status, got, want)
		}
	}
}

// TestWriteErrorPage_TemplateFailure swaps in a template that does not exist,
// so execution fails before anything is written.
func TestWriteErrorPage_TemplateFailure(t *testing.T) {
	saved := errorTemplate
	errorTemplate = "missing.html"
	t.Cleanup(func() { errorTemplate = saved })

	rec := httptest.NewRecorder()
	writeErrorPage(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusNotFound, "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if got := rec.Body.String(); got != "Not Found\n" {
		t.Errorf("body = %q, want the plain status text", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", got)
	}
}
