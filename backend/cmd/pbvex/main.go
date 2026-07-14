package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/pbvex"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/tools/osutils"
)

func main() {
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: "./pb_data",
		DefaultDev:     osutils.IsProbablyGoRun(),
	})

	cfg := pbvex.DefaultConfig()

	app.RootCmd.PersistentFlags().StringVar(
		&cfg.HooksDir,
		"hooksDir",
		"",
		"the directory with the JS app hooks",
	)
	app.RootCmd.PersistentFlags().BoolVar(
		&cfg.HooksWatch,
		"hooksWatch",
		true,
		"auto restart the app on pb_hooks file change",
	)
	app.RootCmd.PersistentFlags().IntVar(
		&cfg.HooksPool,
		"hooksPool",
		15,
		"the total prewarm goja.Runtime instances for the JS app hooks execution",
	)
	app.RootCmd.PersistentFlags().StringVar(
		&cfg.MigrationsDir,
		"migrationsDir",
		"",
		"the directory with the user defined migrations",
	)
	app.RootCmd.PersistentFlags().BoolVar(
		&cfg.Automigrate,
		"automigrate",
		true,
		"enable/disable auto migrations",
	)
	app.RootCmd.PersistentFlags().StringVar(
		&cfg.PublicDir,
		"publicDir",
		cfg.PublicDir,
		"the directory to serve static files",
	)
	app.RootCmd.PersistentFlags().BoolVar(
		&cfg.IndexFallback,
		"indexFallback",
		true,
		"fallback the request to index.html on missing static path",
	)

	// Storage settings. Each flag defaults to the matching PBVEX_STORAGE_*
	// environment variable when set, so deployments can configure the file
	// backend without modifying command-line invocations.
	storageAllowedTypes := envString("PBVEX_STORAGE_ALLOWED_TYPES", "")
	app.RootCmd.PersistentFlags().StringVar(
		&storageAllowedTypes,
		"storageAllowedTypes",
		storageAllowedTypes,
		"comma-separated allowed MIME patterns for uploads (empty = allow all)",
	)

	cfg.Storage.MaxFileSize = envInt64("PBVEX_STORAGE_MAX_FILE_SIZE", cfg.Storage.MaxFileSize)
	app.RootCmd.PersistentFlags().Int64Var(
		&cfg.Storage.MaxFileSize,
		"storageMaxFileSize",
		cfg.Storage.MaxFileSize,
		"hard upper bound in bytes for a single file upload",
	)

	cfg.Storage.DefaultTokenMaxSize = envInt64("PBVEX_STORAGE_TOKEN_MAX_SIZE", cfg.Storage.DefaultTokenMaxSize)
	app.RootCmd.PersistentFlags().Int64Var(
		&cfg.Storage.DefaultTokenMaxSize,
		"storageTokenMaxSize",
		cfg.Storage.DefaultTokenMaxSize,
		"default per-token max upload size in bytes (clamped to storageMaxFileSize)",
	)

	cfg.Storage.MaxFiles = envInt64("PBVEX_STORAGE_MAX_FILES", cfg.Storage.MaxFiles)
	app.RootCmd.PersistentFlags().Int64Var(
		&cfg.Storage.MaxFiles,
		"storageMaxFiles",
		cfg.Storage.MaxFiles,
		"maximum number of stored files (0 = unlimited)",
	)

	cfg.Storage.DefaultUploadTTL = envDuration("PBVEX_STORAGE_UPLOAD_TTL", cfg.Storage.DefaultUploadTTL)
	app.RootCmd.PersistentFlags().DurationVar(
		&cfg.Storage.DefaultUploadTTL,
		"storageUploadTtl",
		cfg.Storage.DefaultUploadTTL,
		"time-to-live for generated upload URLs",
	)

	cfg.Storage.URLSigningTTL = envDuration("PBVEX_STORAGE_URL_SIGN_TTL", cfg.Storage.URLSigningTTL)
	app.RootCmd.PersistentFlags().DurationVar(
		&cfg.Storage.URLSigningTTL,
		"storageUrlSignTtl",
		cfg.Storage.URLSigningTTL,
		"default lifetime for signed download URLs",
	)

	cfg.Storage.CleanupInterval = envDuration("PBVEX_STORAGE_CLEANUP_INTERVAL", cfg.Storage.CleanupInterval)
	app.RootCmd.PersistentFlags().DurationVar(
		&cfg.Storage.CleanupInterval,
		"storageCleanupInterval",
		cfg.Storage.CleanupInterval,
		"interval between background cleanup worker passes",
	)

	cfg.Storage.BaseURL = envString("PBVEX_STORAGE_BASE_URL", cfg.Storage.BaseURL)
	app.RootCmd.PersistentFlags().StringVar(
		&cfg.Storage.BaseURL,
		"storageBaseUrl",
		cfg.Storage.BaseURL,
		"absolute base URL for generated storage URLs (defaults to AppURL)",
	)

	cfg.Storage.BasePath = envString("PBVEX_STORAGE_BASE_PATH", cfg.Storage.BasePath)
	app.RootCmd.PersistentFlags().StringVar(
		&cfg.Storage.BasePath,
		"storageBasePath",
		cfg.Storage.BasePath,
		"API base path used when building storage URLs",
	)

	cfg.Storage.FileStoragePrefix = envString("PBVEX_STORAGE_FILE_PREFIX", cfg.Storage.FileStoragePrefix)
	app.RootCmd.PersistentFlags().StringVar(
		&cfg.Storage.FileStoragePrefix,
		"storageFilePrefix",
		cfg.Storage.FileStoragePrefix,
		"object-key prefix used by the filesystem backend",
	)

	app.RootCmd.ParseFlags(os.Args[1:])

	cfg.Storage.AllowedContentTypes = parseAllowedContentTypes(storageAllowedTypes)

	if err := pbvex.Register(app, cfg); err != nil {
		log.Fatal(err)
	}

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

// parseAllowedContentTypes splits a CSV allowed-types string. An empty input
// means "allow all" (nil). Non-empty inputs are split and trimmed but empty
// entries are preserved so that NormalizeConfig can reject them rather than
// silently dropping them (fail closed).
func parseAllowedContentTypes(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt64(key string, def int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		log.Fatalf("invalid integer for %s=%q: %v", key, v, err)
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Fatalf("invalid duration for %s=%q: %v", key, v, err)
	}
	return d
}
