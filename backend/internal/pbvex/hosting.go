package pbvex

import (
	"context"
	"reflect"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

func registerHosting(app core.App, cfg hosting.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	client, err := hosting.NewClient(cfg)
	if err != nil {
		return err
	}
	if _, err = client.Handshake(context.Background()); err != nil {
		client.Close()
		return err
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
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { client.Close(); return e.Next() })
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
