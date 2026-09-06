package pbvex

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// SMTPConfig carries optional PBVEX_SMTP_* overrides for PocketBase's mail
// settings. Every field is a pointer: nil means the variable was not
// provided and the dashboard-managed setting is left untouched. Provided
// fields are applied to the app settings and persisted on every bootstrap,
// so the environment wins on boot for the fields it declares. Removing a
// variable does not revert the previously persisted value; edit it in the
// dashboard (Settings > Mail settings) to unbind it from the environment.
type SMTPConfig struct {
	Enabled       *bool
	Host          *string
	Port          *int
	Username      *string
	Password      *string
	AuthMethod    *string // PLAIN (default) or LOGIN; compared case-insensitively
	TLS           *bool
	LocalName     *string
	SenderAddress *string // applied to Settings().Meta.SenderAddress
	SenderName    *string // applied to Settings().Meta.SenderName
}

// Empty reports whether no SMTP variable was provided.
func (c SMTPConfig) Empty() bool {
	return c.Enabled == nil && c.Host == nil && c.Port == nil &&
		c.Username == nil && c.Password == nil && c.AuthMethod == nil &&
		c.TLS == nil && c.LocalName == nil && c.SenderAddress == nil &&
		c.SenderName == nil
}

// SMTPConfigFromEnv builds a SMTPConfig from the PBVEX_SMTP_* process
// environment. Absent variables leave the matching field nil; a variable
// explicitly set to an empty string is considered present. Malformed
// booleans or ports return an error so misconfiguration fails startup
// instead of being silently ignored.
//
// These variables configure the server process itself. They are unrelated
// to component envVar bindings, are never exposed to deployed functions,
// and intentionally have no command-line flags so credentials cannot leak
// through argv or shell history.
func SMTPConfigFromEnv() (SMTPConfig, error) {
	enabled, err := envBoolPtr("PBVEX_SMTP_ENABLED")
	if err != nil {
		return SMTPConfig{}, err
	}

	port, err := envPortPtr("PBVEX_SMTP_PORT")
	if err != nil {
		return SMTPConfig{}, err
	}

	enforceTLS, err := envBoolPtr("PBVEX_SMTP_TLS")
	if err != nil {
		return SMTPConfig{}, err
	}

	return SMTPConfig{
		Enabled:       enabled,
		Host:          envStringPtr("PBVEX_SMTP_HOST"),
		Port:          port,
		Username:      envStringPtr("PBVEX_SMTP_USERNAME"),
		Password:      envStringPtr("PBVEX_SMTP_PASSWORD"),
		AuthMethod:    envStringPtr("PBVEX_SMTP_AUTH_METHOD"),
		TLS:           enforceTLS,
		LocalName:     envStringPtr("PBVEX_SMTP_LOCAL_NAME"),
		SenderAddress: envStringPtr("PBVEX_SMTP_SENDER_ADDRESS"),
		SenderName:    envStringPtr("PBVEX_SMTP_SENDER_NAME"),
	}, nil
}

// ApplyTo writes the provided fields onto the app settings and reports
// whether any value changed. Fields without an override are untouched, so
// dashboard-managed values for absent variables survive restarts.
func (c SMTPConfig) ApplyTo(s *core.Settings) bool {
	changed := false
	if c.Enabled != nil && s.SMTP.Enabled != *c.Enabled {
		s.SMTP.Enabled = *c.Enabled
		changed = true
	}
	if c.Host != nil && s.SMTP.Host != *c.Host {
		s.SMTP.Host = *c.Host
		changed = true
	}
	if c.Port != nil && s.SMTP.Port != *c.Port {
		s.SMTP.Port = *c.Port
		changed = true
	}
	if c.Username != nil && s.SMTP.Username != *c.Username {
		s.SMTP.Username = *c.Username
		changed = true
	}
	if c.Password != nil && s.SMTP.Password != *c.Password {
		s.SMTP.Password = *c.Password
		changed = true
	}
	if c.AuthMethod != nil {
		auth := strings.ToUpper(strings.TrimSpace(*c.AuthMethod))
		if s.SMTP.AuthMethod != auth {
			s.SMTP.AuthMethod = auth
			changed = true
		}
	}
	if c.TLS != nil && s.SMTP.TLS != *c.TLS {
		s.SMTP.TLS = *c.TLS
		changed = true
	}
	if c.LocalName != nil && s.SMTP.LocalName != *c.LocalName {
		s.SMTP.LocalName = *c.LocalName
		changed = true
	}
	if c.SenderAddress != nil && s.Meta.SenderAddress != *c.SenderAddress {
		s.Meta.SenderAddress = *c.SenderAddress
		changed = true
	}
	if c.SenderName != nil && s.Meta.SenderName != *c.SenderName {
		s.Meta.SenderName = *c.SenderName
		changed = true
	}
	return changed
}

// ApplySMTPSettings writes the PBVEX_SMTP_* overrides onto the app mail
// settings and persists them. It is a no-op when no variable is provided or
// when every provided value already matches the persisted settings. Saving
// runs PocketBase's settings validation, so an invalid combination (for
// example enabling SMTP without a host) fails the bootstrap.
func ApplySMTPSettings(app core.App, cfg SMTPConfig) error {
	if cfg.Empty() {
		return nil
	}

	settings := app.Settings()
	if !cfg.ApplyTo(settings) {
		return nil
	}

	if err := app.Save(settings); err != nil {
		return fmt.Errorf("apply PBVEX_SMTP_* mail settings: %w", err)
	}
	return nil
}

func envStringPtr(key string) *string {
	if v, ok := os.LookupEnv(key); ok {
		return &v
	}
	return nil
}

func envBoolPtr(key string) (*bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return nil, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid boolean for %s=%q", key, raw)
	}
	return &v, nil
}

func envPortPtr(key string) (*int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid port for %s=%q", key, raw)
	}
	if n < 0 || n > 65535 {
		return nil, fmt.Errorf("port for %s=%q out of range (0-65535)", key, raw)
	}
	return &n, nil
}
