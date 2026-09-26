package pbvex

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/jsvm"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/nathabonfim59/pbvex/backend/hosting/storagequota"
	"github.com/nathabonfim59/pbvex/backend/internal/api"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/realtime"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
	"github.com/nathabonfim59/pbvex/backend/internal/scheduler"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/nathabonfim59/pbvex/backend/internal/storage"
)

// Config configures PBVex registration.
type Config struct {
	Hosting       hosting.Config
	PublicDir     string
	IndexFallback bool
	HooksDir      string
	HooksWatch    bool
	HooksPool     int
	MigrationsDir string
	Automigrate   bool
	Runtime       runtime.Config
	Deploy        deploy.Config
	Realtime      realtime.Config
	Scheduler     scheduler.Config
	Storage       storage.Config
	SMTP          SMTPConfig
	// HostManagedStorageS3 configures host-owned S3 storage for record files
	// and backups. It is honored only with hosting integration enabled and
	// only when Enabled is true; the values are injected into the running
	// app without being persisted in the tenant database.
	HostManagedStorageS3 core.S3Config
	CORS                 api.CORSConfig
	// DevDeployToken grants deployment-only access from loopback requests while
	// it is configured. It must never be configured for a production server.
	DevDeployToken string
}

// DefaultConfig returns sane defaults for PBVex.
func DefaultConfig() Config {
	return Config{
		PublicDir:     defaultPublicDir(),
		IndexFallback: true,
		HooksWatch:    true,
		HooksPool:     15,
		Automigrate:   true,
		Runtime:       runtime.DefaultConfig(),
		Deploy:        deploy.DefaultConfig(),
		Realtime:      realtime.DefaultConfig(),
		Scheduler:     scheduler.DefaultConfig(),
		Storage:       storage.DefaultConfig(),
		CORS:          api.DefaultCORSConfig(),
	}
}

// Register wires PBVex behavior into the provided PocketBase application.
func Register(app *pocketbase.PocketBase, cfg Config) error {
	if _, _, err := RegisterCore(app, cfg); err != nil {
		return err
	}

	// Optional plugins.
	if !cfg.Hosting.Enabled {
		jsvm.MustRegister(app, jsvm.Config{
			MigrationsDir: cfg.MigrationsDir,
			HooksDir:      cfg.HooksDir,
			HooksWatch:    cfg.HooksWatch,
			HooksPoolSize: cfg.HooksPool,
		})
	}

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		TemplateLang: migratecmd.TemplateLangJS,
		Automigrate:  cfg.Automigrate,
		Dir:          cfg.MigrationsDir,
	})

	// Static files fallback. If no publicDir is configured, skip the route so
	// a missing public directory cannot prevent API-only startup.
	if cfg.PublicDir != "" {
		app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
			Func: func(e *core.ServeEvent) error {
				if !e.Router.HasRoute(http.MethodGet, "/{path...}") {
					e.Router.GET("/{path...}", apis.Static(os.DirFS(cfg.PublicDir), cfg.IndexFallback))
				}
				return e.Next()
			},
			Priority: 999,
		})
	}

	return nil
}

