package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// fakeS3 is a minimal in-memory S3 HTTP server suitable for exercising the s3blob driver.
type fakeS3 struct {
	objects map[string]fakeObject
	mu      sync.Mutex

	// copyFailures/deleteFailures/getFailures, when >0, make the next matching
	// operations fail with an injected InternalError (one failure consumed each).
	copyFailures   int32
	deleteFailures int32
	getFailures    int32

	// gets counts successful GET object requests (body reads), used to verify
	// that HEAD/conditional responses avoid opening the object body.
	gets int64
	// puts counts object PUT uploads (staged-blob writes), used to verify that
	// capacity is reserved before any staged blob is persisted.
	puts int64
}

type fakeObject struct {
	body        []byte
	contentType string
	etag        string
	modTime     time.Time
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: make(map[string]fakeObject)}
}

// failNextCopy makes the next copy operation return an injected 500.
func (f *fakeS3) failNextCopy() { atomic.AddInt32(&f.copyFailures, 1) }

// failNextDelete makes the next delete operation return an injected 500.
func (f *fakeS3) failNextDelete() { atomic.AddInt32(&f.deleteFailures, 1) }

// failNextGet makes the next object GET return an injected 500.
func (f *fakeS3) failNextGet() { atomic.AddInt32(&f.getFailures, 1) }

// getReads returns the count of successful object GET requests issued.
func (f *fakeS3) getReads() int64 { return atomic.LoadInt64(&f.gets) }

// putWrites returns the count of object PUT uploads (staged-blob writes).
func (f *fakeS3) putWrites() int64 { return atomic.LoadInt64(&f.puts) }

