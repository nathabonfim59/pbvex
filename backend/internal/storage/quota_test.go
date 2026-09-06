package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeQuotaObserver is a programmable, concurrency-safe quota observer
// used to assert the exact reservation, settlement, release and credit
// behavior of the storage service.
type fakeQuotaObserver struct {
	mu sync.Mutex

	seq   int
	calls []string // ordered call log: "<seq>:<call>"

	denyUpload  bool
	denyVariant bool
	failReserve error

	capacity int64 // 0 = unlimited
	reserved int64 // worst-case bytes currently reserved
	inflight int
	settled  int64
	credited int64

	maxObservedInflight int
	reservations        map[string]*fakeReservation
}

func newFakeQuotaObserver() *fakeQuotaObserver {
	return &fakeQuotaObserver{reservations: map[string]*fakeReservation{}}
}

// External records a non-quota storage event (for example the backend
// persist) in the shared ordered log.
func (f *fakeQuotaObserver) External(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	f.calls = append(f.calls, fmt.Sprintf("%d:%s", f.seq, call))
}

func (f *fakeQuotaObserver) Reserve(ctx context.Context, req QuotaReservationRequest) (QuotaReservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	f.calls = append(f.calls, fmt.Sprintf("%d:reserve(%s,%s,%d)", f.seq, req.OpID, req.Purpose, req.WorstCase))
	if f.failReserve != nil {
		return nil, f.failReserve
	}
	if (req.Purpose == QuotaPurposeUpload && f.denyUpload) ||
		(req.Purpose == QuotaPurposeVariant && f.denyVariant) {
		return nil, ErrQuotaDenied
	}
	if f.capacity > 0 && f.reserved+req.WorstCase > f.capacity {
		return nil, ErrQuotaDenied
	}
	f.reserved += req.WorstCase
	f.inflight++
	if f.inflight > f.maxObservedInflight {
		f.maxObservedInflight = f.inflight
	}
	res := &fakeReservation{id: req.OpID, obs: f, worstCase: req.WorstCase}
	f.reservations[req.OpID] = res
	return res, nil
}

func (f *fakeQuotaObserver) Credit(ctx context.Context, req QuotaCreditRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	f.calls = append(f.calls, fmt.Sprintf("%d:credit(%s,%d)", f.seq, req.OpID, req.Bytes))
	f.credited += req.Bytes
	return nil
}

func (f *fakeQuotaObserver) inflightCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inflight
}

func (f *fakeQuotaObserver) totals() (settled, credited int64, maxInflight int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settled, f.credited, f.maxObservedInflight
}

func (f *fakeQuotaObserver) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeQuotaObserver) hasCall(substr string) bool {
	for _, c := range f.callLog() {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func (f *fakeQuotaObserver) callCount(substr string) int {
	n := 0
	for _, c := range f.callLog() {
		if strings.Contains(c, substr) {
			n++
		}
	}
	return n
}

func (f *fakeQuotaObserver) indexOf(substr string) int {
	for i, c := range f.callLog() {
		if strings.Contains(c, substr) {
			return i
		}
	}
	return -1
}

// assertNoOvershoot verifies the hard quota invariants: no settled bytes
// may exceed the reserved worst-case bound, and every reservation is
// resolved (settled or released).
func (f *fakeQuotaObserver) assertNoOvershoot(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, res := range f.reservations {
		if res.settledBytes > res.worstCase {
			t.Fatalf("reservation %s settled %d beyond its reserved %d", id, res.settledBytes, res.worstCase)
		}
		if !res.settled && !res.released {
			t.Fatalf("reservation %s left unresolved", id)
		}
	}
}

type fakeReservation struct {
	id           string
	obs          *fakeQuotaObserver
	worstCase    int64
	settledBytes int64
	settled      bool
	released     bool
}

func (r *fakeReservation) ID() string { return r.id }

func (r *fakeReservation) Settle(ctx context.Context, actualBytes int64) error {
	r.obs.mu.Lock()
	defer r.obs.mu.Unlock()
	if r.settled || r.released {
		return nil
	}
	r.obs.seq++
	r.obs.calls = append(r.obs.calls, fmt.Sprintf("%d:settle(%s,%d)", r.obs.seq, r.id, actualBytes))
	r.settled = true
	r.settledBytes = actualBytes
	r.obs.reserved -= r.worstCase
	r.obs.inflight--
	r.obs.settled += actualBytes
	return nil
}

func (r *fakeReservation) Release(ctx context.Context) error {
	r.obs.mu.Lock()
	defer r.obs.mu.Unlock()
	if r.settled || r.released {
		return nil
	}
	r.obs.seq++
	r.obs.calls = append(r.obs.calls, fmt.Sprintf("%d:release(%s)", r.obs.seq, r.id))
	r.released = true
	r.obs.reserved -= r.worstCase
	r.obs.inflight--
	return nil
}

func TestUploadNilObserverStandalone(t *testing.T) {
	_, svc := newStorageTestApp(t)
	if svc.hasQuota() {
		t.Fatal("expected standalone service without observer")
	}
	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("x"), 1024)
	id, err := svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(body), "text/plain", "a.txt", int64(len(body)))
	if err != nil || id == "" {
		t.Fatalf("standalone upload must be unchanged: %v", err)
	}
}

