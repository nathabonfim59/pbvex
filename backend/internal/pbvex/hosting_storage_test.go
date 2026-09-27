package pbvex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/nathabonfim59/pbvex/backend/hosting/storagequota"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/nathabonfim59/pbvex/backend/internal/storage"
)

// hostedDemoQuotaCapacity is the demo byte capacity of the composed test
// provider. Tests that need denial paths shrink it with SetCapacity.
const hostedDemoQuotaCapacity = 1 << 30

// composedProviderHandler serves the policy reference service and the
// storage byte quota reference service behind one socket — the same
// composition as backend/examples/policy-service: one /v1/hello listing
// both capabilities, /v1/storage/* for the quota routes, everything else
// for the policy routes.
func composedProviderHandler(policy *hosting.ReferenceService, quota *storagequota.ReferenceQuotaService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/hello":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(hosting.Hello{
				Version:        hosting.Version,
				Implementation: "pbvex-test-policy-storagequota",
				Capabilities:   []string{hosting.FunctionExecute, storagequota.CapabilityStorageReserve},
			})
		case strings.HasPrefix(r.URL.Path, "/v1/storage/"):
			quota.ServeHTTP(w, r)
		default:
			policy.ServeHTTP(w, r)
		}
	})
}

func hostedPNGBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// hostedCreateUploadToken creates a single-use upload token directly in the
// internal tokens collection, the same record shape GenerateUploadURL
// produces, so the router upload route can be exercised end to end.
func hostedCreateUploadToken(t *testing.T, app *tests.TestApp, maxSize int64, policy *storage.ImagePolicy) string {
	t.Helper()
	token, err := storage.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	storageID, err := storage.GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	rec := storage.TokenRecord{
		TokenHash: storage.HashToken(token),
		StorageID: storageID,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		MaxSize:   maxSize,
	}
	if policy != nil {
		rec.AllowedTypes = policy.MimeTypes
		rec.Policy = policy
	}
	if _, err := storage.NewRepo().CreateToken(schema.WithInternalContext(context.Background()), app, rec); err != nil {
		t.Fatalf("failed to create upload token: %v", err)
	}
	return token
}

// hostedPublicToken reads the stored public bearer token of an uploaded
// file so downloads can be exercised through the real public route.
func hostedPublicToken(t *testing.T, app *tests.TestApp, storageID string) string {
	t.Helper()
	var row struct {
		PublicToken string `db:"publicToken"`
	}
	if err := app.DB().Select("publicToken").From(schema.CollectionStorageFiles).
		Where(dbx.HashExp{"storageId": storageID}).One(&row); err != nil {
		t.Fatalf("failed to read public token: %v", err)
	}
	return row.PublicToken
}

