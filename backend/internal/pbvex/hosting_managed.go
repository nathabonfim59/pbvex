package pbvex

import (
	"log/slog"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// paramsTable mirrors the PocketBase internal settings table. The app
// settings are persisted as a single row in this table.
const paramsTable = "_params"

// managedInjection carries the host-owned configuration that is injected into
// the running application without being persisted in the tenant database.
//
// The injection relies on one PocketBase property inspected in v0.40.1: the
// mail client (core.BaseApp.NewMailClient) and the file/backups filesystems
// (NewFilesystem/NewBackupsFilesystem) read the in-memory app settings on
// every use. The host values are therefore applied to the in-memory settings
// after every load, while the persisted settings row keeps neutral (disabled)
// values for the managed categories. Net effect: native storage and mail use
// the host configuration, and neither the values nor their secrets exist in
// the tenant database, in backup archives, or in masked settings responses.
//
// A nil *managedInjection means nothing is host-managed and every hook below
// is skipped.
type managedInjection struct {
	storage core.S3Config
	smtp    SMTPConfig
}

func (m *managedInjection) storageManaged() bool { return m != nil && m.storage.Enabled }
func (m *managedInjection) smtpManaged() bool    { return m != nil && !m.smtp.Empty() }

// registerManagedInjection returns a nil injection when the configuration
// manages neither storage nor mail.
func registerManagedInjection(storage core.S3Config, smtp SMTPConfig) *managedInjection {
	m := &managedInjection{storage: storage, smtp: smtp}
	if !m.storageManaged() && !m.smtpManaged() {
		return nil
	}
	return m
}

// register installs the load-time shadow, the persistence neutralization, and
// the bootstrap baseline hooks. It must run before the app bootstraps so the
// first settings load is already shadowed.
func (m *managedInjection) register(app core.App) error {
	app.OnSettingsReload().Bind(&hook.Handler[*core.SettingsReloadEvent]{
		Id:       "pbvexHostingManagedShadow",
		Priority: -100,
		Func: func(e *core.SettingsReloadEvent) error {
			// e.Next() loads the persisted settings; the shadow is applied
			// on unwind, after the load, on every reload including reloads
			// triggered by settings saves.
			if err := e.Next(); err != nil {
				return err
			}
			m.applyShadow(e.App.Settings(), e.App.Logger())
			return nil
		},
	})

	neutralize := func(e *core.ModelEvent) error {
		s, ok := e.Model.(*core.Settings)
		if !ok {
			return e.Next()
		}
		clone, err := m.neutralClone(s)
		if err != nil {
			return err
		}
		// The clone is what reaches the database. The original in-memory
		// settings keep the host values and stay installed on the app.
		e.Model = clone
		err = e.Next()
		e.Model = s
		return err
	}
	app.OnModelCreate(paramsTable).Bind(&hook.Handler[*core.ModelEvent]{Id: "pbvexHostingManagedNeutralCreate", Priority: -100, Func: neutralize})
	app.OnModelUpdate(paramsTable).Bind(&hook.Handler[*core.ModelEvent]{Id: "pbvexHostingManagedNeutralUpdate", Priority: -100, Func: neutralize})

	// Rewrite any persisted managed-category values left from before the
	// host-managed configuration was enabled. The save passes through the
	// neutralization hooks above, so the stored row becomes the neutral
	// baseline while the in-memory settings are re-shadowed by the reload
	// that the save itself triggers.
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id:       "pbvexHostingManagedBaseline",
		Priority: 92,
		Func: func(e *core.BootstrapEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			return e.App.Save(e.App.Settings())
		},
	})

	return nil
}

// applyShadow writes the host-managed values onto the in-memory settings.
// Clone+Merge are used because the Settings internal lock is unexported;
// Merge performs the final swap under that lock.
func (m *managedInjection) applyShadow(s *core.Settings, logger *slog.Logger) {
	shadow, err := s.Clone()
	if err != nil {
		if logger != nil {
			logger.Error("hosting managed settings shadow skipped", "error", err.Error())
		}
		return
	}
	if m.storageManaged() {
		shadow.S3 = m.storage
		shadow.Backups.S3 = m.storage
	}
	if m.smtpManaged() {
		m.smtp.ApplyTo(shadow)
	}
	if err := s.Merge(shadow); err != nil && logger != nil {
		logger.Error("hosting managed settings shadow failed", "error", err.Error())
	}
}

// effectivePersisted rewrites the managed categories of a settings object
// into the neutral persisted baseline. It is applied to the submitted
// settings before a save so that the host-managed values held in memory can
// never reach the database, even when a caller echoes unrelated settings
// back with the managed parts untouched. A nil injection is a no-op.
func (m *managedInjection) effectivePersisted(s *core.Settings) error {
	if m == nil {
		return nil
	}
	neutral, err := m.neutralClone(s)
	if err != nil {
		return err
	}
	if m.storageManaged() {
		s.S3 = neutral.S3
		s.Backups.S3 = neutral.Backups.S3
	}
	if m.smtpManaged() {
		s.SMTP = neutral.SMTP
	}
	return nil
}

// neutralClone returns a copy of the settings with the managed categories
// reset to the neutral persisted baseline (everything disabled and empty).
// Persisting anything else would put host-owned values or secrets into the
// tenant database, where backups and restores would carry them.
func (m *managedInjection) neutralClone(s *core.Settings) (*core.Settings, error) {
	clone, err := s.Clone()
	if err != nil {
		return nil, err
	}
	if m.storageManaged() {
		clone.S3 = core.S3Config{}
		clone.Backups.S3 = core.S3Config{}
	}
	if m.smtpManaged() {
		clone.SMTP = core.SMTPConfig{}
	}
	return clone, nil
}