func TestUploadReservesBeforeBackendWriteAndSettlesActual(t *testing.T) {
	obs := newFakeQuotaObserver()
	_, svc := newStorageTestApp(t)
	svc.SetQuotaObserver(obs)

	svc.persistHook = func() { obs.External("persist") }

	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("q"), 4096)
	id, err := svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(body), "text/plain", "a.txt", int64(len(body)))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	_ = id

	reserveIdx := obs.indexOf("reserve(")
	persistIdx := obs.indexOf("persist")
	settleIdx := obs.indexOf("settle(")
	if reserveIdx == -1 || persistIdx == -1 || settleIdx == -1 {
		t.Fatalf("missing quota lifecycle calls, log: %v", obs.callLog())
	}
	if !(reserveIdx < persistIdx && persistIdx < settleIdx) {
		t.Fatalf("expected reserve -> backend persist -> settle order, log: %v", obs.callLog())
	}
	// The worst-case bound is the effective staging cap.
	if want := fmt.Sprintf(",%s,%d)", QuotaPurposeUpload, svc.config.MaxFileSize); !obs.hasCall(want) {
		t.Fatalf("expected worst-case bound %d in log: %v", svc.config.MaxFileSize, obs.callLog())
	}
	if n := obs.callCount("reserve("); n != 1 {
		t.Fatalf("expected exactly one reservation, got %d", n)
	}
	settled, _, _ := obs.totals()
	if settled != int64(len(body)) {
		t.Fatalf("expected settled actual %d, got %d", len(body), settled)
	}
	if inflight := obs.inflightCount(); inflight != 0 {
		t.Fatalf("expected no inflight reservations after upload, got %d", inflight)
	}
}

func TestUploadReleasesOnStagingFailure(t *testing.T) {
	obs := newFakeQuotaObserver()
	_, svc := newStorageTestApp(t)
	svc.SetQuotaObserver(obs)

	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Upload(context.Background(), extractToken(uploadURL), errReader{err: errors.New("boom")}, "text/plain", "a.txt", 0)
	if err == nil {
		t.Fatal("expected staging failure")
	}
	if obs.hasCall("settle(") {
		t.Fatal("must not settle a failed upload")
	}
	if !obs.hasCall("release(") {
		t.Fatalf("expected release of the reservation, log: %v", obs.callLog())
	}
	if inflight := obs.inflightCount(); inflight != 0 {
		t.Fatalf("expected reservation released, inflight=%d", inflight)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestUploadDeniedByQuotaFailsClosed(t *testing.T) {
	obs := newFakeQuotaObserver()
	obs.denyUpload = true
	_, svc := newStorageTestApp(t)
	svc.SetQuotaObserver(obs)

	var persisted atomic.Bool
	svc.persistHook = func() { persisted.Store(true) }

	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("denied bytes")
	_, err = svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(body), "text/plain", "a.txt", int64(len(body)))
	var uploadErr *UploadError
	if !errors.As(err, &uploadErr) || uploadErr.Code != ErrorCodeStorageFull {
		t.Fatalf("expected storage_full error, got %v", err)
	}
	if persisted.Load() {
		t.Fatal("denied upload must not write to the backend")
	}
	if inflight := obs.inflightCount(); inflight != 0 {
		t.Fatalf("denied upload must not leave reservations, inflight=%d", inflight)
	}
}

func TestUploadUnavailableFailsClosed(t *testing.T) {
	obs := newFakeQuotaObserver()
	obs.failReserve = ErrQuotaUnavailable
	_, svc := newStorageTestApp(t)
	svc.SetQuotaObserver(obs)

	var persisted atomic.Bool
	svc.persistHook = func() { persisted.Store(true) }

	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader([]byte("x")), "text/plain", "a.txt", 1)
	if err == nil {
		t.Fatal("expected unavailable quota to abort the upload")
	}
	if persisted.Load() {
		t.Fatal("unavailable quota must not allow the write")
	}
}

