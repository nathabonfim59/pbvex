package pbvex

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestHostingSettingsAndRestore(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	path := filepath.Join(t.TempDir(), "p.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	service := hosting.NewReferenceService(10)
	server := &http.Server{Handler: service}
	go server.Serve(l)
	defer server.Close()
	client, _, err := newHostingClient(hosting.Config{Enabled: true, SocketPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := registerHosting(app, client, nil); err != nil {
		t.Fatal(err)
	}
	check := func(changed bool) error {
		old := &core.Settings{}
		next := &core.Settings{}
		if changed {
			next.S3.Bucket = "changed"
		} else {
			next.Meta.AppName = "ordinary edit"
		}
		e := &core.SettingsUpdateRequestEvent{RequestEvent: &core.RequestEvent{App: app, Request: httptest.NewRequest(http.MethodPatch, "/api/settings", nil)}, OldSettings: old, NewSettings: next}
		return app.OnSettingsUpdateRequest().Trigger(e)
	}
	if err := check(false); err != nil {
		t.Fatal("unrelated edit blocked", err)
	}
	if err := check(true); err == nil {
		t.Fatal("protected edit allowed")
	}
	if err := service.SetPolicy("v2", map[string]bool{hosting.SettingsStorage: true, hosting.BackupRestore: true}); err != nil {
		t.Fatal(err)
	}
	if err := check(true); err != nil {
		t.Fatal("dynamic allow not applied", err)
	}
	executed := false
	err = app.OnBackupRestore().Trigger(&core.BackupEvent{App: app, Context: context.Background()}, func(e *core.BackupEvent) error { executed = true; return nil })
	if err == nil || executed {
		t.Fatal("restore side effect allowed")
	}
	server.Close()
	if err := check(true); err == nil {
		t.Fatal("unavailable provider allowed edit")
	}
}
func TestHostingDisabledAndUnavailable(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	client, _, err := newHostingClient(hosting.Config{})
	if err != nil || client != nil {
		t.Fatal("disabled configuration must not create a client", err)
	}
	if err := registerHosting(app, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newHostingClient(hosting.Config{Enabled: true, SocketPath: filepath.Join(t.TempDir(), "absent")}); err == nil {
		t.Fatal("enabled startup allowed missing provider")
	}
}
