package storage

import (
	"fmt"
	"mime"
	"net/url"
	"strings"
	"time"
)

// Config controls storage service behavior and constraints.
type Config struct {
	// MaxFileSize is the hard upper bound for a single file upload.
	MaxFileSize int64
	// DefaultUploadTTL is the default expiry for generated upload URLs.
	DefaultUploadTTL time.Duration
	// DefaultClaimTTL is the maximum time an upload attempt may hold a token claim.
	DefaultClaimTTL time.Duration
	// AllowedContentTypes is a list of allowed MIME type patterns. Empty allows all.
	// Each pattern is either an exact type (e.g. "image/png") or a wildcard suffix (e.g. "image/*").
	AllowedContentTypes []string
	// BasePath is the base API path used when building URLs.
	BasePath string
	// BaseURL is the absolute base URL for generated storage URLs. If empty, falls back to AppURL.
	BaseURL string
	// FileStoragePrefix is the object-key prefix used by the filesystem backend.
	FileStoragePrefix string
	// DefaultTokenMaxSize overrides MaxFileSize per token if non-zero.
	DefaultTokenMaxSize int64
	// CleanupInterval is the interval between background cleanup worker passes.
	CleanupInterval time.Duration
	// URLSigningTTL is the default lifetime for signed download URLs.
	URLSigningTTL time.Duration
	// URLSigningMaxTTL is the absolute maximum lifetime a signed URL may request.
	URLSigningMaxTTL time.Duration
	// KeyRotationInterval controls how often signing keys are rotated.
	KeyRotationInterval time.Duration
	// KeyGracePeriod is how long rotated-out keys stay available for verification.
	KeyGracePeriod time.Duration
	// MaxFiles is the maximum number of active stored files. 0 means unlimited.
	MaxFiles int64
	// UploadLeaseInterval is the validity window of an uploading reservation
	// lease. Active uploads renew it periodically; cleanup reclaims a
	// reservation only once its lease expires without renewal.
	UploadLeaseInterval time.Duration
}

// DefaultConfig returns sane storage defaults.
func DefaultConfig() Config {
	return Config{
		MaxFileSize:         64 << 20, // 64 MiB
		DefaultUploadTTL:    time.Hour,
		DefaultClaimTTL:     5 * time.Minute,
		AllowedContentTypes: nil,
		BasePath:            "/api/pbvex/storage",
		BaseURL:             "",
		FileStoragePrefix:   "storage",
		DefaultTokenMaxSize: 0,
		CleanupInterval:     5 * time.Minute,
		URLSigningTTL:       15 * time.Minute,
		URLSigningMaxTTL:    24 * time.Hour,
		KeyRotationInterval: 24 * time.Hour,
		KeyGracePeriod:      25 * time.Hour,
		MaxFiles:            0,
		UploadLeaseInterval: 30 * time.Second,
	}
}

