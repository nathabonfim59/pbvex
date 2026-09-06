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

func TestNativeRecordDeleteRetainsUsageUntilReconciliation(t *testing.T) {
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
	// Upstream deletes the record files asynchronously, so absence cannot
	// be verified synchronously: the conservative hard-quota behavior
	// retains the settled usage (no credit) for provider reconciliation.
	_, credited, _ := obs.totals()
	if credited != 0 {
		t.Fatalf("record deletion must not free the allowance, credited=%d (log: %v)", credited, obs.callLog())
	}
	if n := obs.callCount("credit("); n != 0 {
		t.Fatalf("expected no credit reports on record deletion, log: %v", obs.callLog())
	}
}

func TestNativeConfirmedRemovalVerificationIsConservative(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, svc, collection := newNativeQuotaApp(t, obs)

	record := core.NewRecord(collection)
	record.Set("slug", "verify")
	record.Set("doc", []*filesystem.File{nativeTempFile(t, 150)})
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	stored, err := app.FindRecordById(collection, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	names := nativePlainNames(stored.GetRaw("doc"))
	if len(names) != 1 {
		t.Fatalf("expected one stored file, got %v", names)
	}
	captured, ok := svc.nativeRemovalOf(app, stored, names[0])
	if !ok || captured.Size != 150 {
		t.Fatalf("expected captured removal of 150 bytes, got %+v ok=%v", captured, ok)
	}

	// While the object is still present (upstream removal failed), the
	// verification must free nothing.
	if freed := svc.nativeConfirmedRemovalBytes(app, stored, captured); freed != 0 {
		t.Fatalf("existing object must not be credited, freed=%d", freed)
	}
	// After the object is verifiably gone the captured bytes are freed.
	fs, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete(stored.BaseFilesPath() + "/" + names[0]); err != nil {
		t.Fatal(err)
	}
	fs.Close()
	if freed := svc.nativeConfirmedRemovalBytes(app, stored, captured); freed != 150 {
		t.Fatalf("expected confirmed removal to free 150 bytes, got %d", freed)
	}
	_ = obs
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

func TestNativeThumbGateFailsClosedWhenQuotaEnforced(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, svc, collection := newNativeQuotaApp(t, obs)
	newNativePhotoCollection(t, app, collection)
	record := newNativePhotoRecord(t, app, collection, "deniedphoto", 16, 12)
	stored := record.GetStringSlice("photo")
	_ = svc

	mux := nativeServeRouter(t, app)
	url := fmt.Sprintf("/api/files/qfiles/%s/%s?thumb=64x64", record.Id, stored[0])

	// Snapshot the upload-phase reservations: the denied generation must
	// not add any quota activity of its own.
	beforeLog := len(obs.callLog())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, url, nil))
	// No exact reservation is possible on the unhookable native generation
	// path: it must be denied before any bytes are written.
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected fail-closed 500 for native generation, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(obs.callLog()) != beforeLog {
		t.Fatalf("the native gate must not pretend an approximate reservation, log: %v", obs.callLog())
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

func TestNativeThumbCachedVariantServesWhenQuotaEnforced(t *testing.T) {
	obs := newFakeQuotaObserver()
	app, svc, collection := newNativeQuotaApp(t, obs)
	newNativePhotoCollection(t, app, collection)
	record := newNativePhotoRecord(t, app, collection, "cached", 32, 24)
	stored := record.GetStringSlice("photo")
	_ = svc

	// Pre-create the variant on the backend (as an earlier generation
	// would have): it must keep being served under enforced quotas.
	fs, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	thumbKey := fmt.Sprintf("%s/thumbs_%s/64x64_%s", record.BaseFilesPath(), stored[0], stored[0])
	if err := fs.CreateThumb(record.BaseFilesPath()+"/"+stored[0], thumbKey, "64x64"); err != nil {
		t.Fatal(err)
	}
	fs.Close()

	mux := nativeServeRouter(t, app)
	rr := httptest.NewRecorder()
	before := len(obs.callLog())
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/qfiles/%s/%s?thumb=64x64", record.Id, stored[0]), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected cached variant to be served, got %d", rr.Code)
	}
	if len(obs.callLog()) != before {
		t.Fatalf("serving a cached variant must not reserve, log: %v", obs.callLog())
	}
}

func TestNativeThumbStandaloneStillGenerates(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	cfg := DefaultConfig()
	svc, err := NewService(app, NewRepo(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	// No observer attached: installing the hooks must be a no-op so the
	// native generation path behaves exactly as upstream.
	if err := svc.InstallNativeQuotaHooks(app); err != nil {
		t.Fatal(err)
	}
	collection := core.NewCollection(core.CollectionTypeBase, "qfiles")
	collection.Fields.Add(&core.TextField{Name: "slug", Max: 100})
	collection.Fields.Add(&core.FileField{Name: "photo", MaxSelect: 1, MaxSize: 1 << 20, Thumbs: []string{"64x64"}})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	record := newNativePhotoRecord(t, app, collection, "standalone", 32, 24)
	stored := record.GetStringSlice("photo")

	mux := nativeServeRouter(t, app)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/qfiles/%s/%s?thumb=64x64", record.Id, stored[0]), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected standalone generation to work, got %d: %s", rr.Code, rr.Body.String())
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
	if !exists {
		t.Fatal("expected the standalone variant to be generated")
	}
}

func TestNativeThumbGateConcurrentRequestsAllFailClosed(t *testing.T) {
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
		if code != http.StatusInternalServerError {
			t.Fatalf("expected every concurrent request to fail closed, got %d", code)
		}
	}
	// No request may reserve or write on this path.
	before := obs.callCount("reserve(")
	_ = before
	if n := obs.callCount("settle(native-variant-"); n != 0 {
		t.Fatalf("expected no gate settlements, log: %v", obs.callLog())
	}
	obs.mu.Lock()
	variants := 0
	for id := range obs.reservations {
		if strings.HasPrefix(id, "variant-") || strings.HasPrefix(id, "native-variant") {
			variants++
		}
	}
	obs.mu.Unlock()
	if variants != 0 {
		t.Fatalf("expected no gate reservations, found %d", variants)
	}
	fs, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	objects, err := fs.List(record.BaseFilesPath() + "/thumbs_" + stored[0] + "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) > 0 {
		t.Fatalf("no variant may be written on the denied path, found %d objects", len(objects))
	}
}

var (
	_ = context.Background
	_ = strings.TrimSpace
	_ = atomic.Bool{}
)
