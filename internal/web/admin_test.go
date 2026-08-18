package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mayloo89/retratar/internal/web"
)

// TestDefaultMuxServesPprof documents a hazard rather than a guarantee.
//
// net/http/pprof registers /debug/pprof/ on http.DefaultServeMux from its
// init, so importing it anywhere in the binary arms the default mux. No import
// form prevents this. The defence is that nothing in this program ever serves
// the default mux, which forbidigo enforces at lint time.
//
// If this test ever starts failing, the standard library changed and the
// forbidigo rules can be revisited.
func TestDefaultMuxServesPprof(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()

	http.DefaultServeMux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Skip("net/http/pprof no longer registers on DefaultServeMux")
	}
}

// TestPublicSurfacesHaveNoDiagnostics keeps pprof, expvar and build metadata
// off both public hostnames.
func TestPublicSurfacesHaveNoDiagnostics(t *testing.T) {
	t.Parallel()

	h := testServer(t).Handler()

	for _, host := range []string{"retratar.com.ar", "sebas.retrat.ar"} {
		for _, path := range []string{"/debug/pprof/", "/debug/vars", "/version"} {
			resp := get(t, h, host, path)
			_ = resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s%s = %d, want 404", host, path, resp.StatusCode)
			}
		}
	}
}

func TestAdminHandlerServesDiagnostics(t *testing.T) {
	t.Parallel()

	h := web.AdminHandler()

	for _, path := range []string{"/debug/pprof/", "/debug/vars", "/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("admin %s = %d, want 200", path, rec.Code)
		}
	}
}
