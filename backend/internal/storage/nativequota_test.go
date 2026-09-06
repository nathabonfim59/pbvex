package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// newNativeQuotaApp returns a test app, a storage service with the fake
// observer attached and native hooks installed, plus a collection with a
// multi-file field and a unique-indexed text field for write failures.
func newNativeQuotaApp(t *testing.T, obs QuotaObserver) (*tests.TestApp, *Service, *core.Collection) {
	t.Helper()
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)

	cfg := DefaultConfig()
	cfg.MaxFileSize = 1 << 20
	svc, err := NewService(app, NewRepo(), cfg)
	if err != nil {
		t.Fatalf("failed to create storage service: %v", err)
	}
	svc.SetQuotaObserver(obs)
	if err := svc.InstallNativeQuotaHooks(app); err != nil {
		t.Fatalf("failed to install native hooks: %v", err)
	}

	collection := core.NewCollection(core.CollectionTypeBase, "qfiles")
	collection.Fields.Add(&core.TextField{Name: "slug", Max: 100})
	collection.Fields.Add(&core.FileField{Name: "doc", MaxSelect: 2, MaxSize: 1 << 20})
	collection.AddIndex("idx_qfiles_slug", true, "slug", "")
	if err := app.Save(collection); err != nil {
		t.Fatalf("failed to save collection: %v", err)
	}
	return app, svc, collection
}

