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
	"net/url"
	"os"
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

	ShutdownTimeout time.Duration
}

// Load reads configuration from the environment and validates it.
//
// Every key is optional and falls back to a development default, except in
// production where [Config.Validate] rejects unsafe combinations.
func Load() (Config, error) {
	shutdown, err := durationEnv("SHUTDOWN_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:             Environment(cmp.Or(os.Getenv("ENV"), string(EnvDevelopment))),
		Addr:            cmp.Or(os.Getenv("ADDR"), ":8080"),
		AppHost:         cmp.Or(os.Getenv("APP_HOST"), "app.localhost:8080"),
		PagesHost:       cmp.Or(os.Getenv("PAGES_HOST"), "pages.localhost:8080"),
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

// hostOnly strips an optional port from a host[:port] string. It does not use
// net.SplitHostPort, which fails on a bare host.
func hostOnly(hostport string) string {
	if i := strings.LastIndexByte(hostport, ':'); i >= 0 {
		return hostport[:i]
	}
	return hostport
}

// isSubdomainOf reports whether host sits under parent, e.g. "a.example.com"
// under "example.com". Equal hosts are not subdomains of each other.
func isSubdomainOf(host, parent string) bool {
	return strings.HasSuffix(host, "."+parent)
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q: %w", ErrInvalidConfig, key, raw, err)
	}
	return d, nil
}