func hostedUploadRequest(t *testing.T, mux http.Handler, uploadPath, contentType, filename string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, uploadPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Upload-Filename", filename)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func hostedMultipartRecordCreate(t *testing.T, mux http.Handler, app *tests.TestApp, collection string, fields map[string]string, files map[string][]byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for field, content := range files {
		fw, err := mw.CreateFormFile(field, field+".bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/collections/"+collection+"/records", &buf)
	req.Header.Set("Authorization", superuserToken(t, app))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func hostedCollectionRecordCount(t *testing.T, app *tests.TestApp, collection string) int64 {
	t.Helper()
	var rows []struct {
		N int64 `db:"n"`
	}
	if err := app.DB().Select("count(*) AS n").From(collection).All(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("unexpected count result rows = %d", len(rows))
	}
	return rows[0].N
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, cond func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

func TestHostedStartupRequiresStorageQuotaCapability(t *testing.T) {
	// A policy-only provider serves a handshake without the
	// storage.reserve capability. Enabled hosting enforces storage byte
	// quotas, so startup must fail instead of silently depending on a
	// provider that cannot account bytes (no silent downgrade, no flag).
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	path := filepath.Join(t.TempDir(), "p.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: hosting.NewReferenceService(8), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(l) }()
	t.Cleanup(func() { server.Close() })

	cfg := DefaultConfig()
	cfg.Hosting.Enabled = true
	cfg.Hosting.SocketPath = path
	if _, _, err := RegisterCore(app, cfg); err == nil {
		t.Fatal("expected startup failure against a provider without storage.reserve")
	}
}

func TestHostedStorageQuotaWiringSmoke(t *testing.T) {
	// Full startup smoke against the example-compatible composed provider:
	// PBVex uploads and lazy image variants go through the real router and
	// are reserved, settled and credited on the shared socket.
	app, _, quota, _, mux := newHostedTestApp(t, nil, true)

	// Plain upload through the real router: the staging cap is reserved
	// before the body is read and the exact size is settled at commit.
	body := bytes.Repeat([]byte("z"), 4096)
	token := hostedCreateUploadToken(t, app, 1<<20, nil)
	rr := hostedUploadRequest(t, mux, "/api/pbvex/storage/upload/"+token, "application/octet-stream", "smoke.bin", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("upload status = %d body %s", rr.Code, rr.Body.String())
	}
	var created struct {
		StorageID string `json:"storageId"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if got := quota.UsedBytes(); got != int64(len(body)) {
		t.Fatalf("expected settled usage %d, got %d", len(body), got)
	}

	// Image upload with a variant policy, then lazy variant generation
	// through the public route: the exact encoded size is reserved before
	// the persistent write and settled afterwards.
	encoded := hostedPNGBytes(t, 8, 6)
	policy := &storage.ImagePolicy{Kind: "image", Thumbs: []string{"4x4"}, MimeTypes: []string{"image/png"}}
	imgToken := hostedCreateUploadToken(t, app, 1<<20, policy)
	rr = hostedUploadRequest(t, mux, "/api/pbvex/storage/upload/"+imgToken, "image/png", "photo.png", encoded)
	if rr.Code != http.StatusOK {
		t.Fatalf("image upload status = %d body %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	beforeVariant := quota.UsedBytes()
	public := hostedPublicToken(t, app, created.StorageID)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pbvex/storage/public/"+public+"/blob.bin?thumb=4x4", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("variant download status = %d body %s", rr.Code, rr.Body.String())
	}
	if quota.UsedBytes() <= beforeVariant {
		t.Fatalf("expected the variant bytes to be settled, usage %d -> %d", beforeVariant, quota.UsedBytes())
	}
	// (The exact thumb key layout is asserted by the storage package tests;
	// here the usage delta is the wired-contract evidence.)

	// The tenant cannot exceed the provider's capacity through the same
	// API: shrink the host capacity and the next upload is denied closed
	// with nothing persisted.
	quota.SetCapacity(1)
	deniedToken := hostedCreateUploadToken(t, app, 1<<20, nil)
	rr = hostedUploadRequest(t, mux, "/api/pbvex/storage/upload/"+deniedToken, "application/octet-stream", "denied.bin", body)
	if rr.Code == http.StatusOK {
		t.Fatalf("expected denied upload above capacity, got %d", rr.Code)
	}
}

func TestHostedNativeRecordQuotaWiring(t *testing.T) {
	// The native record hooks are installed through RegisterCore: record
	// uploads through the real API reserve before writing and settle after
	// the commit, and a capacity denial aborts before any write.
	app, _, quota, _, mux := newHostedTestApp(t, nil, true)

	collection := core.NewCollection(core.CollectionTypeBase, "qnative")
	collection.Fields.Add(&core.TextField{Name: "slug", Max: 100})
	collection.Fields.Add(&core.FileField{Name: "doc", MaxSelect: 2, MaxSize: 1 << 20})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}

	doc := bytes.Repeat([]byte("n"), 300)
	rr := hostedMultipartRecordCreate(t, mux, app, "qnative", map[string]string{"slug": "one"}, map[string][]byte{"doc": doc})
	if rr.Code != http.StatusOK {
		t.Fatalf("native record create status = %d body %s", rr.Code, rr.Body.String())
	}
	if got := quota.UsedBytes(); got != int64(len(doc)) {
		t.Fatalf("expected settled native usage %d, got %d", len(doc), got)
	}

	// Fail closed BEFORE writes: with the capacity exhausted, the record
	// create is denied and neither the record nor its file object exists.
	quota.SetCapacity(1)
	rr = hostedMultipartRecordCreate(t, mux, app, "qnative", map[string]string{"slug": "denied"}, map[string][]byte{"doc": doc})
	if rr.Code == http.StatusOK {
		t.Fatalf("expected denied native record create, got %d", rr.Code)
	}
	if n := hostedCollectionRecordCount(t, app, "qnative"); n != 1 {
		t.Fatalf("denied record must not be persisted, rows = %d", n)
	}
	if got := quota.UsedBytes(); got != int64(len(doc)) {
		t.Fatalf("denied upload must not charge or release, usage = %d", got)
	}
	// Exactly the first record persists; the denied one is gone entirely.
	var stored struct {
		Slug string `db:"slug"`
	}
	if err := app.DB().Select("slug").From("qnative").One(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.Slug != "one" {
		t.Fatalf("unexpected persisted row %+v", stored)
	}
}

func TestHostedNativeThumbGateWiring(t *testing.T) {
	// The native thumbnail gate is installed through RegisterCore: every
	// thumb-carrying request of the native files route is refused with an
	// explicit 403 — cached selectors included, because upstream falls back
	// to an unreserved generation whenever the cached object is missing at
	// serve time. Original downloads (no thumb selector) keep working.
	app, _, _, _, mux := newHostedTestApp(t, nil, true)

	collection := core.NewCollection(core.CollectionTypeBase, "qthumbs")
	collection.Fields.Add(&core.TextField{Name: "slug", Max: 100})
	collection.Fields.Add(&core.FileField{Name: "photo", MaxSelect: 1, MaxSize: 1 << 20, Thumbs: []string{"64x64"}})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}

	photo := hostedPNGBytes(t, 32, 24)
	rr := hostedMultipartRecordCreate(t, mux, app, "qthumbs", map[string]string{"slug": "p"}, map[string][]byte{"photo": photo})
	if rr.Code != http.StatusOK {
		t.Fatalf("photo record create status = %d body %s", rr.Code, rr.Body.String())
	}
	var created struct {
		Id    string `json:"id"`
		Photo string `json:"photo"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Photo == "" {
		t.Fatalf("expected one stored photo, got %q", created.Photo)
	}
	filename := created.Photo

	thumbURL := fmt.Sprintf("/api/files/qthumbs/%s/%s?thumb=64x64", created.Id, filename)

	// Fresh generation: the gate denies before the write.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, thumbURL, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected fail-closed 403 for native thumb request, got %d body %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "storage quotas") {
		t.Fatalf("expected the explicit quota rationale, got %s", rr.Body.String())
	}
	// HEAD is served by the same GET pattern and is denied too; an encoded
	// thumb selector cannot escape the gate.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodHead, thumbURL, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for HEAD thumb request, got %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/qthumbs/%s/%s?thumb=%%36%%34x64", created.Id, filename), nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for encoded thumb selector, got %d body %s", rr.Code, rr.Body.String())
	}

	fs, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	record, err := app.FindRecordById(collection, created.Id)
	if err != nil {
		t.Fatal(err)
	}
	thumbKey := fmt.Sprintf("%s/thumbs_%s/64x64_%s", record.BaseFilesPath(), filename, filename)
	exists, err := fs.Exists(thumbKey)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("denied thumb request must not write a variant")
	}

	// A pre-existing cached variant is denied as well, and the denial is
	// passive: the cached object stays untouched.
	if err := fs.CreateThumb(record.BaseFilesPath()+"/"+filename, thumbKey, "64x64"); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, thumbURL, nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected cached thumb request to be denied too, got %d body %s", rr.Code, rr.Body.String())
	}
	exists, err = fs.Exists(thumbKey)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("the denial must not delete the cached variant")
	}

	// Original downloads without a thumb selector keep working.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/qthumbs/%s/%s", created.Id, filename), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected the original to be served, got %d body %s", rr.Code, rr.Body.String())
	}
}

