package pbvex

import (
	"context"
	"net/http"
	"os"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/jsvm"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"

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
	CORS          api.CORSConfig
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
	jsvm.MustRegister(app, jsvm.Config{
		MigrationsDir: cfg.MigrationsDir,
		HooksDir:      cfg.HooksDir,
		HooksWatch:    cfg.HooksWatch,
		HooksPoolSize: cfg.HooksPool,
	})

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
	repo := deploy.NewRepo()
	manager := runtime.NewManager(cfg.Runtime)
	storageService, err := storage.NewService(app, storage.NewRepo(), cfg.Storage)
	if err != nil {
		return nil, nil, err
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

	api.Register(app, service, broadcaster, schedulerService, storageService, cfg.Storage.BasePath, cfg.CORS)

	return service, broadcaster, nil
}

func defaultPublicDir() string {
	// Public files are served from the current working directory, never from
	// the executable directory. The static handler safely 404s if the
	// directory is missing, so API-only deployments do not break.
	return "./pb_public"
}
