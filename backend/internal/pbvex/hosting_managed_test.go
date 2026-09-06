package pbvex

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/nathabonfim59/pbvex/backend/hosting/storagequota"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/mailer"
)

const (
	hostS3Secret  = "host-s3-secret-do-not-leak"
	hostSMTPPass  = "host-smtp-password-do-not-leak"
	hostS3Bucket  = "host-managed-bucket"
	hostS3Endpoin = "https://s3.host-managed.example"
)

func hostedManagedStorage() core.S3Config {
	return core.S3Config{
		Enabled:   true,
		Bucket:    hostS3Bucket,
		Region:    "auto",
		Endpoint:  hostS3Endpoin,
		AccessKey: "host-access-key",
		Secret:    hostS3Secret,
	}
}

func hostedManagedSMTP() SMTPConfig {
	enabled := true
	port := 587
	host := "smtp.host-managed.example"
	user := "host-mail-user"
	return SMTPConfig{
		Enabled:  &enabled,
		Host:     &host,
		Port:     hostedSMTPIntPtr(port),
		Username: &user,
		Password: hostedSMTPStrPtr(hostSMTPPass),
	}
}

func hostedSMTPStrPtr(v string) *string { return &v }

func hostedSMTPIntPtr(v int) *int { return &v }

// hostedSettingsRow mirrors the persisted settings parts this file asserts on.
type hostedSettingsRow struct {
	S3      core.S3Config      `json:"s3"`
	SMTP    core.SMTPConfig    `json:"smtp"`
	Backups core.BackupsConfig `json:"backups"`
	Meta    core.MetaConfig    `json:"meta"`
}

func readPersistedSettings(t *testing.T, app *tests.TestApp) hostedSettingsRow {
	t.Helper()
	var row struct {
		Value []byte `db:"value"`
	}
	if err := app.DB().NewQuery("SELECT value FROM `_params` WHERE id='settings'").One(&row); err != nil {
		t.Fatalf("failed to read persisted settings: %v", err)
	}
	var parsed hostedSettingsRow
	if err := json.Unmarshal(row.Value, &parsed); err != nil {
		t.Fatalf("failed to parse persisted settings: %v", err)
	}
	return parsed
}

// newHostedTestApp boots a full test app with hosting integration enabled
// against an in-process provider on one Unix socket and exposes the built
// PocketBase router so native endpoints can be exercised. With withQuota
// the provider is the example-compatible composition: the policy reference
// service and the storage byte quota reference service share the socket,
// and the composed /v1/hello lists both capabilities, exactly like
// backend/examples/policy-service. Without it the socket serves the
// policy-only reference service, so RegisterCore must refuse startup.
func newHostedTestApp(t *testing.T, mutate func(*Config), withQuota bool) (*tests.TestApp, *hosting.ReferenceService, *storagequota.ReferenceQuotaService, *http.Server, http.Handler) {
	t.Helper()
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	// Disable the PocketBase request activity logger for these in-process
	// router tests. In PocketBase v0.40.1 (still on master) the logger reads
	// app.Settings().Logs.MaxDays without taking the settings lock from the
	// fire-and-forget goroutine that records each finished request
	// (apis.logRequest -> logger.BatchHandler.Handle -> the initLogger
	// BeforeAddFunc at core/base.go:1492), while every settings save rewrites
	// the same fields under that lock (core.Settings.loadParam through
	// ReloadSettings). The settings boundaries test below deliberately issues
	// denied settings PATCHes next to accepted ones, so the asynchronous
	// request logs overlap the reload write and -race reports a data race
	// whose read and write sides both live in PocketBase; no PBVex-local
	// lock can synchronize them. Logs.MaxDays = 0 is the documented
	// activity-logger switch (see apis.activityLogger): no request is logged,
	// the batch store stays empty, and the settings are then only touched by
	// the goroutine that also performs the reloads. Production binaries keep
	// the default retention and inherit this unresolved upstream race: a
	// settings save concurrent with request logging can still be reported by
	// -race (and is formally a torn read per the Go memory model) until
	// PocketBase synchronizes its logger-side settings reads.
	app.Settings().Logs.MaxDays = 0
	if err := app.Save(app.Settings()); err != nil {
		app.Cleanup()
		t.Fatalf("failed to disable the request activity logger: %v", err)
	}
	path := filepath.Join(t.TempDir(), "p.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		app.Cleanup()
		t.Fatal(err)
	}
	service := hosting.NewReferenceService(64)
	if err := service.SetPolicy("p1", map[string]bool{hosting.FunctionExecute: true}); err != nil {
		t.Fatal(err)
	}
	var quota *storagequota.ReferenceQuotaService
	var provider http.Handler = service
	if withQuota {
		quota = storagequota.NewReferenceQuotaService(64, hostedDemoQuotaCapacity)
		provider = composedProviderHandler(service, quota)
	}
	server := &http.Server{Handler: provider, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(l)

	cfg := DefaultConfig()
	cfg.Runtime.PoolSize = 2
	cfg.Runtime.Timeout = 2 * time.Second
	cfg.Deploy.HistoryLimit = 5
	cfg.Storage.MaxFileSize = 1 << 20
	cfg.Hosting.Enabled = true
	cfg.Hosting.SocketPath = path
	if mutate != nil {
		mutate(&cfg)
	}
	if _, _, err := RegisterCore(app, cfg); err != nil {
		server.Close()
		app.Cleanup()
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		server.Close()
		app.Cleanup()
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		server.Close()
		app.Cleanup()
		t.Fatalf("failed to bootstrap: %v", err)
	}
	if err := app.RunAllMigrations(); err != nil {
		server.Close()
		app.Cleanup()
		t.Fatalf("failed to run migrations: %v", err)
	}
	router, err := apis.NewRouter(app)
	if err != nil {
		server.Close()
		app.Cleanup()
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}); err != nil {
		server.Close()
		app.Cleanup()
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		server.Close()
		app.Cleanup()
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close(); app.Cleanup() })
	return app, service, quota, server, mux
}

