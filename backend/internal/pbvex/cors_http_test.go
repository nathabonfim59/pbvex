package pbvex

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/api"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// httpActionBundle registers an httpAction that echoes the URL, method,
// authorization header, and any action-supplied CORS headers.
const corsTestBundle = `__pbvex.registerFunction({name:"corsEcho",type:"httpAction",visibility:"public",modulePath:"h.js",exportName:"corsEcho",route:{method:"POST",path:"echo"}},function(ctx,request){
	return new Response(request.url,{status:200,headers:{"Access-Control-Allow-Origin":"https://evil.example"}});
});`

func corsTestUploadRequest(deploymentID, bundle string) map[string]any {
	b64 := testBundle(bundle)
	h := bundleHash(bundle)
	return map[string]any{
		"manifest": map[string]any{
			"protocolVersion": "v1",
			"deploymentId":    deploymentID,
			"functions": []any{
				map[string]any{
					"name":       "corsEcho",
					"type":       "httpAction",
					"visibility": "public",
					"modulePath": "h.js",
					"exportName": "corsEcho",
					"route":      map[string]any{"method": "POST", "path": "echo"},
				},
			},
		},
		"bundle": b64,
		"sha256": h,
		"size":   int64(len(bundle)),
	}
}

func setupCORSTestApp(t *testing.T) (*deploy.Service, http.Handler) {
	t.Helper()
	app, service := newTestApp(t)
	base, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: base}); err != nil {
		t.Fatal(err)
	}
	mux, err := base.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return service, mux
}

func TestCORSPreflightOnlyEchoesConfiguredMethods(t *testing.T) {
	service, mux := setupCORSTestApp(t)
	if _, err := service.Upload(corsTestUploadRequest("cors1", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("cors1", true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodOptions, "/api/pbvex/echo", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "X-Evil-Header")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent && rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 204/200", rr.Code)
	}
	acao := rr.Header().Get("Access-Control-Allow-Origin")
	if acao != "*" {
		t.Fatalf("ACAO %q, want * (default wildcard)", acao)
	}
	acah := rr.Header().Get("Access-Control-Allow-Headers")
	if strings.Contains(acah, "X-Evil-Header") {
		t.Fatalf("ACAH must not reflect arbitrary request headers, got %q", acah)
	}
	if !strings.Contains(acah, "Content-Type") {
		t.Fatalf("ACAH must include configured AllowedHeaders, got %q", acah)
	}
}

func setupCORSTestAppWithConfig(t *testing.T, corsCfg api.CORSConfig) (*deploy.Service, http.Handler) {
	t.Helper()
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	cfg := DefaultConfig()
	cfg.Runtime.PoolSize = 2
	cfg.Runtime.Timeout = 2 * time.Second
	cfg.Deploy.HistoryLimit = 5
	cfg.CORS = corsCfg
	service, _, err := RegisterCore(app, cfg)
	if err != nil {
		app.Cleanup()
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap: %v", err)
	}
	if err := app.RunAllMigrations(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to run migrations: %v", err)
	}
	base, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: base}); err != nil {
		t.Fatal(err)
	}
	mux, err := base.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return service, mux
}

