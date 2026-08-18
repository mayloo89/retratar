package web

import (
	"encoding/json"
	"expvar"
	"net/http"
	"net/http/pprof"

	"github.com/mayloo89/retratar/internal/buildinfo"
)

// AdminHandler builds the diagnostics surface: pprof, expvar and build info.
//
// Importing net/http/pprof registers /debug/pprof/ on http.DefaultServeMux
// from the package's init, and there is no import form that avoids it. So the
// default mux in this process serves pprof, and the protection is that nothing
// ever serves the default mux: every server is constructed with an explicit
// handler, and forbidigo in .golangci.yml rejects http.ListenAndServe,
// http.Handle and http.HandleFunc, which are the ways it would leak.
//
// The handlers below are registered explicitly rather than mounted from the
// default mux so the routes carry method patterns and stay visible here.
//
// The caller must serve this handler on a loopback listener.
// config.Config.Validate refuses any other bind address.
func AdminHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	mux.Handle("GET /debug/vars", expvar.Handler())

	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(buildinfo.Get())
	})

	return mux
}