// consumeFailure atomically consumes one failure token, reporting whether the
// operation should fail.
func consumeFailure(p *int32) bool {
	for {
		old := atomic.LoadInt32(p)
		if old <= 0 {
			return false
		}
		if atomic.CompareAndSwapInt32(p, old, old-1) {
			return true
		}
	}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Path-style requests: /<bucket>/<key> or /<bucket>/
	key := strings.TrimPrefix(r.URL.Path, "/test/")
	key = strings.TrimPrefix(key, "/")

	switch r.Method {
	case http.MethodPut:
		if src := r.Header.Get("x-amz-copy-source"); src != "" {
			f.copy(w, r, key, src)
			return
		}
		f.put(w, r, key)
	case http.MethodGet:
		if strings.TrimSuffix(r.URL.Path, "/") == "/test" || r.URL.Path == "/test/" {
			f.list(w, r)
			return
		}
		f.get(w, r, key)
	case http.MethodHead:
		f.head(w, r, key)
	case http.MethodDelete:
		f.delete(w, key)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeS3) put(w http.ResponseWriter, r *http.Request, key string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	atomic.AddInt64(&f.puts, 1)
	sum := md5.Sum(body)
	etag := "\"" + hex.EncodeToString(sum[:]) + "\""
	f.objects[key] = fakeObject{
		body:        body,
		contentType: r.Header.Get("Content-Type"),
		etag:        etag,
		modTime:     time.Now().UTC(),
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3) copy(w http.ResponseWriter, r *http.Request, dstKey, src string) {
	if consumeFailure(&f.copyFailures) {
		f.writeError(w, "InternalError", "injected copy failure", http.StatusInternalServerError)
		return
	}
	src, _ = url.PathUnescape(src)
	src = strings.TrimPrefix(src, "/")
	if idx := strings.Index(src, "/"); idx >= 0 {
		src = src[idx+1:]
	}
	obj, ok := f.objects[src]
	if !ok {
		f.writeError(w, "NoSuchKey", "Not Found", http.StatusNotFound)
		return
	}
	f.objects[dstKey] = obj
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_ = xml.NewEncoder(w).Encode(struct {
		XMLName      xml.Name  `xml:"CopyObjectResult"`
		ETag         string    `xml:"ETag"`
		LastModified time.Time `xml:"LastModified"`
	}{ETag: obj.etag, LastModified: obj.modTime})
}

func (f *fakeS3) get(w http.ResponseWriter, r *http.Request, key string) {
	if consumeFailure(&f.getFailures) {
		f.writeError(w, "InternalError", "injected get failure", http.StatusInternalServerError)
		return
	}
	obj, ok := f.objects[key]
	if !ok {
		f.writeError(w, "NoSuchKey", "Not Found", http.StatusNotFound)
		return
	}
	atomic.AddInt64(&f.gets, 1)

	body := obj.body
	start, end := int64(0), int64(len(body)-1)
	status := http.StatusOK

	if rng := r.Header.Get("Range"); rng != "" {
		var parseErr error
		start, end, parseErr = parseRange(rng, int64(len(body)))
		if parseErr != nil {
			f.writeError(w, "InvalidRange", "Invalid Range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if start > end || start >= int64(len(body)) {
			f.writeError(w, "InvalidRange", "Invalid Range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= int64(len(body)) {
			end = int64(len(body) - 1)
		}
		body = body[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(obj.body)))
		status = http.StatusPartialContent
	}

	w.Header().Set("Content-Type", obj.contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Last-Modified", obj.modTime.Format(http.TimeFormat))
	w.Header().Set("ETag", obj.etag)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (f *fakeS3) head(w http.ResponseWriter, r *http.Request, key string) {
	obj, ok := f.objects[key]
	if !ok {
		f.writeError(w, "NoSuchKey", "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", obj.contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(obj.body)))
	w.Header().Set("Last-Modified", obj.modTime.Format(http.TimeFormat))
	w.Header().Set("ETag", obj.etag)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3) delete(w http.ResponseWriter, key string) {
	if consumeFailure(&f.deleteFailures) {
		f.writeError(w, "InternalError", "injected delete failure", http.StatusInternalServerError)
		return
	}
	delete(f.objects, key)
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	maxKeys := r.URL.Query().Get("max-keys")
	if maxKeys == "" {
		maxKeys = "1000"
	}
	mk, _ := strconv.Atoi(maxKeys)

	contents := make([]listContent, 0)
	for k, obj := range f.objects {
		if strings.HasPrefix(k, prefix) {
			contents = append(contents, listContent{
				Key:          k,
				Size:         len(obj.body),
				LastModified: obj.modTime.Format(time.RFC3339Nano),
				ETag:         obj.etag,
			})
			if len(contents) >= mk {
				break
			}
		}
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_ = xml.NewEncoder(w).Encode(struct {
		XMLName     xml.Name      `xml:"ListBucketResult"`
		Name        string        `xml:"Name"`
		Prefix      string        `xml:"Prefix"`
		MaxKeys     int           `xml:"MaxKeys"`
		KeyCount    int           `xml:"KeyCount"`
		IsTruncated bool          `xml:"IsTruncated"`
		Contents    []listContent `xml:"Contents"`
	}{
		Name:     "test",
		Prefix:   prefix,
		MaxKeys:  mk,
		KeyCount: len(contents),
		Contents: contents,
	})
}

type listContent struct {
	Key          string `xml:"Key"`
	Size         int    `xml:"Size"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
}

func (f *fakeS3) writeError(w http.ResponseWriter, code, message string, status int) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_ = xml.NewEncoder(w).Encode(struct {
		XMLName xml.Name `xml:"Error"`
		Code    string   `xml:"Code"`
		Message string   `xml:"Message"`
	}{Code: code, Message: message})
}

func parseRange(rng string, total int64) (int64, int64, error) {
	if !strings.HasPrefix(rng, "bytes=") {
		return 0, 0, fmt.Errorf("unsupported range unit")
	}
	parts := strings.Split(strings.TrimPrefix(rng, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range")
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	end := total - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, err
		}
	}
	return start, end, nil
}

func newS3TestApp(t *testing.T) (*tests.TestApp, *Service, *fakeS3) {
	t.Helper()

	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	if err := schema.Bootstrap(app); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap schema: %v", err)
	}

	app.Settings().S3 = core.S3Config{
		Enabled:        true,
		Bucket:         "test",
		Region:         "us-east-1",
		Endpoint:       srv.URL,
		AccessKey:      "key",
		Secret:         "secret",
		ForcePathStyle: true,
	}

	cfg := DefaultConfig()
	cfg.MaxFileSize = 1 << 20
	cfg.DefaultUploadTTL = time.Hour
	svc, err := NewService(app, NewRepo(), cfg)
	if err != nil {
		app.Cleanup()
		t.Fatalf("failed to create storage service: %v", err)
	}

	t.Cleanup(app.Cleanup)
	return app, svc, fake
}

func TestS3UploadDownloadAndRange(t *testing.T) {
	_, svc, _ := newS3TestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello s3 world")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "hello.txt", int64(len(body)))
	if err != nil {
		t.Fatalf("s3 upload failed: %v (cause: %v)", err, errors.Unwrap(err))
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if dl == "" {
		t.Fatal("expected download url")
	}

	req := httptest.NewRequest(http.MethodGet, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
		t.Fatalf("s3 download failed: %v", err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != string(body) {
		t.Fatalf("s3 body mismatch: %s", rr.Body.String())
	}
	if rr.Header().Get("Content-Disposition") == "" {
		t.Fatal("expected Content-Disposition header")
	}

	rangeReq := httptest.NewRequest(http.MethodGet, dl, nil)
	rangeReq.Header.Set("Range", "bytes=0-4")
	rangeRR := httptest.NewRecorder()
	if err := svc.Download(rangeRR, rangeReq, id, AuthContext{}); err != nil {
		t.Fatalf("s3 range download failed: %v", err)
	}
	if rangeRR.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rangeRR.Code)
	}
	if rangeRR.Body.String() != "hello" {
		t.Fatalf("s3 range body mismatch: %s", rangeRR.Body.String())
	}
}

func TestS3Head(t *testing.T) {
	_, svc, _ := newS3TestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("s3 head")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodHead, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Fatal("expected empty body for HEAD")
	}
	if rr.Header().Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Fatalf("expected Content-Length %d, got %s", len(body), rr.Header().Get("Content-Length"))
	}
}

func TestS3Delete(t *testing.T) {
	_, svc, _ := newS3TestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("delete me")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("s3 delete failed: %v", err)
	}

	if dl, err := svc.GetURL(context.Background(), id, AuthContext{}); err != nil {
		t.Fatal(err)
	} else if dl != "" {
		t.Fatalf("expected empty url after delete, got %s", dl)
	}
}

// TestS3ConcurrentUploadsDistinctIDs verifies concurrent uploads against the
// fake S3 backend produce distinct ids and fully-active (finalized) files.
func TestS3ConcurrentUploadsDistinctIDs(t *testing.T) {
	_, svc, _ := newS3TestApp(t)

	const n = 12
	tokens := make([]string, n)
	for i := range tokens {
		url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = extractToken(url)
	}

	ids := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := []byte("s3 concurrent body")
			id, err := svc.Upload(context.Background(), tokens[i], bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
			if err != nil {
				errs <- err
				return
			}
			mu.Lock()
			ids[id] = true
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		t.Fatalf("concurrent s3 upload failed: %v", err)
	}
	if len(ids) != n {
		t.Fatalf("expected %d distinct ids, got %d", n, len(ids))
	}
}

// TestS3FinalizeFailureAndRecovery injects a copy failure during post-commit
// finalization, asserts the client observes an error, then confirms the cleanup
// worker recovers the staged file into a fully active, downloadable object.
func TestS3FinalizeFailureAndRecovery(t *testing.T) {
	_, svc, fake := newS3TestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("recover me")

	// Inject a single copy failure so the OnComplete finalization step aborts.
	fake.failNextCopy()
	_, err = svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err == nil {
		t.Fatal("expected upload to fail when finalization copy fails")
	}
	var ue *UploadError
	if !errors.As(err, &ue) || ue.Code != ErrorCodeInternal {
		t.Fatalf("expected internal error, got %v", err)
	}

	// Recovery: with copies working again, the cleanup worker should finalize
	// the committed staged record into an active, downloadable file.
	if err := svc.RunCleanup(); err != nil {
		t.Fatalf("cleanup recovery failed: %v", err)
	}

	// The staged record must now be active: resolve a fresh download URL by
	// listing for a file record keyed by the generated storage id.
	ctx := schema.WithInternalContext(context.Background())
	records := []*core.Record{}
	if err := svc.app.RecordQuery(schema.CollectionStorageFiles).All(&records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly one storage record, got %d", len(records))
	}
	storageID := records[0].GetString(schema.FieldStorageID)
	if status := records[0].GetString(schema.FieldStorageStatus); status != statusActive {
		t.Fatalf("expected active status after recovery, got %q", status)
	}

	dl, err := svc.GetURL(ctx, storageID, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if dl == "" {
		t.Fatal("expected download url after recovery")
	}
	req := httptest.NewRequest(http.MethodGet, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, storageID, AuthContext{}); err != nil {
		t.Fatalf("download after recovery failed: %v", err)
	}
	if rr.Code != http.StatusOK || rr.Body.String() != string(body) {
		t.Fatalf("expected recovered body %q, got %d / %q", string(body), rr.Code, rr.Body.String())
	}
}

// TestS3ReservesCapacityBeforeStreaming verifies that capacity is reserved
// (atomic staged record creation + cap check) BEFORE any staged blob is
// uploaded to the storage backend. With MaxFiles < concurrent uploads, only the
// reserved uploads issue a staged-blob PUT; rejected uploads never reach S3.
func TestS3ReservesCapacityBeforeStreaming(t *testing.T) {
	_, svc, fake := newS3TestApp(t)
	svc.config.MaxFiles = 3

	const n = 10
	tokens := make([]string, n)
	for i := range tokens {
		u, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = extractToken(u)
	}

	// Slow readers widen the window so uploads overlap during streaming.
	var success, full int64
	var wg sync.WaitGroup
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := bytes.Repeat([]byte("x"), 64)
			_, err := svc.Upload(context.Background(), tokens[i], &slowReader{b: body}, "text/plain", "x.txt", int64(len(body)))
			if err == nil {
				atomic.AddInt64(&success, 1)
				return
			}
			var ue *UploadError
			if errors.As(err, &ue) && ue.Code == ErrorCodeStorageFull {
				atomic.AddInt64(&full, 1)
			}
		}(i)
	}
	wg.Wait()

	if success != 3 {
		t.Fatalf("expected 3 successful uploads, got %d", success)
	}
	if full != 7 {
		t.Fatalf("expected 7 storage-full rejections, got %d", full)
	}
	// Only the 3 reserved uploads may persist a staged blob to S3. The 7
	// rejected uploads must never have streamed a staged blob.
	if got := fake.putWrites(); got != 3 {
		t.Fatalf("expected exactly 3 staged-blob PUTs, got %d (cap not reserved before streaming)", got)
	}
}

// slowReader returns its bytes with a tiny delay per chunk to widen concurrency.
type slowReader struct {
	b []byte
	i int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.i >= len(s.b) {
		return 0, io.EOF
	}
	time.Sleep(time.Millisecond)
	n := copy(p, s.b[s.i:])
	s.i += n
	return n, nil
}

// TestS3LeakedStageBlobReclaimed injects a delete failure during finalization's
// stage cleanup so the stage blob leaks after the record goes active, then
// verifies the cleanup worker reclaims the stale stage blob.
func TestS3LeakedStageBlobReclaimed(t *testing.T) {
	app, svc, fake := newS3TestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("leak me")

	// Fail the next delete: finalizeUpload's stage-Delete fails after the copy
	// succeeds, so the record activates while the stage blob leaks.
	fake.failNextDelete()
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatalf("upload should succeed despite stage-delete failure: %v", err)
	}

	// The record is active; both the final blob and the leaked stage blob exist.
	rec, err := svc.repo.GetFileByIDAnyStatus(schema.WithInternalContext(context.Background()), app, id)
	if err != nil {
		t.Fatal(err)
	}
	if status := rec.GetString(schema.FieldStorageStatus); status != statusActive {
		t.Fatalf("expected active record, got %q", status)
	}

	// Run cleanup: the stale stage blob must be reclaimed.
	if err := svc.RunCleanup(); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// After cleanup, only the final blob should remain under the prefix.
	fs, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	prefix := svc.config.FileStoragePrefix
	objs, err := fs.List(prefix + "/")
	if err != nil {
		t.Fatal(err)
	}
	fs.Close()
	stageCount := 0
	finalCount := 0
	for _, o := range objs {
		if _, isStage := parseStorageKey(strings.Trim(prefix, "/"), o.Key); isStage {
			stageCount++
		} else {
			finalCount++
		}
	}
	if stageCount != 0 {
		t.Fatalf("expected leaked stage blob to be reclaimed, found %d stage blobs", stageCount)
	}
	if finalCount != 1 {
		t.Fatalf("expected 1 final blob, got %d", finalCount)
	}
}

// TestS3DeleteMetricsAccurate verifies that a blob delete failure during
// deleting-record recovery is not counted as recovered.
func TestS3DeleteMetricsAccurate(t *testing.T) {
	app, svc, fake := newS3TestApp(t)

	// Create a deleting record with a real blob.
	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	finalKey := svc.fileKey(storageID)
	body := []byte("doomed")
	sha, size, err := svc.streamToFile(bytes.NewReader(body), finalKey, "x.txt", int64(len(body))+1, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.repo.CreateFile(schema.WithInternalContext(context.Background()), app, FileRecord{
		StorageID: storageID, Sha256: sha, Size: size, ContentType: "text/plain",
		FileKey: finalKey, Filename: "x.txt", Status: statusDeleting,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Inject a delete failure: the blob delete fails, so the record must remain
	// "deleting" and NOT be counted as recovered.
	fake.failNextDelete()
	recovered, err := svc.recoverDeleting(schema.WithInternalContext(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 0 {
		t.Fatalf("expected 0 recovered when blob delete fails, got %d", recovered)
	}

	// The record must still be deletable (status unchanged) for retry.
	if _, err := svc.repo.GetFileByIDAnyStatus(schema.WithInternalContext(context.Background()), app, storageID); err != nil {
		t.Fatalf("record should still exist for retry: %v", err)
	}

	// Without the failure, recovery succeeds and is counted.
	recovered, err = svc.recoverDeleting(schema.WithInternalContext(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered after retry, got %d", recovered)
	}
}

// TestS3HeadAndConditionalSkipBody verifies that HEAD and 304/412 responses
// never issue an S3 object GET (body read), using only the metadata/stat path.
func TestS3HeadAndConditionalSkipBody(t *testing.T) {
	_, svc, fake := newS3TestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("body to avoid opening")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}

	// A prior full GET may have populated reads; reset the baseline.
	baseGets := fake.getReads()

	// HEAD must not open the body.
	headReq := httptest.NewRequest(http.MethodHead, dl, nil)
	headRR := httptest.NewRecorder()
	if err := svc.Download(headRR, headReq, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if headRR.Code != http.StatusOK {
		t.Fatalf("expected 200 for HEAD, got %d", headRR.Code)
	}
	if got := fake.getReads() - baseGets; got != 0 {
		t.Fatalf("HEAD opened %d S3 body reads; expected metadata-only", got)
	}

	// Capture the etag to build a 304 precondition.
	getReq := httptest.NewRequest(http.MethodGet, dl, nil)
	getRR := httptest.NewRecorder()
	if err := svc.Download(getRR, getReq, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	etag := getRR.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected etag")
	}
	baseGets = fake.getReads()

	// If-None-Match → 304 must not open the body.
	inm := httptest.NewRequest(http.MethodGet, dl, nil)
	inm.Header.Set("If-None-Match", etag)
	inmRR := httptest.NewRecorder()
	if err := svc.Download(inmRR, inm, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if inmRR.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", inmRR.Code)
	}
	if got := fake.getReads() - baseGets; got != 0 {
		t.Fatalf("304 opened %d S3 body reads; expected metadata-only", got)
	}

	// If-Match mismatch → 412 must not open the body.
	im := httptest.NewRequest(http.MethodGet, dl, nil)
	im.Header.Set("If-Match", `"nonexistent"`)
	imRR := httptest.NewRecorder()
	if err := svc.Download(imRR, im, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if imRR.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412, got %d", imRR.Code)
	}
	if got := fake.getReads() - baseGets; got != 0 {
		t.Fatalf("412 opened %d S3 body reads; expected metadata-only", got)
	}
}
