package pbvex

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

// newHostingClient validates the bootstrap configuration and performs the
// mandatory startup handshake. A disabled configuration returns (nil, nil)
// without connecting to any provider. The client is created once per
// application and shared by the administrative gates and the runtime
// execution observer.
func newHostingClient(cfg hosting.Config) (*hosting.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	client, err := hosting.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	if _, err = client.Handshake(context.Background()); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// registerHosting installs the administrative capability gates and the
// hosted-mode native path restrictions. Administrative PocketBase operations
// stay check-gated only: they are never admitted or reported as protocol
// events.
func registerHosting(app core.App, client *hosting.Client, managed *managedInjection) error {
	if client == nil {
		return nil
	}
	require := func(ctx context.Context, capability string) error {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := client.Require(ctx, capability); err != nil {
			return router.NewForbiddenError("Operation restricted by hosting policy.", nil)
		}
		return nil
	}
	forbidden := func(message string) error {
		return router.NewForbiddenError(message, nil)
	}

	// Host-managed categories are deployment configuration, not dynamic
	// provider policy: no capability grant unlocks them. Only their
	// host-configured value is accepted, and it is replaced by the neutral
	// persisted baseline before anything reaches the database.
	lockSettings := func(e *core.SettingsUpdateRequestEvent) error {
		if managed.storageManaged() {
			if !reflect.DeepEqual(e.OldSettings.S3, e.NewSettings.S3) {
				return forbidden("The file storage configuration is managed by the hosting platform.")
			}
			if !reflect.DeepEqual(e.OldSettings.Backups.S3, e.NewSettings.Backups.S3) {
				return forbidden("The backup storage configuration is managed by the hosting platform.")
			}
		}
		if managed.smtpManaged() {
			if !reflect.DeepEqual(e.OldSettings.SMTP, e.NewSettings.SMTP) {
				return forbidden("The mail settings are managed by the hosting platform.")
			}
		}
		if err := managed.effectivePersisted(e.NewSettings); err != nil {
			return err
		}
		return nil
	}

	app.OnSettingsUpdateRequest().Bind(&hook.Handler[*core.SettingsUpdateRequestEvent]{Id: "pbvexHostingSettings", Priority: -1000, Func: func(e *core.SettingsUpdateRequestEvent) error {
		if err := lockSettings(e); err != nil {
			return err
		}
		for _, change := range []struct {
			changed    bool
			capability string
		}{
			// Managed categories were accepted above only when unchanged
			// relative to the persisted baseline, so they can no longer
			// differ here and need no provider check.
			{!managed.storageManaged() && !reflect.DeepEqual(e.OldSettings.S3, e.NewSettings.S3), hosting.SettingsStorage},
			{!reflect.DeepEqual(effectiveBackups(managed, e.OldSettings), effectiveBackups(managed, e.NewSettings)), hosting.SettingsBackups},
			{!managed.smtpManaged() && !reflect.DeepEqual(e.OldSettings.SMTP, e.NewSettings.SMTP), hosting.SettingsSMTP},
		} {
			if change.changed {
				if err := require(e.Request.Context(), change.capability); err != nil {
					return err
				}
			}
		}
		return e.Next()
	}})

	app.OnBackupCreate().Bind(&hook.Handler[*core.BackupEvent]{Id: "pbvexHostingBackup", Priority: -1000, Func: func(e *core.BackupEvent) error {
		if err := require(e.Context, hosting.BackupCreate); err != nil {
			return err
		}
		return e.Next()
	}})
	// Restores may replace settings and introduce executable files. Until those
	// paths are independently constrained, even a provider allow cannot enable it.
	app.OnBackupRestore().Bind(&hook.Handler[*core.BackupEvent]{Id: "pbvexHostingRestore", Priority: -1000, Func: func(e *core.BackupEvent) error {
		return forbidden("Backup restore is unavailable with hosting integration enabled.")
	}})

	// Collection import replaces collection definitions in one transaction
	// without the per-model validations of individual saves, so it can
	// rewrite system collections that the record-level protections rely on.
	// Hosted tenants change their schema through deployments instead; the
	// denial runs before any import side effect.
	app.OnCollectionsImportRequest().Bind(&hook.Handler[*core.CollectionsImportRequestEvent]{Id: "pbvexHostingCollectionsImport", Priority: -1000, Func: func(e *core.CollectionsImportRequestEvent) error {
		return forbidden("Collection import is unavailable with hosting integration enabled.")
	}})

	// Gate the native routes that have no dedicated PocketBase hook in
	// v0.40.1. Bindings made during OnServe are baked into the mux when
	// OnServe completes, so this middleware covers the built-in routes.
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "pbvexHostingNativeGates", Priority: -1000, Func: func(e *core.ServeEvent) error {
		e.Router.Bind(&hook.Handler[*core.RequestEvent]{Id: "pbvexHostingNativePathGates", Priority: -1000, Func: func(e *core.RequestEvent) error {
			path := strings.TrimSuffix(e.Request.URL.Path, "/")
			method := e.Request.Method
			backupKey := strings.TrimPrefix(path, "/api/backups/")
			switch {
			case path == "/api/sql" && method == http.MethodPost:
				// Arbitrary SQL bypasses every record hook and can read the
				// persisted settings row or rewrite system state.
				return forbidden("Direct SQL execution is unavailable with hosting integration enabled.")
			case path == "/api/collections/import" && method == http.MethodPost,
				path == "/api/backups/upload" && method == http.MethodPost:
				return forbidden("Operation restricted by hosting policy.")
			case strings.HasPrefix(path, "/api/backups/") && method == http.MethodPost &&
				strings.HasSuffix(backupKey, "/restore"):
				// Deny before the restore is scheduled so the caller receives
				// the refusal instead of an optimistic success response.
				return forbidden("Backup restore is unavailable with hosting integration enabled.")
			case strings.HasPrefix(path, "/api/backups/") && backupKey != "" && !strings.Contains(backupKey, "/") &&
				(method == http.MethodGet || method == http.MethodHead):
				// Backup archives embed the tenant database. Downloads stay
				// provider-gated even though managed-mode archives no longer
				// contain host secrets.
				if err := require(e.Request.Context(), hosting.BackupDownload); err != nil {
					return err
				}
			case path == "/api/settings/test/s3" && method == http.MethodPost && managed.storageManaged():
				// The connection test would exercise the host-owned storage
				// credentials on behalf of the tenant.
				return forbidden("The file storage configuration is managed by the hosting platform.")
			case path == "/api/settings/test/email" && method == http.MethodPost && managed.smtpManaged():
				// The mail test would send through the host-owned SMTP
				// credentials to an arbitrary recipient.
				return forbidden("The mail settings are managed by the hosting platform.")
			}
			return e.Next()
		}})
		return e.Next()
	}})

	return nil
}

// effectiveBackups returns the backups category with the managed S3 part
// replaced by the neutral baseline, so the backups capability only tracks the
// fields a tenant can actually change (the cron schedule and retention).
func effectiveBackups(managed *managedInjection, s *core.Settings) core.BackupsConfig {
	backups := s.Backups
	if managed.storageManaged() {
		backups.S3 = core.S3Config{}
	}
	return backups
}