// NormalizeConfig fills missing fields with defaults and validates values.
func NormalizeConfig(cfg Config) (Config, error) {
	if err := validateConfigInput(cfg); err != nil {
		return DefaultConfig(), err
	}

	out := DefaultConfig()
	if cfg.MaxFileSize > 0 {
		out.MaxFileSize = cfg.MaxFileSize
	}
	if cfg.DefaultUploadTTL > 0 {
		out.DefaultUploadTTL = cfg.DefaultUploadTTL
	}
	if cfg.DefaultClaimTTL > 0 {
		out.DefaultClaimTTL = cfg.DefaultClaimTTL
	}
	if cfg.BasePath != "" {
		out.BasePath = cfg.BasePath
	}
	if cfg.BaseURL != "" {
		out.BaseURL = cfg.BaseURL
	}
	if cfg.FileStoragePrefix != "" {
		out.FileStoragePrefix = cfg.FileStoragePrefix
	}
	out.AllowedContentTypes = cfg.AllowedContentTypes
	if cfg.DefaultTokenMaxSize > 0 {
		out.DefaultTokenMaxSize = cfg.DefaultTokenMaxSize
		if out.DefaultTokenMaxSize > out.MaxFileSize {
			out.DefaultTokenMaxSize = out.MaxFileSize
		}
	}
	if cfg.CleanupInterval > 0 {
		out.CleanupInterval = cfg.CleanupInterval
	}
	if cfg.URLSigningTTL > 0 {
		out.URLSigningTTL = cfg.URLSigningTTL
	}
	if cfg.URLSigningMaxTTL > 0 {
		out.URLSigningMaxTTL = cfg.URLSigningMaxTTL
	}
	if cfg.KeyRotationInterval > 0 {
		out.KeyRotationInterval = cfg.KeyRotationInterval
	}
	if cfg.KeyGracePeriod > 0 {
		out.KeyGracePeriod = cfg.KeyGracePeriod
	}
	if cfg.MaxFiles > 0 {
		out.MaxFiles = cfg.MaxFiles
	}
	if cfg.UploadLeaseInterval > 0 {
		out.UploadLeaseInterval = cfg.UploadLeaseInterval
	}

	if err := validateContentTypes(out.AllowedContentTypes); err != nil {
		return out, err
	}
	if out.CleanupInterval < time.Minute {
		out.CleanupInterval = time.Minute
	}
	if out.URLSigningTTL < time.Second {
		out.URLSigningTTL = time.Second
	}
	if out.URLSigningMaxTTL < out.URLSigningTTL {
		out.URLSigningMaxTTL = out.URLSigningTTL
	}
	if out.KeyRotationInterval < time.Minute {
		out.KeyRotationInterval = time.Minute
	}
	if out.KeyGracePeriod < out.URLSigningMaxTTL+time.Minute {
		out.KeyGracePeriod = out.URLSigningMaxTTL + time.Minute
	}

	if out.BaseURL != "" {
		u, err := url.Parse(out.BaseURL)
		if err != nil {
			return out, fmt.Errorf("invalid base url %q: %w", out.BaseURL, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return out, fmt.Errorf("base url must use http or https, got %q", u.Scheme)
		}
		if u.Host == "" {
			return out, fmt.Errorf("base url must have a host")
		}
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
			return out, fmt.Errorf("base url must not contain credentials, query, fragment, opaque, or encoded path data")
		}
		if err := validateBaseURLPath(u.Path); err != nil {
			return out, err
		}
		out.BaseURL = strings.TrimRight(out.BaseURL, "/")
	}
	if err := validateBasePath(out.BasePath); err != nil {
		return out, err
	}
	if err := validateFileStoragePrefix(out.FileStoragePrefix); err != nil {
		return out, err
	}

	return out, nil
}

// validateConfigInput rejects explicitly invalid settings (negatives, empty
// patterns, malformed base path) before they are silently replaced by defaults.
// Zero values are treated as "unset" and kept as defaults; negative values are
// unambiguous errors.
func validateConfigInput(cfg Config) error {
	if cfg.MaxFileSize < 0 {
		return fmt.Errorf("maxFileSize must be non-negative")
	}
	if cfg.MaxFiles < 0 {
		return fmt.Errorf("maxFiles must be non-negative")
	}
	if cfg.DefaultTokenMaxSize < 0 {
		return fmt.Errorf("tokenMaxSize must be non-negative")
	}
	if cfg.DefaultUploadTTL < 0 || cfg.DefaultClaimTTL < 0 || cfg.CleanupInterval < 0 ||
		cfg.URLSigningTTL < 0 || cfg.URLSigningMaxTTL < 0 ||
		cfg.KeyRotationInterval < 0 || cfg.KeyGracePeriod < 0 || cfg.UploadLeaseInterval < 0 {
		return fmt.Errorf("storage durations must be non-negative")
	}
	if cfg.UploadLeaseInterval > 0 && cfg.UploadLeaseInterval < 2*time.Second {
		return fmt.Errorf("uploadLeaseInterval must be at least 2s when explicitly set")
	}
	if cfg.BasePath != "" {
		if err := validateBasePath(cfg.BasePath); err != nil {
			return err
		}
	}
	if cfg.FileStoragePrefix != "" {
		if err := validateFileStoragePrefix(cfg.FileStoragePrefix); err != nil {
			return err
		}
	}
	if err := validateContentTypes(cfg.AllowedContentTypes); err != nil {
		return err
	}
	return nil
}

