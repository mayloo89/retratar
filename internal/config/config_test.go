package config_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mayloo89/retratar/internal/config"
)

// env returns a Getenv backed by a map, so config tests never touch process
// state and can run in parallel.
func env(pairs map[string]string) config.Getenv {
	return func(key string) string { return pairs[key] }
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.Env != config.EnvDevelopment {
		t.Errorf("Env = %q, want %q", cfg.Env, config.EnvDevelopment)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 15s", cfg.ShutdownTimeout)
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		"ENV":              "production",
		"ADDR":             ":9000",
		"APP_HOST":         "retratar.com.ar",
		"PAGES_HOST":       "retrat.ar",
		"DATABASE_URL":     "postgres://u:p@db.internal:5432/retratar",
		"SHUTDOWN_TIMEOUT": "30s",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !cfg.IsProduction() {
		t.Error("IsProduction() = false, want true")
	}
	if cfg.Addr != ":9000" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":9000")
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 30s", cfg.ShutdownTimeout)
	}
}

func TestLoadRejectsBadDuration(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{"SHUTDOWN_TIMEOUT": "soon"}))
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("Load() error = %v, want ErrInvalidConfig", err)
	}
}

// TestValidateRejectsSharedRegistrableDomain is a security test, not a
// correctness one. Serving user-controlled pages inside the origin that issues
// the session cookie makes account takeover a CSS-free, one-line attack. The
// process must refuse to start rather than run in that shape.
func TestValidateRejectsSharedRegistrableDomain(t *testing.T) {
	t.Parallel()

	base := config.Config{
		Env:             config.EnvProduction,
		Addr:            ":8080",
		DatabaseURL:     "postgres://u:p@db.internal:5432/retratar",
		ShutdownTimeout: time.Second,
	}

	tests := []struct {
		name      string
		appHost   string
		pagesHost string
		wantErr   error
	}{
		{"identical hosts", "retratar.com.ar", "retratar.com.ar", config.ErrInsecureHosts},
		{"identical modulo port", "app.local:8080", "app.local:9090", config.ErrInsecureHosts},
		{"pages under app", "retrat.ar", "pages.retrat.ar", config.ErrInsecureHosts},
		{"app under pages", "app.retrat.ar", "retrat.ar", config.ErrInsecureHosts},
		{"separate registrable domains", "retratar.com.ar", "retrat.ar", nil},
		{"separate in development", "app.localhost:8080", "pages.localhost:8080", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := base
			cfg.AppHost, cfg.PagesHost = tt.appHost, tt.pagesHost

			err := cfg.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCollectsEveryProblem(t *testing.T) {
	t.Parallel()

	var cfg config.Config // zero value: everything is wrong

	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("Validate() error = %v, want ErrInvalidConfig", err)
	}
	// errors.Join reports each problem on its own line, so a misconfigured
	// deploy is fixed in one pass instead of one restart per typo.
	if got := len(errors.Join(err).Error()); got == 0 {
		t.Error("Validate() error message is empty")
	}
}

// TestValidateRejectsPublicAdminAddr guards the diagnostics listener. pprof
// serves goroutine stacks and heap contents to anyone who can reach it.
func TestValidateRejectsPublicAdminAddr(t *testing.T) {
	t.Parallel()

	base := config.Config{
		Env:             config.EnvProduction,
		Addr:            ":8080",
		AppHost:         "retratar.com.ar",
		PagesHost:       "retrat.ar",
		DatabaseURL:     "postgres://u:p@db.internal:5432/retratar",
		ShutdownTimeout: time.Second,
	}

	tests := []struct {
		addr    string
		wantErr error
	}{
		{"127.0.0.1:8081", nil},
		{"localhost:8081", nil},
		{"[::1]:8081", nil},
		{"", nil}, // disabled
		{":8081", config.ErrInsecureHosts},
		{"0.0.0.0:8081", config.ErrInsecureHosts},
		{"10.0.0.5:8081", config.ErrInsecureHosts},
	}

	for _, tt := range tests {
		name := tt.addr
		if name == "" {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := base
			cfg.AdminAddr = tt.addr

			err := cfg.Validate()
			if tt.wantErr == nil && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestPageHostFor(t *testing.T) {
	t.Parallel()

	cfg := config.Config{PagesHost: "retrat.ar"}

	if got, want := cfg.PageHostFor("sebas"), "sebas.retrat.ar"; got != want {
		t.Errorf("PageHostFor(%q) = %q, want %q", "sebas", got, want)
	}
}

func TestBaseURLSchemeFollowsEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env  config.Environment
		want string
	}{
		{config.EnvProduction, "https://retratar.com.ar"},
		{config.EnvDevelopment, "http://retratar.com.ar"},
	}

	for _, tt := range tests {
		t.Run(string(tt.env), func(t *testing.T) {
			t.Parallel()

			cfg := config.Config{Env: tt.env, AppHost: "retratar.com.ar"}
			if got := cfg.BaseURL(); got != tt.want {
				t.Errorf("BaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateDatabaseURL(t *testing.T) {
	t.Parallel()

	base := config.Config{
		Addr:            ":8080",
		AppHost:         "retratar.com.ar",
		PagesHost:       "retrat.ar",
		AdminAddr:       "127.0.0.1:8081",
		ShutdownTimeout: time.Second,
	}

	tests := []struct {
		name    string
		env     config.Environment
		url     string
		wantErr error
	}{
		{
			name: "postgres scheme",
			env:  config.EnvProduction,
			url:  "postgres://u:p@db.internal:5432/retratar",
		},
		{
			name: "postgresql scheme",
			env:  config.EnvProduction,
			url:  "postgresql://u:p@db.internal:5432/retratar",
		},
		{
			name:    "empty",
			env:     config.EnvProduction,
			url:     "",
			wantErr: config.ErrInvalidConfig,
		},
		{
			name:    "another database entirely",
			env:     config.EnvProduction,
			url:     "mysql://u:p@db.internal:3306/retratar",
			wantErr: config.ErrInvalidConfig,
		},
		{
			// The whole point of the check: credentials and every row that
			// follows would cross the network in clear.
			name:    "plaintext in production",
			env:     config.EnvProduction,
			url:     "postgres://u:p@db.internal:5432/retratar?sslmode=disable",
			wantErr: config.ErrInsecureHosts,
		},
		{
			// The same URL is how everyone's laptop talks to the container in
			// compose.yaml, so development must accept it.
			name: "plaintext in development",
			env:  config.EnvDevelopment,
			url:  "postgres://u:p@127.0.0.1:5432/retratar?sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := base
			cfg.Env = tt.env
			cfg.DatabaseURL = tt.url

			err := cfg.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadDefaultsToTheComposeDatabase(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !strings.HasPrefix(cfg.DatabaseURL, "postgres://") {
		t.Errorf("DatabaseURL = %q, want a postgres URL", cfg.DatabaseURL)
	}
}