func TestStandaloneStorageWiringUnchanged(t *testing.T) {
	// Standalone (hosting disabled) keeps upstream semantics: no quota
	// client exists, native thumbnail generation works, and PBVex uploads
	// succeed with no provider socket at all — a wired observer would fail
	// every write closed, so success proves the observer is absent.
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	cfg := DefaultConfig()
	cfg.Storage.MaxFileSize = 1 << 20
	if _, _, err := RegisterCore(app, cfg); err != nil {
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
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
		t.Fatal(err)
	}

	// Native record upload and on-demand thumbnail generation behave
	// exactly as upstream ships them.
	collection := core.NewCollection(core.CollectionTypeBase, "qstandalone")
	collection.Fields.Add(&core.TextField{Name: "slug", Max: 100})
	collection.Fields.Add(&core.FileField{Name: "photo", MaxSelect: 1, MaxSize: 1 << 20, Thumbs: []string{"64x64"}})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	photo := hostedPNGBytes(t, 32, 24)
	rr := hostedMultipartRecordCreate(t, mux, app, "qstandalone", map[string]string{"slug": "p"}, map[string][]byte{"photo": photo})
	if rr.Code != http.StatusOK {
		t.Fatalf("standalone record create status = %d body %s", rr.Code, rr.Body.String())
	}
	var created struct {
		Id    string `json:"id"`
		Photo string `json:"photo"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("/api/files/qstandalone/%s/%s?thumb=64x64", created.Id, created.Photo)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, url, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("standalone thumb generation must work, got %d body %s", rr.Code, rr.Body.String())
	}

	// PBVex upload through the router succeeds with no quota provider.
	token := hostedCreateUploadToken(t, app, 1<<20, nil)
	rr = hostedUploadRequest(t, mux, "/api/pbvex/storage/upload/"+token, "application/octet-stream", "solo.bin", []byte("standalone"))
	if rr.Code != http.StatusOK {
		t.Fatalf("standalone upload status = %d body %s", rr.Code, rr.Body.String())
	}
}

func TestHostedTerminateClosesSharedQuotaTransport(t *testing.T) {
	// One socket, one root client: terminate must close the persistent
	// provider connections of the shared transport (the quota client
	// borrows it and never closes it independently).
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	path := filepath.Join(t.TempDir(), "p.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	tracking := &trackingListener{Listener: ln}
	policy := hosting.NewReferenceService(8)
	quota := storagequota.NewReferenceQuotaService(8, hostedDemoQuotaCapacity)
	server := &http.Server{Handler: composedProviderHandler(policy, quota), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(tracking) }()
	t.Cleanup(func() { server.Close() })

	cfg := DefaultConfig()
	cfg.Hosting.Enabled = true
	cfg.Hosting.SocketPath = path
	if _, _, err := RegisterCore(app, cfg); err != nil {
		t.Fatalf("failed to register core: %v", err)
	}
	// The startup handshake leaves a persistent connection.
	waitFor(t, func() bool { return tracking.active() > 0 }, "provider never accepted the handshake connection")

	if err := app.OnTerminate().Trigger(&core.TerminateEvent{App: app}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return tracking.active() == 0 }, "provider connections were not closed at terminate")
}

// trackingListener counts the currently open transport connections the
// provider has accepted.
type trackingListener struct {
	net.Listener
	open atomic.Int64
}

func (l *trackingListener) active() int { return int(l.open.Load()) }

func (l *trackingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.open.Add(1)
	return &trackingConn{Conn: c, listener: l}, nil
}

type trackingConn struct {
	net.Conn
	listener *trackingListener
	counted  atomic.Bool
}

func (c *trackingConn) Close() error {
	if c.counted.CompareAndSwap(false, true) {
		c.listener.open.Add(-1)
	}
	return c.Conn.Close()
}
