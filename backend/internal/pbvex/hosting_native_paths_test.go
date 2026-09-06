package pbvex

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// writeBackupArchive places a minimal archive directly in the backups
// directory. Backup creation itself is denied while hosting is enabled (it
// would write unreserved bytes), so download-gate tests place a
// pre-existing archive the way a pre-hosting or provider-side artifact
// would exist.
func writeBackupArchive(t *testing.T, app *tests.TestApp, name string) {
	t.Helper()
	dir := filepath.Join(app.DataDir(), "backups")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("data.db"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestHostedBackupCreationDeniedDespiteGrant(t *testing.T) {
	// Backup archives are written with no pre-write size bound, so they can
	// never be covered by the storage byte quota. Creation is denied
	// outright: a backup.create grant must not enable it, the API refuses
	// before scheduling, and the OnBackupCreate hook refuses the scheduled
	// and programmatic paths.
	app, service, _, _, mux := newHostedTestApp(t, func(c *Config) {
		c.SMTP = hostedManagedSMTP()
	}, true)
	if err := service.SetPolicy("v2", map[string]bool{hosting.BackupCreate: true, hosting.BackupDownload: true}); err != nil {
		t.Fatal(err)
	}

	rr := hostedJSONRequest(t, mux, app, http.MethodPost, "/api/backups", `{"name":"tenantbackup.zip"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("backup create with grant status = %d body %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "storage byte quota") {
		t.Fatalf("expected the explicit quota rationale, got %s", rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(app.DataDir(), "backups", "tenantbackup.zip")); !os.IsNotExist(err) {
		t.Fatal("denied backup create must not write an archive")
	}

	// The hook refusal covers scheduled and programmatic creation too.
	if err := app.CreateBackup(context.Background(), "scheduled.zip"); err == nil {
		t.Fatal("scheduled backup creation must be denied by the OnBackupCreate hook")
	}
	if _, err := os.Stat(filepath.Join(app.DataDir(), "backups", "scheduled.zip")); !os.IsNotExist(err) {
		t.Fatal("denied scheduled backup must not write an archive")
	}

	// The archive-neutrality intent of managed injection is still visible
	// in the artifact backups embed: the persisted settings row stays
	// neutral.
	row := readPersistedSettings(t, app)
	if row.SMTP.Enabled || row.SMTP.Password != "" || row.SMTP.Host != "" {
		t.Fatalf("persisted SMTP settings are not neutral: %+v", row.SMTP)
	}
}

func TestHostedBackupDownloadGatesEncodedPaths(t *testing.T) {
	// The download gate keys on the matched route pattern. A backup key that
	// reaches the route through percent-encoding (an encoded dot, or an
	// encoded slash that the wildcard matches as one escaped segment) must
	// stay gated; a raw URL-path check would miss the encoded-slash form.
	app, service, _, _, mux := newHostedTestApp(t, nil, true)
	writeBackupArchive(t, app, "encoded.zip")
	token := superuserFileToken(t, app)
	for _, key := range []string{"encoded%2Ezip", "no-such%2Fkey.zip"} {
		rr := hostedJSONRequest(t, mux, app, http.MethodGet, "/api/backups/"+key+"?token="+token, "")
		if rr.Code != http.StatusForbidden {
			t.Fatalf("encoded download %s status = %d body %s", key, rr.Code, rr.Body.String())
		}
	}
	// With the grant, the plain route serves the archive.
	if err := service.SetPolicy("v2", map[string]bool{hosting.BackupDownload: true}); err != nil {
		t.Fatal(err)
	}
	rr := hostedJSONRequest(t, mux, app, http.MethodGet, "/api/backups/encoded.zip?token="+token, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("plain download with grant status = %d body %s", rr.Code, rr.Body.String())
	}
}

func TestHostedBackupDownloadFailsClosedWithoutProvider(t *testing.T) {
	app, _, _, server, mux := newHostedTestApp(t, nil, true)
	writeBackupArchive(t, app, "offline.zip")
	token := superuserFileToken(t, app)
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	rr := hostedJSONRequest(t, mux, app, http.MethodGet, "/api/backups/offline.zip?token="+token, "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("backup download with unavailable provider status = %d", rr.Code)
	}
}

func TestHostedNativePathDenials(t *testing.T) {
	app, _, _, _, mux := newHostedTestApp(t, nil, true)

	// Direct SQL can read the persisted settings row and bypass every
	// record-level protection, so it is unavailable in hosted mode.
	rr := hostedJSONRequest(t, mux, app, http.MethodPost, "/api/sql", `{"query":"SELECT value FROM _params WHERE id='settings'"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("sql api status = %d body %s", rr.Code, rr.Body.String())
	}

	// Collection import is denied before any side effect. The real route is
	// registered with PUT; the gate keys on the matched route pattern, so
	// the real method is refused even though older raw-path checks would
	// have missed it.
	var collections []*core.Collection
	if err := app.CollectionQuery().All(&collections); err != nil {
		t.Fatal(err)
	}
	importBody, err := json.Marshal(map[string]any{
		"collections":   []map[string]any{{"name": "imported_missing", "fields": []map[string]any{}}},
		"deleteMissing": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rr = hostedJSONRequest(t, mux, app, http.MethodPut, "/api/collections/import", string(importBody))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("collections import status = %d body %s", rr.Code, rr.Body.String())
	}
	// A method without a registered route must not fire the gate (the mux
	// rejects it); this pins that the gate matches real routes only.
	rr = hostedJSONRequest(t, mux, app, http.MethodPost, "/api/collections/import", string(importBody))
	if rr.Code == http.StatusForbidden {
		t.Fatal("gate fired for an unregistered method")
	}
	after, err := func() ([]*core.Collection, error) {
		var list []*core.Collection
		err := app.CollectionQuery().All(&list)
		return list, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(collections) {
		t.Fatalf("denied import changed collections: %d -> %d", len(collections), len(after))
	}
	// The hook denial also covers programmatic callers before ImportCollections.
	executed := false
	err = app.OnCollectionsImportRequest().Trigger(&core.CollectionsImportRequestEvent{RequestEvent: &core.RequestEvent{App: app}, CollectionsData: []map[string]any{{"name": "x"}}, DeleteMissing: true}, func(e *core.CollectionsImportRequestEvent) error {
		executed = true
		return nil
	})
	if err == nil || executed {
		t.Fatal("collections import side effect allowed")
	}

	// Backup uploads have no purpose while restore is denied and would turn
	// the backups storage into arbitrary blob storage.
	rr = hostedJSONRequest(t, mux, app, http.MethodPost, "/api/backups/upload", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("backup upload status = %d", rr.Code)
	}

	// Restore refusals reach the caller instead of an optimistic success.
	rr = hostedJSONRequest(t, mux, app, http.MethodPost, "/api/backups/whatever.zip/restore", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("backup restore status = %d body %s", rr.Code, rr.Body.String())
	}

	// Backup listing stays available; it carries no configuration data.
	rr = hostedJSONRequest(t, mux, app, http.MethodGet, "/api/backups", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("backup list status = %d body %s", rr.Code, rr.Body.String())
	}
}

func TestHostedDiagnosticEndpointsDeniedOnlyWhenManaged(t *testing.T) {
	app, _, _, _, mux := newHostedTestApp(t, func(c *Config) {
		c.HostManagedStorageS3 = hostedManagedStorage()
		c.SMTP = hostedManagedSMTP()
	}, true)
	rr := hostedJSONRequest(t, mux, app, http.MethodPost, "/api/settings/test/s3", `{"filesystem":"storage"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("managed s3 test status = %d body %s", rr.Code, rr.Body.String())
	}
	rr = hostedJSONRequest(t, mux, app, http.MethodPost, "/api/settings/test/email", `{"email":"tenant@example.com"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("managed smtp test status = %d body %s", rr.Code, rr.Body.String())
	}

	// Without managed configuration the diagnostic endpoints keep their
	// normal behavior (they fail validation here, which proves the hosting
	// gate did not deny them).
	app2, _, _, _, mux2 := newHostedTestApp(t, nil, true)
	rr = hostedJSONRequest(t, mux2, app2, http.MethodPost, "/api/settings/test/s3", `{"filesystem":"storage"}`)
	if rr.Code == http.StatusForbidden {
		t.Fatal("unmanaged s3 test denied by hosting gate")
	}
	rr = hostedJSONRequest(t, mux2, app2, http.MethodPost, "/api/settings/test/email", `{"email":"tenant@example.com"}`)
	if rr.Code == http.StatusForbidden {
		t.Fatal("unmanaged smtp test denied by hosting gate")
	}
}

func TestStandaloneSMTPOverrideStillPersists(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	cfg := DefaultConfig()
	cfg.SMTP = hostedManagedSMTP()
	if _, _, err := RegisterCore(app, cfg); err != nil {
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	// Standalone keeps the documented PBVEX_SMTP_* behavior: the values are
	// persisted so the dashboard reflects them.
	row := readPersistedSettings(t, app)
	if !row.SMTP.Enabled || row.SMTP.Host != "smtp.host-managed.example" || row.SMTP.Password != hostSMTPPass {
		t.Fatalf("standalone smtp override not persisted: %+v", row.SMTP)
	}
}

func superuserFileToken(t *testing.T, app *tests.TestApp) string {
	t.Helper()
	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatalf("failed to find superuser: %v", err)
	}
	token, err := superuser.NewFileToken()
	if err != nil {
		t.Fatalf("failed to generate file token: %v", err)
	}
	return token
}