func hostedJSONRequest(t *testing.T, mux http.Handler, app *tests.TestApp, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	token := superuserToken(t, app)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestHostManagedStorageShadowAndPersistedBaseline(t *testing.T) {
	app, _, _, _, mux := newHostedTestApp(t, func(c *Config) {
		c.HostManagedStorageS3 = hostedManagedStorage()
	}, true)

	// Runtime view: the in-memory settings carry the host values so native
	// consumers (filesystems, mailer) use them.
	if got := app.Settings().S3; got != hostedManagedStorage() {
		t.Fatalf("shadowed S3 settings = %+v", got)
	}
	if got := app.Settings().Backups.S3; got != hostedManagedStorage() {
		t.Fatalf("shadowed backups S3 settings = %+v", got)
	}

	// Persistence view: the stored row keeps the neutral baseline, so backup
	// archives and restores never carry the host configuration.
	row := readPersistedSettings(t, app)
	if row.S3.Enabled || row.S3.Secret != "" || row.S3.Endpoint != "" || row.S3.Bucket != "" {
		t.Fatalf("persisted S3 settings are not neutral: %+v", row.S3)
	}
	if row.Backups.S3.Enabled || row.Backups.S3.Secret != "" {
		t.Fatalf("persisted backups S3 settings are not neutral: %+v", row.Backups.S3)
	}

	// API view: secrets are masked; the non-secret connection fields of the
	// active (shadowed) settings render normally.
	rr := hostedJSONRequest(t, mux, app, http.MethodGet, "/api/settings", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("settings list status = %d body %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, hostS3Secret) {
		t.Fatal("settings list exposed the host S3 secret")
	}
	if !strings.Contains(body, hostS3Bucket) {
		t.Fatal("settings list did not render the active managed storage")
	}
}

func TestHostManagedSMTPShadowAndMailClient(t *testing.T) {
	app, _, _, _, mux := newHostedTestApp(t, func(c *Config) {
		c.SMTP = hostedManagedSMTP()
	}, true)

	if !app.Settings().SMTP.Enabled || app.Settings().SMTP.Password != hostSMTPPass {
		t.Fatalf("shadowed SMTP settings = %+v", app.Settings().SMTP)
	}

	client, ok := app.NewMailClient().(*mailer.SMTPClient)
	if !ok {
		t.Fatalf("mail client type = %T", app.NewMailClient())
	}
	if client.Host != "smtp.host-managed.example" || client.Password != hostSMTPPass || client.Port != 587 {
		t.Fatalf("mail client does not use the host configuration: %+v", client)
	}

	row := readPersistedSettings(t, app)
	if row.SMTP.Enabled || row.SMTP.Password != "" || row.SMTP.Host != "" {
		t.Fatalf("persisted SMTP settings are not neutral: %+v", row.SMTP)
	}

	rr := hostedJSONRequest(t, mux, app, http.MethodGet, "/api/settings", "")
	if strings.Contains(rr.Body.String(), hostSMTPPass) {
		t.Fatal("settings list exposed the host SMTP password")
	}
}

func TestHostManagedSettingsEditBoundaries(t *testing.T) {
	app, service, _, _, mux := newHostedTestApp(t, func(c *Config) {
		c.HostManagedStorageS3 = hostedManagedStorage()
		c.SMTP = hostedManagedSMTP()
	}, true)

	// An unrelated settings edit is allowed and keeps the managed values out
	// of the database while the runtime keeps using them.
	rr := hostedJSONRequest(t, mux, app, http.MethodPatch, "/api/settings", `{"meta":{"appName":"tenant-edited"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("unrelated settings edit status = %d body %s", rr.Code, rr.Body.String())
	}
	if app.Settings().Meta.AppName != "tenant-edited" {
		t.Fatalf("app name not updated in memory: %q", app.Settings().Meta.AppName)
	}
	if got := app.Settings().S3; got != hostedManagedStorage() {
		t.Fatalf("shadow lost after unrelated save: %+v", got)
	}
	row := readPersistedSettings(t, app)
	if row.Meta.AppName != "tenant-edited" {
		t.Fatalf("app name not persisted: %+v", row.Meta)
	}
	if row.S3.Enabled || row.S3.Secret != "" || row.SMTP.Enabled || row.SMTP.Password != "" {
		t.Fatalf("managed values leaked into the persisted row: %+v", row)
	}

	// Managed categories reject any submitted change, including disabling.
	for _, tc := range []struct {
		name string
		body string
	}{
		{"disable managed s3", `{"s3":{"enabled":false}}`},
		{"repoint managed s3", `{"s3":{"enabled":true,"bucket":"tenant","region":"us-east-1","endpoint":"https://tenant.example","accessKey":"ta","secret":"ts"}}`},
		{"disable managed backups s3", `{"backups":{"cron":"","cronMaxKeep":3,"s3":{"enabled":false}}}`},
		{"disable managed smtp", `{"smtp":{"enabled":false,"host":"smtp.tenant.example","port":587}}`},
	} {
		rr := hostedJSONRequest(t, mux, app, http.MethodPatch, "/api/settings", tc.body)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d body %s", tc.name, rr.Code, rr.Body.String())
		}
	}
	row = readPersistedSettings(t, app)
	if row.S3.Enabled || row.SMTP.Enabled {
		t.Fatalf("denied edits changed the persisted row: %+v", row)
	}
	if got := app.Settings().S3; got != hostedManagedStorage() {
		t.Fatalf("denied edits changed the shadow: %+v", got)
	}

	// The backups cron fields stay editable under the existing capability.
	rr = hostedJSONRequest(t, mux, app, http.MethodPatch, "/api/settings", `{"backups":{"cron":"* * * * *","cronMaxKeep":2}}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("backups cron edit without grant status = %d", rr.Code)
	}
	if err := service.SetPolicy("v2", map[string]bool{hosting.SettingsBackups: true}); err != nil {
		t.Fatal(err)
	}
	rr = hostedJSONRequest(t, mux, app, http.MethodPatch, "/api/settings", `{"backups":{"cron":"* * * * *","cronMaxKeep":2}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("backups cron edit with grant status = %d body %s", rr.Code, rr.Body.String())
	}
	row = readPersistedSettings(t, app)
	if row.Backups.Cron != "* * * * *" || row.Backups.CronMaxKeep != 2 {
		t.Fatalf("backups cron not persisted: %+v", row.Backups)
	}
	if row.Backups.S3.Enabled || row.Backups.S3.Secret != "" {
		t.Fatalf("backups s3 leaked into the persisted row: %+v", row.Backups.S3)
	}
	if got := app.Settings().Backups.S3; got != hostedManagedStorage() {
		t.Fatalf("backups shadow lost after save: %+v", got)
	}
}

func TestHostManagedWholeSettingsSaveStaysNeutral(t *testing.T) {
	app, _, _, _, _ := newHostedTestApp(t, func(c *Config) {
		c.HostManagedStorageS3 = hostedManagedStorage()
		c.SMTP = hostedManagedSMTP()
	}, true)

	// Internal code paths persist the whole in-memory settings object (for
	// example the rate limit relabeling hook in PocketBase). The managed
	// categories must be neutralized on their way to the database.
	if err := app.Save(app.Settings()); err != nil {
		t.Fatal(err)
	}
	row := readPersistedSettings(t, app)
	if row.S3.Enabled || row.S3.Secret != "" || row.SMTP.Enabled || row.SMTP.Password != "" || row.Backups.S3.Enabled {
		t.Fatalf("whole-settings save persisted managed values: %+v", row)
	}
	if got := app.Settings().S3; got != hostedManagedStorage() {
		t.Fatalf("shadow lost after whole-settings save: %+v", got)
	}
	if !app.Settings().SMTP.Enabled || app.Settings().SMTP.Password != hostSMTPPass {
		t.Fatalf("smtp shadow lost after whole-settings save: %+v", app.Settings().SMTP)
	}
}

func TestHostManagedConfigRequiresHosting(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	cfg := DefaultConfig()
	cfg.HostManagedStorageS3 = hostedManagedStorage()
	if _, _, err := RegisterCore(app, cfg); err == nil {
		t.Fatal("managed storage accepted without hosting integration")
	}
}

func TestHostManagedShadowFailureFailsReload(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	managed := registerManagedInjection(hostedManagedStorage(), hostedManagedSMTP())
	if managed == nil {
		t.Fatal("expected a managed injection")
	}
	managed.shadow = func(*core.Settings) error { return errors.New("synthetic shadow failure") }
	if err := managed.register(app); err != nil {
		t.Fatal(err)
	}

	// The reload must fail closed: a shadow failure would otherwise leave the
	// in-memory settings on stale or neutral values, silently deactivating
	// managed storage and mail. Failing the reload fails bootstraps and
	// settings saves.
	if err := app.ReloadSettings(); err == nil {
		t.Fatal("shadow failure did not fail the settings reload")
	}
	// The values that were live before the failed reload remain in memory;
	// nothing partially neutralized them.
	if app.Settings().S3.Enabled {
		t.Fatal("shadow failure left unexpected managed values in memory")
	}
}

func TestHostManagedBaselineRewritesPersistedValues(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	// Persist tenant-owned storage/mail configuration while hosting is not
	// registered yet: the onboarding state the baseline rewrite targets.
	app.Settings().S3 = core.S3Config{Enabled: true, Bucket: "tenant-bucket", Region: "us-east-1", Endpoint: "https://s3.tenant.example", AccessKey: "tenant-ak", Secret: "tenant-secret"}
	app.Settings().Backups.S3 = app.Settings().S3
	app.Settings().SMTP = core.SMTPConfig{Enabled: true, Host: "smtp.tenant.example", Port: 587, Password: "tenant-smtp-secret"}
	if err := app.Save(app.Settings()); err != nil {
		t.Fatal(err)
	}
	row := readPersistedSettings(t, app)
	if !row.S3.Enabled || row.S3.Secret != "tenant-secret" || !row.SMTP.Enabled {
		t.Fatal("precondition: tenant values not persisted", row.S3.Enabled, row.SMTP.Enabled)
	}

	// Enable managed hosting and bootstrap: the baseline rewrite must replace
	// the persisted managed categories and keep the shadow active.
	path := filepath.Join(t.TempDir(), "p.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	service := hosting.NewReferenceService(8)
	quota := storagequota.NewReferenceQuotaService(8, hostedDemoQuotaCapacity)
	server := &http.Server{Handler: composedProviderHandler(service, quota)}
	go server.Serve(l)
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Hosting.Enabled = true
	cfg.Hosting.SocketPath = path
	cfg.HostManagedStorageS3 = hostedManagedStorage()
	cfg.SMTP = hostedManagedSMTP()
	if _, _, err := RegisterCore(app, cfg); err != nil {
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap with preexisting persisted values failed: %v", err)
	}

	row = readPersistedSettings(t, app)
	if row.S3.Enabled || row.S3.Secret != "" || row.SMTP.Enabled || row.SMTP.Password != "" {
		t.Fatalf("baseline rewrite did not neutralize the persisted row: %+v", row)
	}
	if got := app.Settings().S3; got != hostedManagedStorage() {
		t.Fatalf("managed shadow not active after baseline rewrite: %+v", got)
	}
	if !app.Settings().SMTP.Enabled || app.Settings().SMTP.Password != hostSMTPPass {
		t.Fatalf("managed smtp shadow not active after baseline rewrite: %+v", app.Settings().SMTP)
	}
}