// validateBasePath enforces a clean absolute route prefix. Custom storage routes
// must live outside PocketBase's /api namespace; the canonical default is the
// sole exception. This prevents exact, ancestor, and descendant collisions with
// PBVex platform routes and leaves room for their parameterized descendants.
func validateBasePath(basePath string) error {
	if !strings.HasPrefix(basePath, "/") {
		return fmt.Errorf("base path must be absolute (start with /), got %q", basePath)
	}
	if basePath == "/" {
		return fmt.Errorf("base path must not be the root")
	}
	if strings.HasSuffix(basePath, "/") {
		return fmt.Errorf("base path must not have a trailing slash: %q", basePath)
	}
	for _, ch := range basePath {
		switch {
		case ch == '/':
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case strings.ContainsRune("-._~!$&'()*+,;=:@", ch):
		default:
			return fmt.Errorf("base path contains disallowed character %q in %q", ch, basePath)
		}
	}
	for _, segment := range strings.Split(strings.TrimPrefix(basePath, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("base path contains an invalid segment: %q", basePath)
		}
	}
	if basePath == DefaultConfig().BasePath {
		return nil
	}
	for _, reserved := range []string{
		"/api",
		"/api/pbvex",
		"/api/pbvex/call",
		"/api/pbvex/realtime",
		"/api/pbvex/deployments",
		"/api/pbvex/jobs",
		DefaultConfig().BasePath,
	} {
		if routePrefixesOverlap(basePath, reserved) {
			return fmt.Errorf("base path collides with reserved platform route %q", reserved)
		}
	}
	return nil
}

func routePrefixesOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func validateBaseURLPath(value string) error {
	if value == "" || value == "/" {
		return nil
	}
	if !strings.HasPrefix(value, "/") || strings.Contains(value, "//") || strings.Contains(value, "\\") {
		return fmt.Errorf("base url path must be a clean absolute path")
	}
	for _, segment := range strings.Split(strings.Trim(value, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("base url path contains an invalid segment")
		}
		for _, ch := range segment {
			if !isStoragePathChar(ch) {
				return fmt.Errorf("base url path contains disallowed character %q", ch)
			}
		}
	}
	return nil
}

func validateFileStoragePrefix(prefix string) error {
	if prefix == "" || strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") || strings.Contains(prefix, "\\") {
		return fmt.Errorf("file storage prefix must be a non-empty relative path")
	}
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("file storage prefix contains an invalid segment")
		}
		for _, ch := range segment {
			if !isStoragePathChar(ch) {
				return fmt.Errorf("file storage prefix contains disallowed character %q", ch)
			}
		}
	}
	return nil
}

func isStoragePathChar(ch rune) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-._~", ch)
}

// validateContentTypes ensures patterns are exact MIME types or trailing "/*" wildcards.
func validateContentTypes(patterns []string) error {
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			return fmt.Errorf("empty content type pattern")
		}
		if p == "*" {
			continue
		}
		if strings.Contains(p, "*") {
			// Only allow a single trailing wildcard, e.g. "image/*".
			if !strings.HasSuffix(p, "/*") || strings.Count(p, "*") != 1 {
				return fmt.Errorf("invalid content type wildcard %q", p)
			}
			prefix := strings.TrimSuffix(p, "/*")
			if prefix == "" || strings.Contains(prefix, "/") {
				return fmt.Errorf("invalid content type wildcard %q", p)
			}
			continue
		}
		_, _, err := mime.ParseMediaType(p)
		if err != nil {
			return fmt.Errorf("invalid content type %q: %w", p, err)
		}
	}
	return nil
}
