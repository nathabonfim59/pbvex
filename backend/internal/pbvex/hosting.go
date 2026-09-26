package pbvex

import (
	"context"
	"reflect"

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

// registerHosting installs the administrative capability gates. Administrative
// PocketBase operations stay check-gated only: they are never admitted or
// reported as protocol events.
func registerHosting(app core.App, client *hosting.Client) error {
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
	app.OnSettingsUpdateRequest().Bind(&hook.Handler[*core.SettingsUpdateRequestEvent]{Id: "pbvexHostingSettings", Priority: -1000, Func: func(e *core.SettingsUpdateRequestEvent) error {
		for _, change := range []struct {
			changed    bool
			capability string
		}{
			{!reflect.DeepEqual(e.OldSettings.S3, e.NewSettings.S3), hosting.SettingsStorage},
			{!reflect.DeepEqual(e.OldSettings.Backups, e.NewSettings.Backups), hosting.SettingsBackups},
			{!reflect.DeepEqual(e.OldSettings.SMTP, e.NewSettings.SMTP), hosting.SettingsSMTP},
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
		return router.NewForbiddenError("Backup restore is unavailable with hosting integration enabled.", nil)
	}})
	return nil
}