func nativeTempFile(t *testing.T, size int) *filesystem.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("f%d.txt", size))
	out := make([]byte, size)
	for i := range out {
		out[i] = byte('a' + i%26)
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := filesystem.NewFileFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNativeUploadReservesAndSettles(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, svc, collection := newNativeQuotaApp(t, obs)

	record := core.NewRecord(collection)
	record.Set("slug", "one")
	record.Set("doc", []*filesystem.File{
		nativeTempFile(t, 100),
		nativeTempFile(t, 240),
	})

	if err := app.Save(record); err != nil {
		t.Fatalf("native save failed: %v", err)
	}
	// One reservation of the exact worst-case total before the write, one
	// settlement of the stored bytes.
	if n := obs.callCount("reserve("); n != 1 {
		t.Fatalf("expected one reservation, log: %v", obs.callLog())
	}
	if want := fmt.Sprintf(",%s,%d)", QuotaPurposeUpload, 100+240); !obs.hasCall(want) {
		t.Fatalf("expected worst-case total %d, log: %v", 100+240, obs.callLog())
	}
	settled, _, _ := obs.totals()
	if settled != 100+240 {
		t.Fatalf("expected settled %d, got %d", 100+240, settled)
	}
	if inflight := obs.inflightCount(); inflight != 0 {
		t.Fatalf("expected no inflight reservations, got %d", inflight)
	}
	stored, err := app.FindRecordById(collection, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := stored.GetStringSlice("doc"); len(got) != 2 {
		t.Fatalf("expected two stored files, got %v", got)
	}
	_ = svc
}

func TestNativeSavesWithoutFilesDoNotReserve(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, _, collection := newNativeQuotaApp(t, obs)

	record := core.NewRecord(collection)
	record.Set("slug", "nofiles")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if n := obs.callCount("reserve("); n != 0 {
		t.Fatalf("saves without file changes must not reserve, log: %v", obs.callLog())
	}
}

func TestNativeUploadDeniedFailsClosed(t *testing.T) {
	obs := newFakeQuotaObserver()
	obs.denyUpload = true
	app, _, collection := newNativeQuotaApp(t, obs)

	record := core.NewRecord(collection)
	record.Set("slug", "denied")
	record.Set("doc", nativeTempFile(t, 64))

	err := app.Save(record)
	if err == nil {
		t.Fatal("expected denied native upload to fail the save")
	}
	if !errors.Is(err, ErrQuotaDenied) {
		t.Fatalf("expected quota denial, got %v", err)
	}
	if _, err := app.FindRecordById(collection, record.Id); err == nil {
		t.Fatal("denied record must not be persisted")
	}
	if n := obs.callCount("settle("); n != 0 {
		t.Fatal("denied upload must not settle")
	}
}

func TestNativeUploadUnavailableFailsClosed(t *testing.T) {
	obs := newFakeQuotaObserver()
	obs.failReserve = ErrQuotaUnavailable
	app, _, collection := newNativeQuotaApp(t, obs)

	record := core.NewRecord(collection)
	record.Set("slug", "unavailable")
	record.Set("doc", nativeTempFile(t, 64))

	if err := app.Save(record); err == nil {
		t.Fatal("expected unavailable quota to fail the save")
	}
	if _, err := app.FindRecordById(collection, record.Id); err == nil {
		t.Fatal("unavailable quota must not persist the record")
	}
}

func TestNativeUploadFailureReleasesVerified(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, _, collection := newNativeQuotaApp(t, obs)

	// First save succeeds and occupies the unique slug.
	first := core.NewRecord(collection)
	first.Set("slug", "dup")
	first.Set("doc", nativeTempFile(t, 32))
	if err := app.Save(first); err != nil {
		t.Fatal(err)
	}

	// The second save uploads its file (upstream writes files before the
	// DB insert), then fails on the unique slug; upstream cleanup deletes
	// the upload and the reservation must be released.
	second := core.NewRecord(collection)
	second.Set("slug", "dup")
	second.Set("doc", nativeTempFile(t, 48))
	if err := app.Save(second); err == nil {
		t.Fatal("expected unique-index failure")
	}
	if !obs.hasCall("release(") {
		t.Fatalf("expected release after verified cleanup, log: %v", obs.callLog())
	}
	if inflight := obs.inflightCount(); inflight != 0 {
		t.Fatalf("expected no inflight reservations, got %d", inflight)
	}
}

func TestNativeUpdateReplacementSettlesAndCredits(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, _, collection := newNativeQuotaApp(t, obs)

	record := core.NewRecord(collection)
	record.Set("slug", "replace")
	record.Set("doc", nativeTempFile(t, 120))
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}

	stored, err := app.FindRecordById(collection, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	stored.Set("doc", nativeTempFile(t, 340))
	if err := app.Save(stored); err != nil {
		t.Fatal(err)
	}

	// The replacement settles the new upload and credits the confirmed
	// removal of the replaced file.
	settled, credited, _ := obs.totals()
	if settled != 120+340 {
		t.Fatalf("expected settled %d, got %d", 120+340, settled)
	}
	if credited != 120 {
		t.Fatalf("expected credited %d for the replaced file, got %d (log: %v)", 120, credited, obs.callLog())
	}
	if n := obs.callCount("credit("); n != 1 {
		t.Fatalf("expected one credit, log: %v", obs.callLog())
	}
}

func TestNativeRecordDeleteCredits(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, _, collection := newNativeQuotaApp(t, obs)

	record := core.NewRecord(collection)
	record.Set("slug", "gone")
	record.Set("doc", []*filesystem.File{nativeTempFile(t, 210)})
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	// Real delete flows load the record from the database first.
	stored, err := app.FindRecordById(collection, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Delete(stored); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	_, credited, _ := obs.totals()
	if credited != 210 {
		t.Fatalf("expected credited %d, got %d (log: %v)", 210, credited, obs.callLog())
	}
}

// newNativePhotoCollection extends the qfiles collection with a
// thumbs-declaring image field.
func newNativePhotoCollection(t *testing.T, app *tests.TestApp, collection *core.Collection) {
	t.Helper()
	collection.Fields.Add(&core.FileField{Name: "photo", MaxSelect: 1, MaxSize: 1 << 20, Thumbs: []string{"64x64"}})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
}

func newNativePhotoRecord(t *testing.T, app *tests.TestApp, collection *core.Collection, slug string, width, height int) *core.Record {
	t.Helper()
	photoPath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(photoPath, testPNGBytes(t, width, height), 0600); err != nil {
		t.Fatal(err)
	}
	photo, err := filesystem.NewFileFromPath(photoPath)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("slug", slug)
	record.Set("photo", photo)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

// nativeServeRouter builds the real apis router with the app's OnServe
// handlers (including the native thumbnail gate) and returns the mux.
func nativeServeRouter(t *testing.T, app core.App) http.Handler {
	t.Helper()
	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	serveEvent := new(core.ServeEvent)
	serveEvent.App = app
	serveEvent.Router = baseRouter
	if err := app.OnServe().Trigger(serveEvent, func(e *core.ServeEvent) error { return e.Next() }); err != nil {
		t.Fatal(err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

func TestNativeThumbGateReservesGeneratesAndSettles(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, svc, collection := newNativeQuotaApp(t, obs)
	newNativePhotoCollection(t, app, collection)
	record := newNativePhotoRecord(t, app, collection, "photo", 32, 24)
	stored := record.GetStringSlice("photo")
	if len(stored) != 1 {
		t.Fatalf("expected one stored photo, got %v", stored)
	}
	_ = svc

	mux := nativeServeRouter(t, app)
	url := fmt.Sprintf("/api/files/qfiles/%s/%s?thumb=64x64", record.Id, stored[0])

	// First request: the gate reserves, upstream generates, settle follows.
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, url, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected served thumb, got status %d: %s", rr.Code, rr.Body.String())
	}
	if n := obs.callCount("settle(native-variant-"); n != 1 {
		t.Fatalf("expected one native variant settlement, log: %v", obs.callLog())
	}
	settled, _, _ := obs.totals()
	if settled <= int64(len(testPNGBytes(t, 32, 24))) {
		t.Fatalf("expected variant bytes settled beyond the original, settled=%d", settled)
	}

	// A second request is served from the existing variant: no new write.
	before := obs.callCount("reserve(")
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, url, nil))
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected cached thumb, got %d", rr2.Code)
	}
	if after := obs.callCount("reserve("); after != before {
		t.Fatal("existing variant must not reserve again")
	}

	// Requests without a thumb parameter never consult the gate.
	rr3 := httptest.NewRecorder()
	mux.ServeHTTP(rr3, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/qfiles/%s/%s", record.Id, stored[0]), nil))
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected served original, got %d", rr3.Code)
	}
}

func TestNativeThumbGateDeniedFailsClosed(t *testing.T) {
	obs := newFakeQuotaObserver()
	obs.denyVariant = true
	app, svc, collection := newNativeQuotaApp(t, obs)
	newNativePhotoCollection(t, app, collection)
	record := newNativePhotoRecord(t, app, collection, "deniedphoto", 16, 12)
	stored := record.GetStringSlice("photo")
	_ = svc

	mux := nativeServeRouter(t, app)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/qfiles/%s/%s?thumb=64x64", record.Id, stored[0]), nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected fail-closed 500 for denied variant, got %d: %s", rr.Code, rr.Body.String())
	}
	fs, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	exists, err := fs.Exists(fmt.Sprintf("%s/thumbs_%s/64x64_%s", record.BaseFilesPath(), stored[0], stored[0]))
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("denied variant must not be written")
	}
}

func TestNativeThumbGateConcurrentRequestsShareOneReservation(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, svc, collection := newNativeQuotaApp(t, obs)
	newNativePhotoCollection(t, app, collection)
	record := newNativePhotoRecord(t, app, collection, "concurrent", 32, 24)
	stored := record.GetStringSlice("photo")
	_ = svc

	mux := nativeServeRouter(t, app)
	url := fmt.Sprintf("/api/files/qfiles/%s/%s?thumb=64x64", record.Id, stored[0])

	const total = 5
	var wg sync.WaitGroup
	codes := make(chan int, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, url, nil))
			codes <- rr.Code
		}()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("expected all requests served, got %d", code)
		}
	}
	// All requests share one generation: exactly one reservation and one
	// settlement for the variant.
	if n := obs.callCount("reserve(native-variant-"); n != 1 {
		t.Fatalf("expected one shared variant reservation, log: %v", obs.callLog())
	}
	if n := obs.callCount("settle(native-variant-"); n != 1 {
		t.Fatalf("expected one variant settlement, log: %v", obs.callLog())
	}
}

var (
	_ = context.Background
	_ = strings.TrimSpace
	_ = atomic.Bool{}
)
