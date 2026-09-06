package pbvex

import (
	"os"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

var smtpEnvKeys = []string{
	"PBVEX_SMTP_ENABLED",
	"PBVEX_SMTP_HOST",
	"PBVEX_SMTP_PORT",
	"PBVEX_SMTP_USERNAME",
	"PBVEX_SMTP_PASSWORD",
	"PBVEX_SMTP_AUTH_METHOD",
	"PBVEX_SMTP_TLS",
	"PBVEX_SMTP_LOCAL_NAME",
	"PBVEX_SMTP_SENDER_ADDRESS",
	"PBVEX_SMTP_SENDER_NAME",
}

// clearSMTPEnv unsets every PBVEX_SMTP_* variable and restores the prior
// process environment when the test finishes.
func clearSMTPEnv(t *testing.T) {
	t.Helper()
	for _, key := range smtpEnvKeys {
		old, had := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
		if had {
			t.Cleanup(func() { os.Setenv(key, old) })
		}
	}
}

func strPtr(v string) *string { return &v }

func intPtr(v int) *int { return &v }

func boolPtr(v bool) *bool { return &v }

func TestSMTPConfigEmpty(t *testing.T) {
	if !(SMTPConfig{}).Empty() {
		t.Fatal("zero value config should be empty")
	}
	if (SMTPConfig{Host: strPtr("smtp.example.com")}).Empty() {
		t.Fatal("config with an override should not be empty")
	}
}

func TestSMTPConfigFromEnv(t *testing.T) {
	t.Run("absent variables leave all fields nil", func(t *testing.T) {
		clearSMTPEnv(t)

		cfg, err := SMTPConfigFromEnv()
		if err != nil {
			t.Fatalf("SMTPConfigFromEnv() failed: %v", err)
		}
		if !cfg.Empty() {
			t.Fatal("expected an empty config")
		}
	})

	t.Run("provided variables are captured", func(t *testing.T) {
		clearSMTPEnv(t)
		t.Setenv("PBVEX_SMTP_ENABLED", "true")
		t.Setenv("PBVEX_SMTP_HOST", "smtp.example.com")
		t.Setenv("PBVEX_SMTP_PORT", "465")
		t.Setenv("PBVEX_SMTP_PASSWORD", "secret")
		t.Setenv("PBVEX_SMTP_TLS", "false")

		cfg, err := SMTPConfigFromEnv()
		if err != nil {
			t.Fatalf("SMTPConfigFromEnv() failed: %v", err)
		}

		if cfg.Enabled == nil || !*cfg.Enabled {
			t.Fatalf("Enabled = %v, want true", cfg.Enabled)
		}
		if cfg.Host == nil || *cfg.Host != "smtp.example.com" {
			t.Fatalf("Host = %v, want smtp.example.com", cfg.Host)
		}
		if cfg.Port == nil || *cfg.Port != 465 {
			t.Fatalf("Port = %v, want 465", cfg.Port)
		}
		if cfg.Password == nil || *cfg.Password != "secret" {
			t.Fatalf("Password = %v, want secret", cfg.Password)
		}
		if cfg.TLS == nil || *cfg.TLS {
			t.Fatalf("TLS = %v, want false", cfg.TLS)
		}
		for key, value := range map[string]*string{
			"Username":      cfg.Username,
			"AuthMethod":    cfg.AuthMethod,
			"LocalName":     cfg.LocalName,
			"SenderAddress": cfg.SenderAddress,
			"SenderName":    cfg.SenderName,
		} {
			if value != nil {
				t.Fatalf("%s = %v, want nil", key, value)
			}
		}
	})

	t.Run("explicitly empty variable is present", func(t *testing.T) {
		clearSMTPEnv(t)
		t.Setenv("PBVEX_SMTP_USERNAME", "")

		cfg, err := SMTPConfigFromEnv()
		if err != nil {
			t.Fatalf("SMTPConfigFromEnv() failed: %v", err)
		}
		if cfg.Username == nil || *cfg.Username != "" {
			t.Fatalf("Username = %v, want an explicit empty string", cfg.Username)
		}
	})

	t.Run("invalid boolean fails", func(t *testing.T) {
		clearSMTPEnv(t)
		t.Setenv("PBVEX_SMTP_ENABLED", "yes")

		_, err := SMTPConfigFromEnv()
		if err == nil || !strings.Contains(err.Error(), "PBVEX_SMTP_ENABLED") {
			t.Fatalf("expected a PBVEX_SMTP_ENABLED error, got %v", err)
		}
	})

	t.Run("invalid port fails", func(t *testing.T) {
		clearSMTPEnv(t)
		t.Setenv("PBVEX_SMTP_PORT", "smtp")

		_, err := SMTPConfigFromEnv()
		if err == nil || !strings.Contains(err.Error(), "PBVEX_SMTP_PORT") {
			t.Fatalf("expected a PBVEX_SMTP_PORT error, got %v", err)
		}
	})

	t.Run("port out of range fails", func(t *testing.T) {
		clearSMTPEnv(t)
		t.Setenv("PBVEX_SMTP_PORT", "70000")

		_, err := SMTPConfigFromEnv()
		if err == nil || !strings.Contains(err.Error(), "PBVEX_SMTP_PORT") {
			t.Fatalf("expected a PBVEX_SMTP_PORT error, got %v", err)
		}
	})
}

func newSMTPTestApp(t *testing.T) *tests.TestApp {
	t.Helper()

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

// smtpSnapshot copies the observable fields without copying the settings
// mutex.
type smtpSnapshot struct {
	enabled       bool
	host          string
	port          int
	username      string
	password      string
	authMethod    string
	tls           bool
	localName     string
	senderAddress string
	senderName    string
}

func smtpSettingsSnapshot(s *core.Settings) smtpSnapshot {
	return smtpSnapshot{
		enabled:       s.SMTP.Enabled,
		host:          s.SMTP.Host,
		port:          s.SMTP.Port,
		username:      s.SMTP.Username,
		password:      s.SMTP.Password,
		authMethod:    s.SMTP.AuthMethod,
		tls:           s.SMTP.TLS,
		localName:     s.SMTP.LocalName,
		senderAddress: s.Meta.SenderAddress,
		senderName:    s.Meta.SenderName,
	}
}

func TestApplySMTPSettings(t *testing.T) {
	t.Run("empty config is a no-op", func(t *testing.T) {
		app := newSMTPTestApp(t)
		before := smtpSettingsSnapshot(app.Settings())

		if err := ApplySMTPSettings(app, SMTPConfig{}); err != nil {
			t.Fatalf("ApplySMTPSettings() failed: %v", err)
		}
		if after := smtpSettingsSnapshot(app.Settings()); after != before {
			t.Fatalf("settings changed: before %+v, after %+v", before, after)
		}
	})

	t.Run("provided fields are applied and persisted", func(t *testing.T) {
		app := newSMTPTestApp(t)
		cfg := SMTPConfig{
			Enabled:       boolPtr(true),
			Host:          strPtr("smtp.example.com"),
			Port:          intPtr(465),
			Username:      strPtr("no-reply@example.com"),
			Password:      strPtr("secret"),
			AuthMethod:    strPtr("login"),
			TLS:           boolPtr(true),
			LocalName:     strPtr("example.com"),
			SenderAddress: strPtr("no-reply@example.com"),
			SenderName:    strPtr("Example"),
		}

		if err := ApplySMTPSettings(app, cfg); err != nil {
			t.Fatalf("ApplySMTPSettings() failed: %v", err)
		}

		got := app.Settings()
		if !got.SMTP.Enabled || got.SMTP.Host != "smtp.example.com" || got.SMTP.Port != 465 {
			t.Fatalf("SMTP = %+v, want the configured host settings", got.SMTP)
		}
		if got.SMTP.Username != "no-reply@example.com" || got.SMTP.Password != "secret" {
			t.Fatalf("SMTP credentials = %q/%q, want the configured values", got.SMTP.Username, got.SMTP.Password)
		}
		if got.SMTP.AuthMethod != "LOGIN" {
			t.Fatalf("AuthMethod = %q, want normalized LOGIN", got.SMTP.AuthMethod)
		}
		if !got.SMTP.TLS || got.SMTP.LocalName != "example.com" {
			t.Fatalf("TLS/LocalName = %v/%q, want true/example.com", got.SMTP.TLS, got.SMTP.LocalName)
		}
		if got.Meta.SenderAddress != "no-reply@example.com" || got.Meta.SenderName != "Example" {
			t.Fatalf("Meta sender = %q/%q, want the configured values", got.Meta.SenderAddress, got.Meta.SenderName)
		}

		// Prove the values were persisted by sabotaging the in-memory copy
		// and reloading it from the database.
		got.SMTP.Host = "tampered.example.com"
		if err := app.ReloadSettings(); err != nil {
			t.Fatalf("ReloadSettings() failed: %v", err)
		}
		if host := app.Settings().SMTP.Host; host != "smtp.example.com" {
			t.Fatalf("persisted Host = %q, want smtp.example.com", host)
		}
	})

	t.Run("absent fields keep the dashboard-managed values", func(t *testing.T) {
		app := newSMTPTestApp(t)
		before := smtpSettingsSnapshot(app.Settings())

		if err := ApplySMTPSettings(app, SMTPConfig{Host: strPtr("smtp.example.com")}); err != nil {
			t.Fatalf("ApplySMTPSettings() failed: %v", err)
		}

		after := smtpSettingsSnapshot(app.Settings())
		if after.host != "smtp.example.com" {
			t.Fatalf("Host = %q, want the override", after.host)
		}
		if after.enabled != before.enabled || after.port != before.port ||
			after.username != before.username || after.password != before.password ||
			after.senderAddress != before.senderAddress || after.senderName != before.senderName {
			t.Fatalf("absent fields changed: before %+v, after %+v", before, after)
		}
	})

	t.Run("invalid combination fails validation", func(t *testing.T) {
		app := newSMTPTestApp(t)
		cfg := SMTPConfig{Enabled: boolPtr(true), Host: strPtr("not a host")}

		err := ApplySMTPSettings(app, cfg)
		if err == nil || !strings.Contains(err.Error(), "PBVEX_SMTP_*") {
			t.Fatalf("expected a validation error, got %v", err)
		}
	})
}

func TestRegisterCoreBindsSMTPSettings(t *testing.T) {
	newApp := func(t *testing.T) *tests.TestApp {
		t.Helper()
		app, err := tests.NewTestApp()
		if err != nil {
			t.Fatalf("failed to create test app: %v", err)
		}
		t.Cleanup(app.Cleanup)
		return app
	}

	appPlain := newApp(t)
	if _, _, err := RegisterCore(appPlain, DefaultConfig()); err != nil {
		t.Fatalf("RegisterCore() failed: %v", err)
	}

	cfg := DefaultConfig()
	cfg.SMTP = SMTPConfig{Host: strPtr("smtp.example.com")}
	appSMTP := newApp(t)
	if _, _, err := RegisterCore(appSMTP, cfg); err != nil {
		t.Fatalf("RegisterCore() failed: %v", err)
	}

	plain := appPlain.OnBootstrap().Length()
	withSMTP := appSMTP.OnBootstrap().Length()
	if withSMTP != plain+1 {
		t.Fatalf("OnBootstrap handlers = %d with SMTP config, want %d (+1) with plain config", withSMTP, plain+1)
	}
}
