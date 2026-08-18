// Package config loads and validates runtime configuration from the environment.
//
// Configuration is read once at startup and passed explicitly to the components
// that need it. Nothing in this package reads the environment after Load returns,
// and no other package reads the environment at all.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Environment names the deployment the process is running in. It gates
// behaviour that must differ between local development and production,
// most importantly whether cookies may be sent over plaintext HTTP.
type Environment string

const (
	// EnvDevelopment is local development. Logs are text, HSTS is off.
	EnvDevelopment Environment = "development"
	// EnvProduction is the deployed environment. Logs are JSON, HSTS is on.
	EnvProduction Environment = "production"
)

// Config is the fully validated runtime configuration.
type Config struct {
	Env  Environment
	Addr string

	// AppHost serves the owner-facing surface: login, editor, settings.
	// Visitors never see this hostname.
	AppHost string

	// PagesHost is the parent domain of the public pages. A page is served
	// from a subdomain of it, e.g. "sebas" + "." + PagesHost.
	//
	// AppHost and PagesHost must be different registrable domains. See
	// [Config.Validate] for why.
	PagesHost string

	// AdminAddr serves diagnostics: pprof, expvar, build info. It listens on
	// its own socket, and that socket must never be public. Empty disables it.
	AdminAddr string

	ShutdownTimeout time.Duration
}

// Getenv looks up an environment variable. Taking it as a parameter rather
// than calling [os.Getenv] keeps configuration testable: a test supplies a map
// instead of mutating process-wide state, so config tests run in parallel.
type Getenv func(string) string

// Load reads configuration through getenv and validates it. Pass [os.Getenv]
// in main.
//
// Every key is optional and falls back to a development default, except in
// production where [Config.Validate] rejects unsafe combinations.
func Load(getenv Getenv) (Config, error) {
	shutdown, err := durationEnv(getenv, "SHUTDOWN_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:             Environment(cmp.Or(getenv("ENV"), string(EnvDevelopment))),
		Addr:            cmp.Or(getenv("ADDR"), ":8080"),
		AppHost:         cmp.Or(getenv("APP_HOST"), "app.localhost:8080"),
		PagesHost:       cmp.Or(getenv("PAGES_HOST"), "pages.localhost:8080"),
		AdminAddr:       cmp.Or(getenv("ADMIN_ADDR"), "127.0.0.1:8081"),
		ShutdownTimeout: shutdown,
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// IsProduction reports whether the process runs in production. Callers use it
// to decide whether cookies get the Secure attribute and whether HSTS is sent.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// Validate checks the invariants the rest of the program relies on.
func (c Config) Validate() error {
	var errs []error

	switch c.Env {
	case EnvDevelopment, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("%w: ENV=%q, want %q or %q",
			ErrInvalidConfig, c.Env, EnvDevelopment, EnvProduction))
	}

	if c.Addr == "" {
		errs = append(errs, fmt.Errorf("%w: ADDR is empty", ErrInvalidConfig))
	}
	if c.AppHost == "" {
		errs = append(errs, fmt.Errorf("%w: APP_HOST is empty", ErrInvalidConfig))
	}
	if c.PagesHost == "" {
		errs = append(errs, fmt.Errorf("%w: PAGES_HOST is empty", ErrInvalidConfig))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, fmt.Errorf("%w: SHUTDOWN_TIMEOUT must be positive, got %s",
			ErrInvalidConfig, c.ShutdownTimeout))
	}

	// pprof exposes goroutine stacks and heap contents, and expvar exposes
	// internal counters. Bound to a public interface that is a data leak and a
	// cheap denial of service, so the process refuses to start that way.
	if c.AdminAddr != "" && !isLoopback(c.AdminAddr) {
		errs = append(errs, fmt.Errorf(
			"%w: ADMIN_ADDR %q must bind to loopback; it serves pprof and expvar",
			ErrInsecureHosts, c.AdminAddr))
	}

	// The session cookie is issued by AppHost. If PagesHost were AppHost or a
	// subdomain of it, every user-controlled page would sit inside the cookie's
	// origin: a page could read the session cookie, or set a Domain-scoped
	// cookie on the parent and fix the owner's session. Separate registrable
	// domains are the boundary that makes user-controlled pages safe to host.
	if c.AppHost != "" && c.PagesHost != "" {
		app, pages := hostOnly(c.AppHost), hostOnly(c.PagesHost)
		switch {
		case app == pages:
			errs = append(errs, fmt.Errorf(
				"%w: APP_HOST and PAGES_HOST are both %q; they must be different registrable domains",
				ErrInsecureHosts, app))
		case isSubdomainOf(app, pages) || isSubdomainOf(pages, app):
			errs = append(errs, fmt.Errorf(
				"%w: APP_HOST %q and PAGES_HOST %q share a registrable domain; they must not",
				ErrInsecureHosts, app, pages))
		}
	}

	return errors.Join(errs...)
}

// PageHostFor returns the hostname a page is served from.
func (c Config) PageHostFor(handle string) string {
	return handle + "." + c.PagesHost
}

// BaseURL returns the absolute origin of the owner-facing app.
func (c Config) BaseURL() string {
	scheme := "http"
	if c.IsProduction() {
		scheme = "https"
	}
	u := url.URL{Scheme: scheme, Host: c.AppHost}
	return u.String()
}

// isLoopback reports whether addr binds only to the local host.
func isLoopback(addr string) bool {
	host := hostOnly(addr)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostOnly strips an optional port from a host[:port] string, and the brackets
// from an IPv6 literal. net.SplitHostPort alone is not enough: it errors on a
// bare host, which is a valid input here.
func hostOnly(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return strings.Trim(hostport, "[]")
}

// isSubdomainOf reports whether host sits under parent, e.g. "a.example.com"
// under "example.com". Equal hosts are not subdomains of each other.
func isSubdomainOf(host, parent string) bool {
	return strings.HasSuffix(host, "."+parent)
}

func durationEnv(getenv Getenv, key string, fallback time.Duration) (time.Duration, error) {
	raw := getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q: %w", ErrInvalidConfig, key, raw, err)
	}
	return d, nil
}