func TestCORSPreflightDeniedOriginReturns403(t *testing.T) {
	restrictedCORS := api.CORSConfig{
		AllowedOrigins: []string{"https://app.example"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAgeSeconds:  3600,
	}
	service, mux := setupCORSTestAppWithConfig(t, restrictedCORS)
	if _, err := service.Upload(corsTestUploadRequest("cors2", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("cors2", true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodOptions, "/api/pbvex/echo", nil)
	req.Header.Set("Origin", "https://denied.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code == http.StatusNoContent {
		t.Fatal("denied origin must not get 204")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("denied origin must not get ACAO header")
	}
}

func TestHTTPActionStripsActionSuppliedACAO(t *testing.T) {
	service, mux := setupCORSTestApp(t)
	if _, err := service.Upload(corsTestUploadRequest("cors3", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("cors3", true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	req.Header.Set("Origin", "https://app.example")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	acao := rr.Header().Get("Access-Control-Allow-Origin")
	if acao == "https://evil.example" {
		t.Fatal("action-supplied ACAO must be stripped, not echoed")
	}
}

func TestPublicErrorsShareSanitizedRequestIDAndCORS(t *testing.T) {
	_, mux := setupCORSTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("X-Request-Id", "invalid/request/id")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rr.Code, rr.Body.String())
	}
	requestID := rr.Header().Get("X-Request-Id")
	if requestID == "" || requestID == "invalid/request/id" {
		t.Fatalf("request ID was not replaced: %q", requestID)
	}
	var body struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RequestID != requestID {
		t.Fatalf("body requestId %q != header %q", body.RequestID, requestID)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("structured public error omitted CORS header")
	}
	if !strings.Contains(rr.Header().Get("Access-Control-Expose-Headers"), "X-Request-Id") {
		t.Fatal("request ID response header is not CORS-exposed")
	}
}

func TestHTTPActionOversizedBodyReturns413WithRequestMetadata(t *testing.T) {
	service, mux := setupCORSTestApp(t)
	upload := corsTestUploadRequest("smallbody", corsTestBundle)
	upload["manifest"].(map[string]any)["config"] = map[string]any{"maxFunctionArgsBytes": 4}
	if _, err := service.Upload(upload); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("smallbody", true); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader("12345"))
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("X-Request-Id", "oversized-body")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-Request-Id"); got != "oversized-body" {
		t.Fatalf("request ID %q, want oversized-body", got)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("413 response omitted CORS header")
	}
}

func TestRealtimeCORSEndpointHeaders(t *testing.T) {
	_, mux := setupCORSTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/pbvex/realtime?id=sub1&path=test", nil)
	req.Header.Set("Origin", "https://app.example")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	acao := rr.Header().Get("Access-Control-Allow-Origin")
	if acao == "" {
		t.Fatal("realtime endpoint must set CORS Access-Control-Allow-Origin header")
	}
}

func TestHTTPRequestAbsoluteURL(t *testing.T) {
	service, mux := setupCORSTestApp(t)
	if _, err := service.Upload(corsTestUploadRequest("abs1", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("abs1", true); err != nil {
		t.Fatal(err)
	}

	// Spoofed Host and X-Forwarded-Proto must be ignored; URL must be
	// constructed from the server AppURL, not raw client headers.
	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	req.Host = "evil.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	body := strings.TrimSpace(rr.Body.String())
	if !strings.HasPrefix(body, "http://") && !strings.HasPrefix(body, "https://") {
		t.Fatalf("Request.url must be absolute, got %q", body)
	}
	if strings.Contains(body, "evil.example") {
		t.Fatalf("Request.url must not contain spoofed Host, got %q", body)
	}
}

func TestHTTPActionAuthorizationHeaderReachesAction(t *testing.T) {
	authBundle := `__pbvex.registerFunction({name:"corsEcho",type:"httpAction",visibility:"public",modulePath:"h.js",exportName:"corsEcho",route:{method:"POST",path:"echo"}},function(ctx,request){
		var auth = request.headers.get("authorization");
		return new Response(auth || "none",{status:200});
	});`
	service, mux := setupCORSTestApp(t)
	if _, err := service.Upload(corsTestUploadRequest("auth1", authBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("auth1", true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer secret-token")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	body := strings.TrimSpace(rr.Body.String())
	if body != "Bearer secret-token" {
		t.Fatalf("Authorization header must reach action, got %q", body)
	}
}

func TestCORSRejectsCredentialedWildcard(t *testing.T) {
	credWildcardCORS := api.CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	}
	service, mux := setupCORSTestAppWithConfig(t, credWildcardCORS)
	if _, err := service.Upload(corsTestUploadRequest("cw1", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("cw1", true); err != nil {
		t.Fatal(err)
	}

	// With AllowCredentials=true and AllowedOrigins=["*"], the wildcard
	// must be suppressed. A specific origin must NOT get ACAO=*.
	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	req.Header.Set("Origin", "https://app.example")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	acao := rr.Header().Get("Access-Control-Allow-Origin")
	if acao == "*" {
		t.Fatal("must never emit ACAO=* with AllowCredentials=true")
	}
}

func setupCORSTestAppWithAppURL(t *testing.T, appURL string) (*deploy.Service, http.Handler) {
	t.Helper()
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	cfg := DefaultConfig()
	cfg.Runtime.PoolSize = 2
	cfg.Runtime.Timeout = 2 * time.Second
	cfg.Deploy.HistoryLimit = 5
	service, _, err := RegisterCore(app, cfg)
	if err != nil {
		app.Cleanup()
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap: %v", err)
	}
	if err := app.RunAllMigrations(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to run migrations: %v", err)
	}
	// Set production AppURL after bootstrap loaded default settings, before
	// OnServe resolves it into CORSConfig.AppURL.
	app.Settings().Meta.AppURL = appURL
	base, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: base}); err != nil {
		t.Fatal(err)
	}
	mux, err := base.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return service, mux
}

func TestHTTPRequestURLUsesSettingsAppURL(t *testing.T) {
	service, mux := setupCORSTestAppWithAppURL(t, "https://prod.example.com")
	if _, err := service.Upload(corsTestUploadRequest("produrl", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("produrl", true); err != nil {
		t.Fatal(err)
	}

	// Spoofed Host and X-Forwarded-Proto must be ignored; URL must come
	// from the PocketBase settings AppURL, not raw client headers.
	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	req.Host = "evil.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	body := strings.TrimSpace(rr.Body.String())
	if !strings.HasPrefix(body, "https://prod.example.com/") {
		t.Fatalf("Request.url must use settings AppURL, got %q", body)
	}
	if strings.Contains(body, "evil.example") {
		t.Fatalf("Request.url must not contain spoofed Host, got %q", body)
	}
}

func TestExplicitAppURLOverridesSettings(t *testing.T) {
	explicitCORS := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		AppURL:         "https://explicit.example.org",
	}
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	cfg := DefaultConfig()
	cfg.Runtime.PoolSize = 2
	cfg.Runtime.Timeout = 2 * time.Second
	cfg.Deploy.HistoryLimit = 5
	cfg.CORS = explicitCORS
	service, _, err := RegisterCore(app, cfg)
	if err != nil {
		app.Cleanup()
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap: %v", err)
	}
	if err := app.RunAllMigrations(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to run migrations: %v", err)
	}
	// Settings AppURL is different from explicit CORSConfig.AppURL.
	app.Settings().Meta.AppURL = "https://settings.example.net"
	base, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: base}); err != nil {
		t.Fatal(err)
	}
	mux, err := base.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Upload(corsTestUploadRequest("explicit", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("explicit", true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	body := strings.TrimSpace(rr.Body.String())
	if !strings.HasPrefix(body, "https://explicit.example.org/") {
		t.Fatalf("Request.url must use explicit CORSConfig.AppURL, got %q", body)
	}
}

func TestHTTPRequestURLPreservesBasePath(t *testing.T) {
	service, mux := setupCORSTestAppWithAppURL(t, "https://example.com/myapp")
	if _, err := service.Upload(corsTestUploadRequest("basepath", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("basepath", true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	body := strings.TrimSpace(rr.Body.String())
	if !strings.HasPrefix(body, "https://example.com/myapp/api/pbvex/echo") {
		t.Fatalf("Request.url must preserve configured base-path, got %q", body)
	}
}

func setupAndTriggerServe(t *testing.T, corsCfg api.CORSConfig, settingsAppURL string) error {
	t.Helper()
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	cfg := DefaultConfig()
	cfg.Runtime.PoolSize = 2
	cfg.Runtime.Timeout = 2 * time.Second
	cfg.Deploy.HistoryLimit = 5
	cfg.CORS = corsCfg
	if _, _, err := RegisterCore(app, cfg); err != nil {
		app.Cleanup()
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap: %v", err)
	}
	if err := app.RunAllMigrations(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to run migrations: %v", err)
	}
	app.Settings().Meta.AppURL = settingsAppURL
	base, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	return app.OnServe().Trigger(&core.ServeEvent{App: app, Router: base})
}

func TestExplicitInvalidAppURLRejected(t *testing.T) {
	invalidCORS := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		AppURL:         "not-a-valid-url",
	}
	if err := setupAndTriggerServe(t, invalidCORS, ""); err == nil {
		t.Fatal("expected startup error for invalid explicit AppURL")
	}
}

func TestSettingsInvalidAppURLRejected(t *testing.T) {
	defaultCORS := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}
	if err := setupAndTriggerServe(t, defaultCORS, "ftp://bad-scheme"); err == nil {
		t.Fatal("expected startup error for invalid settings AppURL")
	}
}

func TestExplicitEncodedBasePathPreserved(t *testing.T) {
	encodedCORS := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		AppURL:         "https://example.com/my%2Ftenant",
	}
	service, mux := setupCORSTestAppWithConfig(t, encodedCORS)
	if _, err := service.Upload(corsTestUploadRequest("enc1", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("enc1", true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	body := strings.TrimSpace(rr.Body.String())
	if !strings.HasPrefix(body, "https://example.com/my%2Ftenant/api/pbvex/echo") {
		t.Fatalf("Request.url must preserve encoded base-path, got %q", body)
	}
}

func TestSettingsEncodedBasePathPreserved(t *testing.T) {
	service, mux := setupCORSTestAppWithAppURL(t, "https://example.com/my%2Ftenant")
	if _, err := service.Upload(corsTestUploadRequest("enc2", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("enc2", true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	body := strings.TrimSpace(rr.Body.String())
	if !strings.HasPrefix(body, "https://example.com/my%2Ftenant/api/pbvex/echo") {
		t.Fatalf("Request.url must preserve encoded base-path from settings, got %q", body)
	}
}

func TestBareQueryDelimiterRejected(t *testing.T) {
	explicitBareQuery := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		AppURL:         "https://example.com/base?",
	}
	if err := setupAndTriggerServe(t, explicitBareQuery, ""); err == nil {
		t.Fatal("expected startup error for explicit AppURL with bare '?'")
	}
	defaultCORS := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}
	if err := setupAndTriggerServe(t, defaultCORS, "https://example.com/base?"); err == nil {
		t.Fatal("expected startup error for settings AppURL with bare '?'")
	}
}

func TestPopulatedQueryRejected(t *testing.T) {
	explicitQuery := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		AppURL:         "https://example.com/base?token=x",
	}
	if err := setupAndTriggerServe(t, explicitQuery, ""); err == nil {
		t.Fatal("expected startup error for explicit AppURL with populated query")
	}
	defaultCORS := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}
	if err := setupAndTriggerServe(t, defaultCORS, "https://example.com/base?token=x"); err == nil {
		t.Fatal("expected startup error for settings AppURL with populated query")
	}
}

func TestBareFragmentDelimiterRejected(t *testing.T) {
	explicitBareFragment := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		AppURL:         "https://example.com/base#",
	}
	if err := setupAndTriggerServe(t, explicitBareFragment, ""); err == nil {
		t.Fatal("expected startup error for explicit AppURL with bare '#'")
	}
	defaultCORS := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}
	if err := setupAndTriggerServe(t, defaultCORS, "https://example.com/base#"); err == nil {
		t.Fatal("expected startup error for settings AppURL with bare '#'")
	}
}

func TestEncodedQueryAndFragmentInPathAccepted(t *testing.T) {
	// %3F (encoded '?') and %23 (encoded '#') in the path must be accepted
	// and preserved — they are not query/fragment delimiters.
	encodedCORS := api.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		AppURL:         "https://example.com/p%3Fq%23r",
	}
	service, mux := setupCORSTestAppWithConfig(t, encodedCORS)
	if _, err := service.Upload(corsTestUploadRequest("enc3", corsTestBundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("enc3", true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/echo", strings.NewReader(""))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	body := strings.TrimSpace(rr.Body.String())
	if !strings.HasPrefix(body, "https://example.com/p%3Fq%23r/api/pbvex/echo") {
		t.Fatalf("Request.url must preserve encoded %%3F/%%23 in path, got %q", body)
	}
}
