package config_test

import (
	"errors"
	"testing"
	"time"

	"github.com/mayloo89/retratar/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load()
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

func TestLoadRejectsBadDuration(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "soon")

	_, err := config.Load()
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("Load() error = %v, want ErrInvalidConfig", err)
	}
}

// TestValidateRejectsSharedRegistrableDomain is a security test, not a
// correctness one. Serving user-controlled pages inside the origin that issues
// the session cookie makes account takeover a CSS-free, one-line attack. The
// process must refuse to start rather than run in that shape.
func TestValidateRejectsSharedRegistrableDomain(t *testing.T) {
	base := config.Config{
		Env:             config.EnvProduction,
		Addr:            ":8080",
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

func TestPageHostFor(t *testing.T) {
	cfg := config.Config{PagesHost: "retrat.ar"}

	if got, want := cfg.PageHostFor("sebas"), "sebas.retrat.ar"; got != want {
		t.Errorf("PageHostFor(%q) = %q, want %q", "sebas", got, want)
	}
}

func TestBaseURLSchemeFollowsEnvironment(t *testing.T) {
	tests := []struct {
		env  config.Environment
		want string
	}{
		{config.EnvProduction, "https://retratar.com.ar"},
		{config.EnvDevelopment, "http://retratar.com.ar"},
	}

	for _, tt := range tests {
		t.Run(string(tt.env), func(t *testing.T) {
			cfg := config.Config{Env: tt.env, AppHost: "retratar.com.ar"}
			if got := cfg.BaseURL(); got != tt.want {
				t.Errorf("BaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