func TestCustomObserverErrorTextIsSanitized(t *testing.T) {
	obs := newFakeQuotaObserver()
	obs.failReserve = errors.New("provider secret internal state leak")
	_, svc := newStorageTestApp(t)
	svc.SetQuotaObserver(obs)

	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader([]byte("x")), "text/plain", "a.txt", 1)
	if err == nil {
		t.Fatal("expected fail-closed error")
	}
	if strings.Contains(fmt.Sprint(err), "provider secret") {
		t.Fatalf("custom observer error text leaked: %v", err)
	}
	if !errors.Is(err, ErrQuotaUnavailable) {
		t.Fatalf("expected sanitized unavailable classification, got %v", err)
	}
}

func TestUploadQuotaCapacityDeniesThenRecovers(t *testing.T) {
	obs := newFakeQuotaObserver()
	obs.capacity = 2048
	_, svc := newStorageTestApp(t)
	svc.SetQuotaObserver(obs)

	body := bytes.Repeat([]byte("y"), 4096)
	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	// The worst-case staging cap exceeds the capacity: denied before any
	// write even though the actual body is small.
	_, err = svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(body), "text/plain", "a.txt", int64(len(body)))
	var uploadErr *UploadError
	if !errors.As(err, &uploadErr) || uploadErr.Code != ErrorCodeStorageFull {
		t.Fatalf("expected storage_full under capacity, got %v", err)
	}
	// Raising the host capacity lets the same tenant continue.
	obs.mu.Lock()
	obs.capacity = 0
	obs.mu.Unlock()
	url2, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Upload(context.Background(), extractToken(url2), bytes.NewReader(body), "text/plain", "a.txt", int64(len(body))); err != nil {
		t.Fatalf("expected upload after capacity raise: %v", err)
	}
}

