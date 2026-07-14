package pbvex

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestFullRegisterMuxKeepsHTTPActionsAndStaticFallbackDistinct(t *testing.T) {
	publicDir := t.TempDir()
	if err := os.WriteFile(publicDir+"/asset.txt", []byte("static asset"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	cfg := DefaultConfig()
	cfg.PublicDir = publicDir
	cfg.HooksWatch = false
	cfg.MigrationsDir = t.TempDir()
	if err := Register(app, cfg); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}); err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("full PocketBase mux failed to build: %v", err)
	}

	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	} {
		t.Run("http action "+method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/pbvex/not-deployed", nil)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusNotFound, rr.Body.String())
			}
			if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("HTTP action was shadowed by static fallback: Content-Type=%q", got)
			}
		})
	}

	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/api/pbvex/call", http.StatusBadRequest},
		{http.MethodGet, "/api/pbvex/deployments", http.StatusUnauthorized},
		{http.MethodOptions, "/api/pbvex/realtime", http.StatusNoContent},
	} {
		t.Run("reserved "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rr.Code, tc.want, rr.Body.String())
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/asset.txt", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "static asset" {
		t.Fatalf("static fallback status=%d body=%q", rr.Code, rr.Body.String())
	}
}

func TestPlatformRouteMatrixKeepsStorageRealtimeCallAndAdminDistinct(t *testing.T) {
	app, _ := newTestApp(t)
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}); err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		method string
		path   string
		body   []byte
		want   int
	}{
		{http.MethodPost, "/api/pbvex/call", []byte(`{"name":"missing","args":{}}`), http.StatusNotFound},
		{http.MethodPost, "/api/pbvex/realtime", []byte(`{}`), http.StatusNotAcceptable},
		{http.MethodGet, "/api/pbvex/realtime", nil, http.StatusNotAcceptable},
		{http.MethodPost, "/api/pbvex/storage/upload/tampered", []byte("x"), http.StatusUnauthorized},
		{http.MethodGet, "/api/pbvex/storage/not-a-storage-id", nil, http.StatusNotFound},
		{http.MethodHead, "/api/pbvex/storage/not-a-storage-id", nil, http.StatusNotFound},
		{http.MethodGet, "/api/pbvex/deployments", nil, http.StatusUnauthorized},
		{http.MethodGet, "/api/pbvex/jobs", nil, http.StatusUnauthorized},
		{http.MethodOptions, "/api/pbvex/call", nil, http.StatusNoContent},
		{http.MethodOptions, "/api/pbvex/realtime", nil, http.StatusNoContent},
		{http.MethodOptions, "/api/pbvex/storage/upload/token", nil, http.StatusNoContent},
		{http.MethodOptions, "/api/pbvex/storage/pbv_0123456789abcdef0123456789abcdef", nil, http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
			if len(tc.body) > 0 && (tc.path == "/api/pbvex/call" || tc.path == "/api/pbvex/realtime") {
				req.Header.Set("Content-Type", "application/json")
			}
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rr.Code, tc.want, rr.Body.String())
			}
		})
	}
}

func TestStorageBasePathValidationPreventsRouterCollisions(t *testing.T) {
	for _, basePath := range []string{"/api/pbvex/storage", "/files", "/api2/files"} {
		t.Run("build mux "+basePath, func(t *testing.T) {
			app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer app.Cleanup()
			cfg := DefaultConfig()
			cfg.Storage.BasePath = basePath
			if _, _, err := RegisterCore(app, cfg); err != nil {
				t.Fatalf("valid storage route rejected: %v", err)
			}
			router, err := apis.NewRouter(app)
			if err != nil {
				t.Fatal(err)
			}
			if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}); err != nil {
				t.Fatal(err)
			}
			if _, err := router.BuildMux(); err != nil {
				t.Fatalf("valid storage route failed BuildMux: %v", err)
			}
		})
	}

	for _, basePath := range []string{
		"/api",
		"/api/other",
		"/api/pbvex",
		"/api/pbvex/call",
		"/api/pbvex/call/nested",
		"/api/pbvex/realtime",
		"/api/pbvex/realtime/events",
		"/api/pbvex/deployments",
		"/api/pbvex/deployments/archive",
		"/api/pbvex/jobs",
		"/api/pbvex/jobs/archive",
		"/api/pbvex/storage/nested",
	} {
		t.Run("reject "+basePath, func(t *testing.T) {
			app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer app.Cleanup()
			cfg := DefaultConfig()
			cfg.Storage.BasePath = basePath
			if _, _, err := RegisterCore(app, cfg); err == nil {
				t.Fatalf("expected collision %q to fail before route registration", basePath)
			} else if !strings.Contains(err.Error(), "reserved platform route") {
				t.Fatalf("unexpected collision error: %v", err)
			}
		})
	}
}