// RegisterCore wires PBVex core behavior into any core.App implementation.
func RegisterCore(app core.App, cfg Config) (*deploy.Service, deploy.Invalidator, error) {
	// Host-managed storage is a hosted-mode deployment concern: without the
	// policy integration there is no hosting boundary, so the configuration
	// is rejected instead of being silently ignored.
	if !cfg.Hosting.Enabled && cfg.HostManagedStorageS3.Enabled {
		return nil, nil, errors.New("host-managed storage requires hosting integration to be enabled")
	}
	if err := cfg.HostManagedStorageS3.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid host-managed storage configuration: %w", err)
	}
	client, hello, err := newHostingClient(cfg.Hosting)
	if err != nil {
		return nil, nil, err
	}
	var managed *managedInjection
	var quotaClient *storagequota.Client
	if client != nil {
		// Hosting enabled enforces storage byte quotas: the same provider
		// must serve the /v1/storage/ routes on the shared socket and list
		// the storage.reserve capability in the startup handshake. Without
		// this check an enabled deployment would silently depend on a
		// provider that cannot account bytes, so startup fails instead —
		// there is no silent downgrade and no separate bootstrap flag. A
		// provider extends its endpoint by serving the storage routes and
		// adding the capability; see docs/hosting-storage-quotas.md.
		if !storagequota.SupportsReserve(hello) {
			client.Close()
			return nil, nil, errors.New("hosting provider does not support storage byte quotas (missing storage.reserve handshake capability)")
		}
		// The quota client borrows the policy client's transport: one
		// socket, one connection pool, one in-flight budget per app. It is
		// closed with the root client at terminate and never closed here.
		quotaClient, err = storagequota.NewClientFromRoot(client)
		if err != nil {
			client.Close()
			return nil, nil, err
		}
		managed = registerManagedInjection(cfg.HostManagedStorageS3, cfg.SMTP)
		app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { client.Close(); return e.Next() })
		if err := registerHosting(app, client, managed); err != nil {
			return nil, nil, err
		}
		if managed != nil {
			if err := managed.register(app); err != nil {
				return nil, nil, err
			}
		}
		// One shared client gates administrative operations and meters every
		// observed runtime execution. An externally supplied observer is
		// composed (external Begin first), never silently overwritten. The
		// environment resolver is composed the same way: permission first,
		// then any configured custom resolver, then the default lookup.
		cfg.Runtime.ExecutionObserver = newHostingExecutionObserver(app.Logger(), client, cfg.Runtime.ExecutionObserver)
		cfg.Runtime.EnvironmentResolver = hostedEnvironmentResolver(client, app.Logger(), cfg.Runtime.EnvironmentResolver)
	}
	repo := deploy.NewRepo()
	manager := runtime.NewManager(cfg.Runtime)
	storageService, err := storage.NewService(app, storage.NewRepo(), cfg.Storage)
	if err != nil {
		return nil, nil, err
	}
	if quotaClient != nil {
		// Install the quota observer and the native hooks before the
		// service is started (the bootstrap handler below calls Start), so
		// no upload, variant or native record write can run unaccounted
		// and the thumbnail gate is in place before the router is built.
		storageService.SetQuotaObserver(storage.NewHostedQuotaObserver(quotaClient))
		if err := storageService.InstallNativeQuotaHooks(app); err != nil {
			return nil, nil, err
		}
	}
	manager.AddContextExtender(storageExtender(storageService))
	manager.AddContextExtender(emailExtender())
	manager.AddContextExtender(outboundHTTPExtender(nil))
	service := deploy.NewService(app, repo, manager, cfg.Deploy)
	schedulerService := scheduler.NewService(app, service, cfg.Scheduler)
	cronManager := scheduler.NewCronManager(app, schedulerService)
	service.SetActivationObserver(cronManager)
	manager.Scheduler = schedulerService

	broadcaster := realtime.NewBroadcaster(service, cfg.Realtime)
	service.SetInvalidator(broadcaster)

	// Bootstrap the PBVex system schema after the core bootstrap.
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id:       "pbvexBootstrap",
		Priority: 90,
		Func: func(e *core.BootstrapEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			if err := schema.Bootstrap(e.App); err != nil {
				return err
			}
			if err := storageService.Start(); err != nil {
				return err
			}
			if err := service.WarmActive(); err != nil {
				_ = storageService.Stop()
				return err
			}
			if err := schedulerService.Start(context.Background()); err != nil {
				_ = storageService.Stop()
				return err
			}
			return nil
		},
	})

	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id:       "pbvexTerminate",
		Priority: 90,
		Func: func(e *core.TerminateEvent) error {
			cronManager.Clear()
			schedulerService.Stop()
			if err := storageService.Stop(); err != nil {
				return err
			}
			return e.Next()
		},
	})

	// PBVEX_SMTP_* overrides for PocketBase mail settings. Priority 95 runs
	// inside the pbvexBootstrap e.Next() chain, after the core bootstrap has
	// loaded the persisted settings and before the PBVex system schema work.
	// With hosting integration enabled the same variables are host-owned:
	// the managed injection applies them to the in-memory settings only and
	// the persisted mail settings stay neutral, so they are intentionally
	// not persisted here.
	if !cfg.SMTP.Empty() && !cfg.Hosting.Enabled {
		app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
			Id:       "pbvexSMTPSettings",
			Priority: 95,
			Func: func(e *core.BootstrapEvent) error {
				if err := e.Next(); err != nil {
					return err
				}
				return ApplySMTPSettings(e.App, cfg.SMTP)
			},
		})
	}

	// Protect reserved PBVex collections and generated document backing stores
	// from direct writes. Runtime writes carry InternalContextKey and therefore
	// retain their request cancellation/deadline while raw app/API writes do not
	// get an authorization bypass.
	protect := func(e *core.RecordEvent) error {
		if e.Context != nil && e.Context.Value(schema.InternalContextKey) != nil {
			return e.Next()
		}
		if schema.IsReservedCollection(e.Record.Collection().Name) || schema.IsBackingCollection(e.Record.Collection()) {
			return router.NewForbiddenError("PBVex system collections are immutable.", nil)
		}
		return e.Next()
	}

	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{
		Id:       "pbvexProtectCreate",
		Priority: 0,
		Func:     protect,
	})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{
		Id:       "pbvexProtectUpdate",
		Priority: 0,
		Func:     protect,
	})
	app.OnRecordDelete().Bind(&hook.Handler[*core.RecordEvent]{
		Id:       "pbvexProtectDelete",
		Priority: 0,
		Func:     protect,
	})

	// Conservative invalidation: any successful record mutation invalidates
	// all active subscriptions. Coalescing in the broadcaster keeps this bounded.
	invalidate := func(e *core.RecordEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		broadcaster.InvalidateAll()
		return nil
	}
	app.OnRecordAfterCreateSuccess().Bind(&hook.Handler[*core.RecordEvent]{
		Id:       "pbvexInvalidateCreate",
		Priority: 0,
		Func:     invalidate,
	})
	app.OnRecordAfterUpdateSuccess().Bind(&hook.Handler[*core.RecordEvent]{
		Id:       "pbvexInvalidateUpdate",
		Priority: 0,
		Func:     invalidate,
	})
	app.OnRecordAfterDeleteSuccess().Bind(&hook.Handler[*core.RecordEvent]{
		Id:       "pbvexInvalidateDelete",
		Priority: 0,
		Func:     invalidate,
	})

	// PocketBase superusers bypass nil collection rules and hidden fields. The
	// generated backing collection is intentionally not an administrative raw
	// API: bypassing it would sidestep ctx.db validation, opaque IDs and
	// transaction semantics. Request hooks cover every built-in record endpoint
	// while internal PBVex work uses the app directly.
	forbidBackingList := func(e *core.RecordsListRequestEvent) error {
		if schema.IsBackingCollection(e.Collection) {
			return router.NewForbiddenError("PBVex document storage is not available through the PocketBase API.", nil)
		}
		return e.Next()
	}
	forbidBackingRecord := func(e *core.RecordRequestEvent) error {
		if schema.IsBackingCollection(e.Collection) {
			return router.NewForbiddenError("PBVex document storage is not available through the PocketBase API.", nil)
		}
		return e.Next()
	}
	app.OnRecordsListRequest().Bind(&hook.Handler[*core.RecordsListRequestEvent]{Id: "pbvexProtectBackingList", Priority: 0, Func: forbidBackingList})
	app.OnRecordViewRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "pbvexProtectBackingView", Priority: 0, Func: forbidBackingRecord})
	app.OnRecordCreateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "pbvexProtectBackingCreate", Priority: 0, Func: forbidBackingRecord})
	app.OnRecordUpdateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "pbvexProtectBackingUpdate", Priority: 0, Func: forbidBackingRecord})
	app.OnRecordDeleteRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "pbvexProtectBackingDelete", Priority: 0, Func: forbidBackingRecord})

	api.Register(app, service, broadcaster, schedulerService, storageService, cfg.Storage.BasePath, cfg.CORS, cfg.DevDeployToken)

	return service, broadcaster, nil
}

func defaultPublicDir() string {
	// Public files are served from the current working directory, never from
	// the executable directory. The static handler safely 404s if the
	// directory is missing, so API-only deployments do not break.
	return "./pb_public"
}