func TestVariantReservationDeniedAndAllowed(t *testing.T) {
	obs := newFakeQuotaObserver()
	_, svc := newStorageTestApp(t)
	svc.SetQuotaObserver(obs)

	encoded := testPNGBytes(t, 8, 6)
	policy := ImagePolicy{Kind: "image", Thumbs: []string{"4x4"}, MimeTypes: []string{"image/png"}}
	uploadURL, err := svc.GenerateImageUploadURL(context.Background(), AuthContext{}, policy)
	if err != nil {
		t.Fatal(err)
	}
	id, err := svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(encoded), "application/octet-stream", "photo.fake", int64(len(encoded)))
	if err != nil {
		t.Fatalf("image upload failed: %v", err)
	}
	dl, err := svc.GetCapabilityURL(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	// Deny variants: the download fails closed and no variant is stored.
	obs.denyVariant = true
	req := httptest.NewRequest(http.MethodGet, dl+"&thumb=4x4", nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err == nil {
		t.Fatal("expected denied variant to fail the download")
	}
	if quotaTestThumbExists(t, svc, id, "4x4") {
		t.Fatal("denied variant must not be written")
	}

	// Allow variants: the download generates one and settles its actual
	// bytes after the write.
	obs.denyVariant = false
	req2 := httptest.NewRequest(http.MethodGet, dl+"&thumb=4x4", nil)
	rr2 := httptest.NewRecorder()
	if err := svc.Download(rr2, req2, id, AuthContext{}); err != nil {
		t.Fatalf("allowed variant download failed: %v", err)
	}
	if !quotaTestThumbExists(t, svc, id, "4x4") {
		t.Fatal("expected generated variant on disk")
	}
	if n := obs.callCount("settle(variant-"); n != 1 {
		t.Fatalf("expected exactly one variant settlement, got %d (log: %v)", n, obs.callLog())
	}
	settled, _, _ := obs.totals()
	if settled <= int64(len(encoded)) {
		t.Fatalf("expected variant bytes settled beyond the original, settled=%d", settled)
	}
	if inflight := obs.inflightCount(); inflight != 0 {
		t.Fatalf("expected all reservations settled, inflight=%d", inflight)
	}
	// The variant reservation is taken from the staged encode and must be
	// EXACT: reserved bound equals settled bytes, never less.
	obs.mu.Lock()
	for id, res := range obs.reservations {
		if strings.HasPrefix(id, "variant-") && res.settled {
			if res.settledBytes != res.worstCase {
				t.Fatalf("expected exact variant reservation, reserved=%d settled=%d", res.worstCase, res.settledBytes)
			}
		}
	}
	obs.mu.Unlock()
	obs.assertNoOvershoot(t)
}

func quotaTestThumbExists(t *testing.T, svc *Service, id, thumb string) bool {
	t.Helper()
	fs, err := svc.app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	exists, err := fs.Exists(fmt.Sprintf("%s/%s/thumbs/%s/blob", strings.Trim(svc.config.FileStoragePrefix, "/"), id, thumb))
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestDeleteCreditsOriginalAndVariantBytes(t *testing.T) {
	obs := newFakeQuotaObserver()
	_, svc := newStorageTestApp(t)
	svc.SetQuotaObserver(obs)

	encoded := testPNGBytes(t, 8, 6)
	policy := ImagePolicy{Kind: "image", Thumbs: []string{"4x4"}, MimeTypes: []string{"image/png"}}
	uploadURL, err := svc.GenerateImageUploadURL(context.Background(), AuthContext{}, policy)
	if err != nil {
		t.Fatal(err)
	}
	id, err := svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(encoded), "application/octet-stream", "photo.fake", int64(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := svc.GetCapabilityURL(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, dl+"&thumb=4x4", nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if !quotaTestThumbExists(t, svc, id, "4x4") {
		t.Fatal("expected variant before delete")
	}

	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	_, credited, _ := obs.totals()
	// The credit covers the confirmed original plus variant bytes.
	if credited <= int64(len(encoded)) {
		t.Fatalf("expected credited bytes beyond the original, got %d", credited)
	}
	if !obs.hasCall("credit(delete-" + id) {
		t.Fatalf("expected stable deletion identity, log: %v", obs.callLog())
	}
	if quotaTestThumbExists(t, svc, id, "4x4") {
		t.Fatal("variant must be removed with the original")
	}
}

func TestConcurrentUploadsReserveAtomicallyBeforeWrites(t *testing.T) {
	obs := newFakeQuotaObserver()
	obs.capacity = 3 * 4096 // three worst-case slots
	_, svc := newStorageTestApp(t)
	svc.SetQuotaObserver(obs)

	var persistOvershoot atomic.Int32
	svc.persistHook = func() {
		// A backend write while three reservations are already inflight
		// would mean a write escaped the reservation gate.
		if obs.inflightCount() > 3 {
			persistOvershoot.Add(1)
		}
	}

	const total = 8
	var wg sync.WaitGroup
	var allowed, denied atomic.Int32
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
			if err != nil {
				t.Error(err)
				return
			}
			body := bytes.Repeat([]byte("c"), 4096)
			_, err = svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(body), "text/plain", fmt.Sprintf("c%d.txt", i), int64(len(body)))
			if err != nil {
				var uploadErr *UploadError
				if errors.As(err, &uploadErr) && uploadErr.Code == ErrorCodeStorageFull {
					denied.Add(1)
					return
				}
				t.Errorf("unexpected error: %v", err)
				return
			}
			allowed.Add(1)
		}(i)
	}
	wg.Wait()

	if persistOvershoot.Load() != 0 {
		t.Fatal("observed a backend write outside the reservation gate")
	}
	if got := allowed.Load() + denied.Load(); got != total {
		t.Fatalf("expected every attempt to settle, allowed=%d denied=%d", allowed.Load(), denied.Load())
	}
	if denied.Load() == 0 {
		t.Fatal("expected the capacity to deny some concurrent attempts")
	}
	_, settled, maxInflight := obs.totals()
	if maxInflight > 3 {
		t.Fatalf("concurrent reservations exceeded capacity: %d", maxInflight)
	}
	if settled != int64(allowed.Load())*4096 {
		t.Fatalf("expected settled bytes to match stored uploads, got %d", settled)
	}
	if inflight := obs.inflightCount(); inflight != 0 {
		t.Fatalf("expected all reservations resolved, inflight=%d", inflight)
	}
}

// testPNGBytes renders a deterministic PNG for upload and variant tests.
func testPNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 17), G: uint8(y * 29), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
