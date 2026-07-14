package storage

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"unicode/utf8"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

func newStorageTestApp(t *testing.T) (*tests.TestApp, *Service) {
	t.Helper()

	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	if err := schema.Bootstrap(app); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap schema: %v", err)
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
	return app, svc
}

func TestGenerateUploadURLAndUpload(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatalf("generate upload url failed: %v", err)
	}
	if url == "" {
		t.Fatal("expected upload url")
	}

	token := extractToken(url)
	body := []byte("hello world")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "hello.txt", int64(len(body)))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected storage id")
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatalf("getUrl failed: %v", err)
	}
	if dl == "" {
		t.Fatal("expected download url")
	}
	if !strings.Contains(dl, id) {
		t.Fatalf("download url does not contain storage id: %s", dl)
	}
}

func TestUploadRejectsReplay(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello")
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err == nil {
		t.Fatal("expected replay to fail")
	}
}

func TestUploadRejectsExpiry(t *testing.T) {
	_, svc := newStorageTestApp(t)

	cfg := DefaultConfig()
	cfg.DefaultUploadTTL = time.Millisecond
	svc, err := NewService(svc.app, NewRepo(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	time.Sleep(2 * time.Millisecond)
	body := []byte("hello")
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err == nil {
		t.Fatal("expected expired token to fail")
	}
}

func TestUploadRejectsOversized(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := bytes.Repeat([]byte("x"), 2<<20)
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err == nil {
		t.Fatal("expected oversized upload to fail")
	}
}

func TestUploadRejectsBadContentType(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello")
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/\\.\\.invalid", "x.txt", int64(len(body))); err == nil {
		t.Fatal("expected malformed content type to fail")
	}
}

func TestGetURLReturnsNullForMissing(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GetURL(context.Background(), "missing-id", AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if url != "" {
		t.Fatalf("expected empty url for missing id, got %s", url)
	}
}

func TestDeleteAndGetURL(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if err := svc.Delete(context.Background(), id); err != ErrStorageNotFound {
		t.Fatalf("expected not found for deleted id, got %v", err)
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if dl != "" {
		t.Fatalf("expected empty url after delete, got %s", dl)
	}
}

func TestDownloadHeadersAndBytes(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello world")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
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
		t.Fatalf("download failed: %v", err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != string(body) {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("expected text/plain, got %s", ct)
	}
	if rr.Header().Get("Digest") == "" {
		t.Fatal("expected Digest header")
	}
}

func TestDownloadHead(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if dl == "" {
		t.Fatal("expected download url")
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
	if rr.Header().Get("Content-Length") == "" {
		t.Fatal("expected Content-Length")
	}
}

func TestDownloadMissing(t *testing.T) {
	_, svc := newStorageTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/storage/missing", nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, "missing", AuthContext{}); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDownloadRange(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello world")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if dl == "" {
		t.Fatal("expected download url")
	}

	req := httptest.NewRequest(http.MethodGet, dl, nil)
	req.Header.Set("Range", "bytes=0-4")
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rr.Code)
	}
	if rr.Body.String() != "hello" {
		t.Fatalf("unexpected range body: %s", rr.Body.String())
	}
}

func TestDownloadConditional(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	etag := rr.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag")
	}

	req2 := httptest.NewRequest(http.MethodGet, dl, nil)
	req2.Header.Set("If-None-Match", etag)
	rr2 := httptest.NewRecorder()
	if err := svc.Download(rr2, req2, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rr2.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", rr2.Code)
	}
}

func TestDownloadPreconditionsRFC(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	etag := rr.Header().Get("ETag")
	lastMod := rr.Header().Get("Last-Modified")
	if etag == "" || lastMod == "" {
		t.Fatal("expected ETag and Last-Modified")
	}

	cases := []struct {
		name   string
		header string
		value  string
		code   int
	}{
		{"If-None-Match matches", "If-None-Match", etag, http.StatusNotModified},
		{"If-None-Match star", "If-None-Match", "*", http.StatusNotModified},
		{"If-None-Match mismatch", "If-None-Match", `"other"`, http.StatusOK},
		{"If-Match mismatch", "If-Match", `"other"`, http.StatusPreconditionFailed},
		{"If-Match star", "If-Match", "*", http.StatusOK},
		{"If-Match matches", "If-Match", etag, http.StatusOK},
		{"If-Modified-Since past", "If-Modified-Since", "Sun, 06 Nov 1994 08:49:37 GMT", http.StatusOK},
		{"If-Unmodified-Since past", "If-Unmodified-Since", "Sun, 06 Nov 1994 08:49:37 GMT", http.StatusPreconditionFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, dl, nil)
			r.Header.Set(tc.header, tc.value)
			rr := httptest.NewRecorder()
			if err := svc.Download(rr, r, id, AuthContext{}); err != nil {
				t.Fatal(err)
			}
			if rr.Code != tc.code {
				t.Fatalf("expected %d, got %d", tc.code, rr.Code)
			}
			if rr.Code == http.StatusPreconditionFailed || rr.Code == http.StatusNotModified {
				if rr.Header().Get("Content-Disposition") == "" {
					t.Fatal("expected Content-Disposition on conditional response")
				}
			}
		})
	}

	// If-Range with ETag matching should return a 206 partial.
	rng := httptest.NewRequest(http.MethodGet, dl, nil)
	rng.Header.Set("Range", "bytes=0-2")
	rng.Header.Set("If-Range", etag)
	rrRng := httptest.NewRecorder()
	if err := svc.Download(rrRng, rng, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rrRng.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 for If-Range, got %d", rrRng.Code)
	}
}

func TestFilenameSanitization(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello")
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "../../etc/passwd", int64(len(body))); err == nil {
		t.Fatal("expected path traversal filename to fail")
	}
}

func TestConcurrentUploadsUseDistinctIDs(t *testing.T) {
	_, svc := newStorageTestApp(t)

	urls := make([]string, 10)
	for i := range urls {
		u, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
		if err != nil {
			t.Fatal(err)
		}
		urls[i] = u
	}

	ids := make(map[string]bool)
	for _, u := range urls {
		token := extractToken(u)
		id, err := svc.Upload(context.Background(), token, strings.NewReader("data"), "text/plain", "x.txt", 4)
		if err != nil {
			t.Fatal(err)
		}
		if ids[id] {
			t.Fatal("duplicate storage id")
		}
		ids[id] = true
	}
}

func TestConcurrentUploadSameTokenRace(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello race")

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			// Add a tiny stagger to maximize contention.
			if i == 1 {
				time.Sleep(time.Millisecond)
			}
			_, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
			results <- err
		}(i)
	}

	successes := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one successful upload, got %d", successes)
	}
}

func TestFailedUploadReleasesClaim(t *testing.T) {
	app, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)

	// Oversized upload fails after claiming the token.
	body := bytes.Repeat([]byte("x"), 2<<20)
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err == nil {
		t.Fatal("expected oversized upload to fail")
	}

	// The same token should now be claimable again.
	valid := []byte("hello")
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(valid), "text/plain", "x.txt", int64(len(valid))); err != nil {
		t.Fatalf("expected retry with same token to succeed: %v", err)
	}

	// Confirm only one file record exists.
	var files []*core.Record
	if err := app.RecordQuery(schema.CollectionStorageFiles).All(&files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one file record, got %d", len(files))
	}
}

func TestRestartPersistence(t *testing.T) {
	app, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("persisted")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
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

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if dl == "" {
		t.Fatal("expected download url after restart")
	}
}

func TestCleanupWorkerRecoversStaged(t *testing.T) {
	app, svc := newStorageTestApp(t)

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	stageKey := svc.stageFileKey(storageID, "attempt123")
	body := []byte("staged data")

	if _, _, err := svc.streamToFile(bytes.NewReader(body), stageKey, "x.txt", int64(len(body))+1, 0); err != nil {
		t.Fatal(err)
	}

	rec := FileRecord{
		StorageID:   storageID,
		Sha256:      "abc",
		Size:        int64(len(body)),
		ContentType: "text/plain",
		FileKey:     stageKey,
		Filename:    "x.txt",
		Status:      statusStaged,
	}
	if _, err := svc.repo.CreateFile(schema.WithInternalContext(context.Background()), app, rec); err != nil {
		t.Fatal(err)
	}

	if err := svc.RunCleanup(); err != nil {
		t.Fatal(err)
	}

	dl, err := svc.GetURL(context.Background(), storageID, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if dl == "" {
		t.Fatal("expected download url after cleanup recovery")
	}

	req := httptest.NewRequest(http.MethodGet, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, storageID, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func extractToken(uploadURL string) string {
	parts := strings.Split(uploadURL, "/")
	return parts[len(parts)-1]
}

func TestUploadStreamCancellationCleanup(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)

	pr, pw := io.Pipe()
	go func() {
		_ = pw.Close()
	}()

	if _, err := svc.Upload(context.Background(), token, pr, "text/plain", "x.txt", 0); err != nil {
		// Empty uploads are rejected by filesystem.NewFileFromPath because empty content.
		if !strings.Contains(err.Error(), "empty") && !strings.Contains(err.Error(), "stage") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestEmptyUploadRejected(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(nil), "text/plain", "x.txt", 0); err == nil {
		t.Fatal("expected empty upload to fail")
	}
}

func TestDeleteMissingID(t *testing.T) {
	_, svc := newStorageTestApp(t)

	// A validly-branded id that was never uploaded should resolve to not-found,
	// not a validation error.
	missing, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), missing); err != ErrStorageNotFound {
		t.Fatalf("expected not found, got %v", err)
	}

	// A non-branded id is rejected at the boundary before any lookup.
	if err := svc.Delete(context.Background(), "missing-id"); !errors.Is(err, ErrInvalidStorageID) {
		t.Fatalf("expected invalid storage id, got %v", err)
	}
}

func TestAllowedContentTypes(t *testing.T) {
	_, svc := newStorageTestApp(t)

	cfg := DefaultConfig()
	cfg.AllowedContentTypes = []string{"image/*"}
	svc, err := NewService(svc.app, NewRepo(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("hello")
	if _, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err == nil {
		t.Fatal("expected text/plain to be rejected")
	}
}

func TestTamperedToken(t *testing.T) {
	_, svc := newStorageTestApp(t)

	if _, err := svc.Upload(context.Background(), "not-a-token", strings.NewReader("data"), "text/plain", "x.txt", 4); err == nil {
		t.Fatal("expected tampered token to fail")
	}
}

func TestDownloadAuthIsolation(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{IsAuthenticated: true, UserID: "user1"})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("secret")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{IsAuthenticated: true, UserID: "user1"})
	if err != nil {
		t.Fatal(err)
	}
	if dl == "" {
		t.Fatal("expected download url")
	}

	// URL signed for user1 should work when accessed by user1.
	req := httptest.NewRequest(http.MethodGet, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{IsAuthenticated: true, UserID: "user1"}); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != string(body) {
		t.Fatal("download body mismatch")
	}

	// Anonymous caller must not access a user1-bound URL.
	reqAnon := httptest.NewRequest(http.MethodGet, dl, nil)
	rrAnon := httptest.NewRecorder()
	if err := svc.Download(rrAnon, reqAnon, id, AuthContext{}); err == nil {
		t.Fatal("expected anonymous caller to be forbidden")
	}

	// user2 must not access a user1-bound URL.
	reqUser2 := httptest.NewRequest(http.MethodGet, dl, nil)
	rrUser2 := httptest.NewRecorder()
	if err := svc.Download(rrUser2, reqUser2, id, AuthContext{IsAuthenticated: true, UserID: "user2"}); err == nil {
		t.Fatal("expected user2 to be forbidden")
	}

	// Tampering the identity in the signed URL should fail verification.
	tampered := strings.Replace(dl, "sub=user1", "sub=user2", 1)
	req2 := httptest.NewRequest(http.MethodGet, tampered, nil)
	rr2 := httptest.NewRecorder()
	if err := svc.Download(rr2, req2, id, AuthContext{IsAuthenticated: true, UserID: "user2"}); err == nil {
		t.Fatal("expected tampered signed url to fail")
	}
}

func TestSignedURLAndCreatedByUseGlobalTokenIdentifier(t *testing.T) {
	app, svc := newStorageTestApp(t)
	first := "pocketbase:collection_one:same_record_id"
	second := "pocketbase:collection_two:same_record_id"
	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{IsAuthenticated: true, TokenIdentifier: first})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("identity-bound")
	id, err := svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	record, err := svc.repo.GetFile(schema.WithInternalContext(context.Background()), app, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := record.GetString(schema.FieldStorageCreatedBy); got != first {
		t.Fatalf("createdBy=%q want %q", got, first)
	}
	downloadURL, err := svc.GetURL(context.Background(), id, AuthContext{IsAuthenticated: true, TokenIdentifier: first})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Download(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, downloadURL, nil), id, AuthContext{IsAuthenticated: true, TokenIdentifier: second}); err == nil {
		t.Fatal("same record id from another auth collection accessed signed URL")
	}
	if err := svc.Download(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, downloadURL, nil), id, AuthContext{IsAuthenticated: true, TokenIdentifier: first}); err != nil {
		t.Fatalf("stable collection-id token identifier rejected: %v", err)
	}
}

func TestDownloadCacheControlIdentityBound(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("data")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	// Anonymous URL uses public cache.
	dlAnon, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	reqAnon := httptest.NewRequest(http.MethodGet, dlAnon, nil)
	rrAnon := httptest.NewRecorder()
	if err := svc.Download(rrAnon, reqAnon, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if cc := rrAnon.Header().Get("Cache-Control"); !strings.Contains(cc, "public") || !strings.Contains(cc, "max-age=") {
		t.Fatalf("expected public max-age Cache-Control, got %q", cc)
	}

	// Identity-bound URL uses private cache.
	dlUser, err := svc.GetURL(context.Background(), id, AuthContext{IsAuthenticated: true, UserID: "user1"})
	if err != nil {
		t.Fatal(err)
	}
	reqUser := httptest.NewRequest(http.MethodGet, dlUser, nil)
	rrUser := httptest.NewRecorder()
	if err := svc.Download(rrUser, reqUser, id, AuthContext{IsAuthenticated: true, UserID: "user1"}); err != nil {
		t.Fatal(err)
	}
	if cc := rrUser.Header().Get("Cache-Control"); !strings.Contains(cc, "private") || !strings.Contains(cc, "max-age=") {
		t.Fatalf("expected private max-age Cache-Control, got %q", cc)
	}
}

func TestActiveContentDisposition(t *testing.T) {
	_, svc := newStorageTestApp(t)

	for _, ct := range []string{"text/html", "image/svg+xml", "application/xhtml+xml"} {
		url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
		if err != nil {
			t.Fatal(err)
		}
		token := extractToken(url)
		body := []byte("<html></html>")
		id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), ct, "x.html", int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		dl, err := svc.GetURL(context.Background(), id, AuthContext{})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, dl, nil)
		rr := httptest.NewRecorder()
		if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
			t.Fatal(err)
		}
		if disp := rr.Header().Get("Content-Disposition"); !strings.Contains(disp, "attachment") {
			t.Fatalf("expected attachment for %s, got %q", ct, disp)
		}
		if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("expected X-Content-Type-Options: nosniff")
		}
	}
}

func TestKeyringRotationOverlap(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("overlap")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if dl == "" {
		t.Fatal("expected download url")
	}

	// Simulate a key rotation by generating a new current key while keeping the old key in grace.
	if err := svc.kr.LoadOrCreate(schema.WithInternalContext(context.Background())); err != nil {
		t.Fatal(err)
	}
	newKey, err := svc.kr.generate(schema.WithInternalContext(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	svc.kr.current = newKey
	svc.kr.keys[newKey.id] = newKey

	req := httptest.NewRequest(http.MethodGet, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with rotated key, got %d", rr.Code)
	}
	if rr.Body.String() != string(body) {
		t.Fatalf("body mismatch: %s", rr.Body.String())
	}
}

func TestCorruptSigningKeysAreNeverLoaded(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "malformed base64", key: "%%%"},
		{name: "wrong length", key: base64.StdEncoding.EncodeToString([]byte("short"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, svc := newStorageTestApp(t)
			if err := svc.WarmActive(); err != nil {
				t.Fatal(err)
			}
			badID := svc.kr.current.id
			records := []*core.Record{}
			if err := app.RecordQuery(schema.CollectionStorageKeyring).All(&records); err != nil || len(records) != 1 {
				t.Fatalf("load key record: count=%d err=%v", len(records), err)
			}
			records[0].Set(schema.FieldKeyringKey, tc.key)
			if err := app.SaveWithContext(schema.WithInternalContext(context.Background()), records[0]); err != nil {
				t.Fatal(err)
			}

			reloaded, err := NewService(app, NewRepo(), svc.config)
			if err != nil {
				t.Fatal(err)
			}
			if err := reloaded.WarmActive(); err != nil {
				t.Fatal(err)
			}
			if reloaded.kr.current.id == badID {
				t.Fatal("corrupt key became the current signing key")
			}
			if _, err := reloaded.kr.Get(context.Background(), badID); err == nil {
				t.Fatal("corrupt key was returned for verification")
			}
		})
	}
}

func TestSignedURLExpiresAtExactBoundary(t *testing.T) {
	_, svc := newStorageTestApp(t)
	if err := svc.WarmActive(); err != nil {
		t.Fatal(err)
	}
	id, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	key := svc.kr.current
	pathValue := svc.config.BasePath + "/" + url.PathEscape(id)
	values := url.Values{
		"v":     {key.id},
		"pid":   {id},
		"exp":   {strconv.FormatInt(time.Now().UTC().Unix(), 10)},
		"sub":   {""},
		"pol":   {"download"},
		"nonce": {"boundary"},
	}
	payload := pathValue + "?" + values.Encode()
	values.Set("sig", base64.RawURLEncoding.EncodeToString(key.sign(payload)))
	if err := svc.verifySignedURL(id, &url.URL{Path: pathValue, RawQuery: values.Encode()}, AuthContext{}); !errors.Is(err, ErrURLExpired) {
		t.Fatalf("expected exact-boundary expiry, got %v", err)
	}
}

func TestTransactionUsesCommittedSigningKeyWithoutRotationWrite(t *testing.T) {
	app, svc := newStorageTestApp(t)
	if err := svc.WarmActive(); err != nil {
		t.Fatal(err)
	}
	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("rotation")
	id, err := svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	svc.kr.mu.Lock()
	key := svc.kr.current
	key.createdAt = time.Now().UTC().Add(-svc.config.KeyRotationInterval - time.Minute)
	key.expiresAt = time.Now().UTC().Add(svc.config.URLSigningTTL + time.Hour)
	svc.kr.current = key
	svc.kr.keys[key.id] = key
	svc.kr.mu.Unlock()

	before := []*core.Record{}
	if err := app.RecordQuery(schema.CollectionStorageKeyring).All(&before); err != nil {
		t.Fatal(err)
	}
	if err := app.RunInTransaction(func(txApp core.App) error {
		rec, err := svc.repo.GetFile(schema.WithInternalContext(context.Background()), txApp, id)
		if err != nil {
			return err
		}
		rec.Set("updated", types.NowDateTime())
		if err := txApp.SaveWithContext(schema.WithInternalContext(context.Background()), rec); err != nil {
			return err
		}
		got, err := svc.GetURL(WithApp(context.Background(), txApp), id, AuthContext{})
		if err != nil {
			return err
		}
		if got == "" {
			return fmt.Errorf("empty signed url")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after := []*core.Record{}
	if err := app.RecordQuery(schema.CollectionStorageKeyring).All(&after); err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("transaction rotated signing key: before=%d after=%d", len(before), len(after))
	}
}

func TestCreatedByIsAuditMetadataNotAutomaticAuthorization(t *testing.T) {
	app, svc := newStorageTestApp(t)
	uploadURL, err := svc.GenerateUploadURL(context.Background(), AuthContext{IsAuthenticated: true, UserID: "creator"})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("bearer upload")
	id, err := svc.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := svc.repo.GetFile(schema.WithInternalContext(context.Background()), app, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.GetString(schema.FieldStorageCreatedBy); got != "creator" {
		t.Fatalf("createdBy=%q, want creator", got)
	}
	if got, err := svc.GetURL(context.Background(), id, AuthContext{IsAuthenticated: true, UserID: "other"}); err != nil || got == "" {
		t.Fatalf("createdBy unexpectedly enforced as getUrl authorization: url=%q err=%v", got, err)
	}
}

func TestRestartSignedURL(t *testing.T) {
	app, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("restart")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if dl == "" {
		t.Fatal("expected download url")
	}

	// Force a keyring reload on next use to simulate a process restart.
	svc.kr.loaded = false

	if err := app.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 after restart, got %d", rr.Code)
	}
	if rr.Body.String() != string(body) {
		t.Fatalf("body mismatch after restart: %s", rr.Body.String())
	}
}

func TestUploadCancellation(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	body := []byte("data")
	if _, err := svc.Upload(ctx, token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err == nil {
		t.Fatal("expected cancelled context to fail")
	}
}

func TestStorageIDNotRevealPath(t *testing.T) {
	id, err := GenerateStorageID()
	if err != nil {
		t.Fatalf("generate storage id failed: %v", err)
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		t.Fatalf("storage id reveals path: %s", id)
	}
}

func TestValidateStorageID(t *testing.T) {
	valid, err := GenerateStorageID()
	if err != nil {
		t.Fatalf("generate storage id failed: %v", err)
	}
	// Invalid: empty, path-bearing, oversized, non-branded, wrong prefix,
	// wrong length, non-hex characters, and uppercase.
	invalid := []string{
		"", "a/../b", "a/b", "a\\b", strings.Repeat("x", 129),
		"missing-id", "valid-id", "pbv_abc", "x" + valid,
		"pbv_" + strings.Repeat("g", 32), "pbv_" + strings.Repeat("a", 31),
		"PBV_" + valid[4:],
	}
	for _, id := range invalid {
		if err := ValidateStorageID(id); err == nil {
			t.Fatalf("expected invalid storage id: %q", id)
		}
	}
	if err := ValidateStorageID(valid); err != nil {
		t.Fatalf("expected valid storage id %q: %v", valid, err)
	}
}

func TestTokenURLDoesNotContainAuthPath(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(url, "Bearer") || strings.Contains(url, "/../") || strings.Contains(url, "\\") {
		t.Fatalf("url contains sensitive data: %s", url)
	}
}

func TestDeleteTransactionRollback(t *testing.T) {
	app, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("rollback")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	// Trigger a failing transaction by deleting twice concurrently. Only one should succeed.
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			done <- svc.Delete(context.Background(), id)
		}()
	}
	var successes int
	var notFound int
	for i := 0; i < 2; i++ {
		err := <-done
		if err == nil {
			successes++
		} else if err == ErrStorageNotFound {
			notFound++
		} else {
			t.Fatalf("unexpected delete error: %v", err)
		}
	}
	if successes != 1 || notFound != 1 {
		t.Fatalf("expected 1 success and 1 not found, got %d success, %d not found", successes, notFound)
	}

	if _, err := app.FindCollectionByNameOrId(schema.CollectionStorageFiles); err != nil {
		t.Fatalf("storage files collection missing: %v", err)
	}
}

func TestUploadTransactionRollback(t *testing.T) {
	app, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("data")

	// Interrupt upload by cancelling context mid-stream.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.Upload(ctx, token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err == nil {
		t.Fatal("expected cancelled upload to fail")
	}

	// Ensure token is not consumed so the client can retry with a fresh URL.
	rec, err := svc.repo.GetTokenByHash(schema.WithInternalContext(context.Background()), app, HashToken(token))
	if err != nil {
		t.Fatalf("token not found after rollback: %v", err)
	}
	if rec.GetBool(schema.FieldTokenConsumed) {
		t.Fatal("token should not be consumed after failed upload")
	}
}

func TestMaxFilesCap(t *testing.T) {
	_, svc := newStorageTestApp(t)
	svc.config.MaxFiles = 2

	for i := 0; i < 3; i++ {
		url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
		if err != nil {
			t.Fatal(err)
		}
		token := extractToken(url)
		body := []byte("data")
		_, err = svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
		if i < 2 {
			if err != nil {
				t.Fatalf("upload %d failed: %v", i+1, err)
			}
		} else {
			if err == nil {
				t.Fatal("expected third upload to exceed cap")
			}
			var uploadErr *UploadError
			if !errors.As(err, &uploadErr) || uploadErr.Code != ErrorCodeStorageFull {
				t.Fatalf("expected storage full error, got %v", err)
			}
		}
	}
}

func TestMaxFilesCapConcurrency(t *testing.T) {
	_, svc := newStorageTestApp(t)
	svc.config.MaxFiles = 5

	tokens := make([]string, 10)
	for i := range tokens {
		url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = extractToken(url)
	}

	var success, full int64
	var wg sync.WaitGroup
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := []byte("data")
			_, err := svc.Upload(context.Background(), tokens[i], bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
			if err == nil {
				atomic.AddInt64(&success, 1)
				return
			}
			var uploadErr *UploadError
			if errors.As(err, &uploadErr) && uploadErr.Code == ErrorCodeStorageFull {
				atomic.AddInt64(&full, 1)
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if success != svc.config.MaxFiles {
		t.Fatalf("expected %d successful uploads, got %d", svc.config.MaxFiles, success)
	}
	if full != int64(len(tokens))-svc.config.MaxFiles {
		t.Fatalf("expected %d storage full errors, got %d", len(tokens)-int(svc.config.MaxFiles), full)
	}

	count, err := svc.repo.GetActiveFilesCount(schema.WithInternalContext(context.Background()), svc.app)
	if err != nil {
		t.Fatal(err)
	}
	if count != svc.config.MaxFiles {
		t.Fatalf("expected %d consuming files, got %d", svc.config.MaxFiles, count)
	}
}

func TestMaxFilesIncludesStagedAndDeleting(t *testing.T) {
	app, svc := newStorageTestApp(t)
	svc.config.MaxFiles = 3

	// Create one staged and one deleting record to consume capacity.
	for _, status := range []string{statusStaged, statusDeleting} {
		storageID, err := GenerateStorageID()
		if err != nil {
			t.Fatal(err)
		}
		key := svc.fileKey(storageID)
		body := []byte("data")
		sha, size, err := svc.streamToFile(bytes.NewReader(body), key, "x.txt", int64(len(body))+1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.repo.CreateFile(schema.WithInternalContext(context.Background()), app, FileRecord{
			StorageID:   storageID,
			Sha256:      sha,
			Size:        size,
			ContentType: "text/plain",
			FileKey:     key,
			Filename:    "x.txt",
			Status:      status,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// One active upload should fill the cap.
	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("data")
	if _, err := svc.Upload(context.Background(), extractToken(url), bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err != nil {
		t.Fatalf("upload should succeed: %v", err)
	}

	// The next upload should be rejected because staged+deleting+active = 3.
	url, err = svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Upload(context.Background(), extractToken(url), bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err == nil {
		t.Fatal("expected upload to exceed cap")
	}
}

func TestDefaultTokenMaxSizeClampedToMaxFileSize(t *testing.T) {
	app, svc := newStorageTestApp(t)

	cfg, err := NormalizeConfig(Config{
		MaxFileSize:         1 << 20,
		DefaultTokenMaxSize: 1 << 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultTokenMaxSize != cfg.MaxFileSize {
		t.Fatalf("expected DefaultTokenMaxSize clamped to %d, got %d", cfg.MaxFileSize, cfg.DefaultTokenMaxSize)
	}

	svc.config.DefaultTokenMaxSize = 1 << 30
	svc.config.MaxFileSize = 1 << 20

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := svc.repo.GetTokenByHash(schema.WithInternalContext(context.Background()), app, HashToken(extractToken(url)))
	if err != nil {
		t.Fatal(err)
	}
	if got := int64(rec.GetInt(schema.FieldTokenMaxSize)); got > svc.config.MaxFileSize {
		t.Fatalf("token max size %d exceeds MaxFileSize %d", got, svc.config.MaxFileSize)
	}
}

func TestCleanupWorkerRecoversDeleting(t *testing.T) {
	app, svc := newStorageTestApp(t)

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	finalKey := svc.fileKey(storageID)
	body := []byte("deleting data")

	sha, size, err := svc.streamToFile(bytes.NewReader(body), finalKey, "x.txt", int64(len(body))+1, 0)
	if err != nil {
		t.Fatal(err)
	}

	rec := FileRecord{
		StorageID:   storageID,
		Sha256:      sha,
		Size:        size,
		ContentType: "text/plain",
		FileKey:     finalKey,
		Filename:    "x.txt",
		Status:      statusDeleting,
	}
	if _, err := svc.repo.CreateFile(schema.WithInternalContext(context.Background()), app, rec); err != nil {
		t.Fatal(err)
	}

	if err := svc.RunCleanup(); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.repo.GetFile(schema.WithInternalContext(context.Background()), app, storageID); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("expected file not found after delete recovery, got %v", err)
	}

	fs, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if exists, err := fs.Exists(finalKey); err != nil {
		t.Fatal(err)
	} else if exists {
		t.Fatal("expected final blob to be removed after delete recovery")
	}
}

func TestCleanupOrphanBlobs(t *testing.T) {
	app, svc := newStorageTestApp(t)

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	finalKey := svc.fileKey(storageID)

	fs, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Upload([]byte("orphan"), finalKey); err != nil {
		t.Fatal(err)
	}
	fs.Close()

	if err := svc.RunCleanup(); err != nil {
		t.Fatal(err)
	}

	fs, err = app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if exists, err := fs.Exists(finalKey); err != nil {
		t.Fatal(err)
	} else if exists {
		t.Fatal("expected orphan blob to be removed")
	}
}

func TestCleanupOrphanBlobsSkipsActiveToken(t *testing.T) {
	app, svc := newStorageTestApp(t)

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	finalKey := svc.fileKey(storageID)

	fs, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Upload([]byte("orphan"), finalKey); err != nil {
		t.Fatal(err)
	}
	fs.Close()

	if _, err := svc.repo.CreateToken(schema.WithInternalContext(context.Background()), app, TokenRecord{
		TokenHash: HashToken("token"),
		StorageID: storageID,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		MaxSize:   1024,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.RunCleanup(); err != nil {
		t.Fatal(err)
	}

	fs, err = app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if exists, err := fs.Exists(finalKey); err != nil {
		t.Fatal(err)
	} else if !exists {
		t.Fatal("expected orphan blob to be kept while a token exists")
	}
}

// TestFilenameUTF8Truncation verifies that oversized multibyte filenames are
// truncated on a rune boundary so the persisted name stays valid UTF-8.
func TestFilenameUTF8Truncation(t *testing.T) {
	app, svc := newStorageTestApp(t)

	// "ü" is 2 bytes in UTF-8; 200 of them is 400 bytes, well over the 255 limit.
	long := strings.Repeat("ü", 200) + ".txt"
	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("data")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", long, int64(len(body)))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	rec, err := svc.repo.GetFile(schema.WithInternalContext(context.Background()), app, id)
	if err != nil {
		t.Fatal(err)
	}
	name := rec.GetString(schema.FieldStorageFilename)
	if !utf8.ValidString(name) {
		t.Fatalf("stored filename is not valid UTF-8: %q", name)
	}
	if len(name) > 255 {
		t.Fatalf("stored filename exceeds 255 bytes: %d", len(name))
	}

	// Downloading must still serve the file and emit a safe Content-Disposition
	// with an RFC 5987 filename* parameter.
	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if disp := rr.Header().Get("Content-Disposition"); !strings.Contains(disp, "filename*=UTF-8''") {
		t.Fatalf("expected RFC 5987 filename* in Content-Disposition, got %q", disp)
	}
}

// TestTokenErrorClassification verifies that a failed claim surfaces a precise
// code: consumed, expired, or in-use (pending), rather than a generic rejection.
func TestTokenErrorClassification(t *testing.T) {
	app, svc := newStorageTestApp(t)

	t.Run("consumed", func(t *testing.T) {
		url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
		if err != nil {
			t.Fatal(err)
		}
		token := extractToken(url)
		body := []byte("first")
		if _, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body))); err != nil {
			t.Fatal(err)
		}
		// Replay: the token is now consumed.
		_, err = svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
		var ue *UploadError
		if !errors.As(err, &ue) || ue.Code != ErrorCodeUploadConsumed {
			t.Fatalf("expected upload_consumed, got %v", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		// Create a token that is already expired.
		storageID, err := GenerateStorageID()
		if err != nil {
			t.Fatal(err)
		}
		token, err := GenerateToken()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.repo.CreateToken(schema.WithInternalContext(context.Background()), app, TokenRecord{
			TokenHash: HashToken(token),
			StorageID: storageID,
			ExpiresAt: time.Now().UTC().Add(-time.Hour),
			MaxSize:   1 << 20,
		}); err != nil {
			t.Fatal(err)
		}
		body := []byte("data")
		_, err = svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
		var ue *UploadError
		if !errors.As(err, &ue) || ue.Code != ErrorCodeUploadExpired {
			t.Fatalf("expected upload_expired, got %v", err)
		}
	})

	t.Run("pending", func(t *testing.T) {
		// Create a token held by a live claim that has not been consumed.
		storageID, err := GenerateStorageID()
		if err != nil {
			t.Fatal(err)
		}
		token, err := GenerateToken()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.repo.CreateToken(schema.WithInternalContext(context.Background()), app, TokenRecord{
			TokenHash: HashToken(token),
			StorageID: storageID,
			ExpiresAt: time.Now().UTC().Add(time.Hour),
			MaxSize:   1 << 20,
		}); err != nil {
			t.Fatal(err)
		}
		// Claim it directly so a concurrent attempt observes an active claim.
		if _, err := svc.repo.ClaimToken(schema.WithInternalContext(context.Background()), app, HashToken(token), "other-attempt", time.Now().UTC().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		body := []byte("data")
		_, err = svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
		var ue *UploadError
		if !errors.As(err, &ue) || ue.Code != ErrorCodeUploadPending {
			t.Fatalf("expected upload_pending, got %v", err)
		}
	})
}

// TestFinalizeMissingBothMarksLost verifies that when both the staged and final
// blobs are gone, the record is explicitly marked deleted (no silent staged
// leak) and a distinct data-lost error is surfaced.
func TestFinalizeMissingBothMarksLost(t *testing.T) {
	app, svc := newStorageTestApp(t)

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	stageKey := svc.stageFileKey(storageID, "ghost")
	rec := FileRecord{
		StorageID:   storageID,
		Sha256:      "abc",
		Size:        1,
		ContentType: "text/plain",
		FileKey:     stageKey,
		Filename:    "x.txt",
		Status:      statusStaged,
	}
	created, err := svc.repo.CreateFile(schema.WithInternalContext(context.Background()), app, rec)
	if err != nil {
		t.Fatal(err)
	}

	err = svc.finalizeUpload(created, stageKey, svc.fileKey(storageID))
	if !errors.Is(err, ErrStorageDataLost) {
		t.Fatalf("expected ErrStorageDataLost, got %v", err)
	}

	// The record must no longer resolve as an active file (it was marked deleted).
	if _, err := svc.repo.GetFile(schema.WithInternalContext(context.Background()), app, storageID); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("expected not found after missing-both, got %v", err)
	}
}

// TestDownloadServesBytesAndStatus verifies the observability wrapper captures
// bytes/status without altering response semantics (full and range reads).
func TestDownloadServesBytesAndStatus(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := bytes.Repeat([]byte("b"), 2048)
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}

	full := httptest.NewRequest(http.MethodGet, dl, nil)
	fullRR := httptest.NewRecorder()
	if err := svc.Download(fullRR, full, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if fullRR.Code != http.StatusOK || fullRR.Body.Len() != len(body) {
		t.Fatalf("expected 200 with %d bytes, got %d / %d", len(body), fullRR.Code, fullRR.Body.Len())
	}

	rng := httptest.NewRequest(http.MethodGet, dl, nil)
	rng.Header.Set("Range", "bytes=0-1023")
	rngRR := httptest.NewRecorder()
	if err := svc.Download(rngRR, rng, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rngRR.Code != http.StatusPartialContent || rngRR.Body.Len() != 1024 {
		t.Fatalf("expected 206 with 1024 bytes, got %d / %d", rngRR.Code, rngRR.Body.Len())
	}
}

// TestFilenameRejectsInvalidUTF8 verifies that filenames containing invalid
// UTF-8 byte sequences are rejected at the boundary rather than persisted.
func TestFilenameRejectsInvalidUTF8(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	// 0xff/0xfe are invalid UTF-8 lead bytes.
	invalid := "bad\xff\xfe.txt"
	body := []byte("data")
	_, err = svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", invalid, int64(len(body)))
	if !errors.Is(err, ErrInvalidFilename) {
		t.Fatalf("expected ErrInvalidFilename for invalid UTF-8, got %v", err)
	}
}

// TestRFC5987FilenameEncoding verifies the filename* ext-value uses the RFC 5987
// attr-char grammar: characters outside attr-char (colon, space, non-ASCII) are
// percent-encoded as %XX rather than left raw.
func TestRFC5987FilenameEncoding(t *testing.T) {
	// Direct encoder check.
	enc := rfc5987Encode("café: tea.txt")
	for _, bad := range []string{":", " ", ";", "=", "?"} {
		if strings.Contains(enc, bad) {
			t.Fatalf("rfc5987Encode left %q unencoded in %q", bad, enc)
		}
	}
	if !strings.Contains(enc, "%3A") {
		t.Fatalf("expected colon percent-encoded in %q", enc)
	}
	if !strings.Contains(enc, "%20") {
		t.Fatalf("expected space percent-encoded in %q", enc)
	}
	if !strings.Contains(enc, "%C3%A9") {
		t.Fatalf("expected é (U+00E9) UTF-8 percent-encoded in %q", enc)
	}

	// End-to-end: the emitted Content-Disposition must carry the RFC 5987 form.
	_, svc := newStorageTestApp(t)
	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("data")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "café: tea.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, dl, nil)
	rr := httptest.NewRecorder()
	if err := svc.Download(rr, req, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	disp := rr.Header().Get("Content-Disposition")
	if !strings.Contains(disp, "filename*=UTF-8''") {
		t.Fatalf("expected RFC 5987 filename*, got %q", disp)
	}
	// Extract the filename* ext-value and verify it is attr-char correct.
	idx := strings.Index(disp, "filename*=UTF-8''")
	ext := disp[idx+len("filename*=UTF-8''"):]
	if end := strings.IndexAny(ext, "; \t"); end >= 0 {
		ext = ext[:end]
	}
	for _, bad := range []string{":", " ", ";", "=", "?"} {
		if strings.Contains(ext, bad) {
			t.Fatalf("filename* left %q unencoded in %q (full: %q)", bad, ext, disp)
		}
	}
	if !strings.Contains(ext, "%3A") || !strings.Contains(ext, "%20") {
		t.Fatalf("expected colon/space percent-encoded in filename* %q", ext)
	}
}

// TestNormalizeConfigRejectsInvalid verifies that explicitly invalid config
// (negatives, empty allowed-type entries) is rejected rather than failing open.
func TestNormalizeConfigRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"negative MaxFileSize", Config{MaxFileSize: -1}},
		{"negative MaxFiles", Config{MaxFiles: -1}},
		{"negative token size", Config{DefaultTokenMaxSize: -1}},
		{"negative upload ttl", Config{DefaultUploadTTL: -1}},
		{"negative cleanup interval", Config{CleanupInterval: -1}},
		{"empty allowed type entry", Config{AllowedContentTypes: []string{"image/png", ""}}},
		{"invalid allowed type", Config{AllowedContentTypes: []string{"not a type"}}},
		{"tiny upload lease interval", Config{MaxFileSize: 1 << 20, UploadLeaseInterval: 1 * time.Nanosecond}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NormalizeConfig(tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}

	// Valid configs (including empty allowed-types meaning allow-all) succeed.
	if _, err := NormalizeConfig(Config{MaxFileSize: 1 << 20, AllowedContentTypes: nil}); err != nil {
		t.Fatalf("expected valid config to pass: %v", err)
	}
	if _, err := NormalizeConfig(Config{MaxFileSize: 1 << 20, AllowedContentTypes: []string{"image/*", "text/plain"}}); err != nil {
		t.Fatalf("expected valid content types to pass: %v", err)
	}
}

// TestUploadNonExistentTokenIsGenuineMiss verifies that a token which never
// existed is treated as a genuine CAS miss and surfaced as a plain unauthorized
// error (not classified as consumed/expired and not masked as an internal error).
func TestUploadNonExistentTokenIsGenuineMiss(t *testing.T) {
	_, svc := newStorageTestApp(t)

	_, err := svc.Upload(context.Background(), "nonexistent-token-abcdef", bytes.NewReader([]byte("x")), "text/plain", "x.txt", 1)
	var ue *UploadError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UploadError, got %T %v", err, err)
	}
	if ue.Code != ErrorCodeUnauthorized {
		t.Fatalf("expected unauthorized for genuine miss, got %s", ue.Code)
	}
	if !strings.Contains(ue.Message, "invalid upload token") {
		t.Fatalf("expected invalid-token message, got %q", ue.Message)
	}
}

// TestEvaluatePreconditionsRFC covers the RFC 7232 §6 precedence rules:
// strong-only If-Match comparison (weak must fail), If-Unmodified-Since
// ignored when If-Match is present, If-Modified-Since ignored when
// If-None-Match is present, and sub-second modtime truncation.
func TestEvaluatePreconditionsRFC(t *testing.T) {
	const etag = `"abc"`
	mod := time.Date(2024, 1, 1, 12, 0, 30, 500_000_000, time.UTC) // sub-second precision

	cases := []struct {
		name   string
		header string
		value  string
		code   int
	}{
		// If-Match strong comparison.
		{"If-Match strong match", "If-Match", etag, 0},
		{"If-Match star", "If-Match", "*", 0},
		{"If-Match weak must fail", "If-Match", `W/` + etag, http.StatusPreconditionFailed},
		{"If-Match mismatch", "If-Match", `"other"`, http.StatusPreconditionFailed},
		// If-Unmodified-Since is ignored when If-Match is present and passes.
		{"IUS ignored when If-Match present", "If-Unmodified-Since", "Sun, 06 Nov 1994 08:49:37 GMT", 0},
		// If-None-Match weak comparison (weak matches).
		{"If-None-Match match", "If-None-Match", etag, http.StatusNotModified},
		{"If-None-Match weak matches", "If-None-Match", `W/` + etag, http.StatusNotModified},
		{"If-None-Match star", "If-None-Match", "*", http.StatusNotModified},
		{"If-None-Match mismatch", "If-None-Match", `"other"`, 0},
		// If-Modified-Since truncates modtime to seconds: mod=12:00:30, IMS=12:00:30 → not modified.
		{"If-Modified-Since exact second", "If-Modified-Since", "Mon, 01 Jan 2024 12:00:30 GMT", http.StatusNotModified},
		{"If-Modified-Since older", "If-Modified-Since", "Mon, 01 Jan 2024 12:00:29 GMT", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// If-Match + IUS combo: IUS should be ignored, If-Match governs.
			if tc.name == "IUS ignored when If-Match present" {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("If-Match", etag) // passes
				r.Header.Set("If-Unmodified-Since", tc.value)
				if got := evaluatePreconditions(r, etag, mod); got != 0 {
					t.Fatalf("expected IUS ignored (0), got %d", got)
				}
				return
			}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set(tc.header, tc.value)
			if got := evaluatePreconditions(r, etag, mod); got != tc.code {
				t.Fatalf("expected %d, got %d", tc.code, got)
			}
		})
	}
}

// TestHeadEvaluatesPreconditions verifies HEAD applies the same conditional
// preconditions as GET (returning 304/412) rather than always 200.
func TestHeadEvaluatesPreconditions(t *testing.T) {
	_, svc := newStorageTestApp(t)

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := []byte("head preconditions")
	id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := svc.GetURL(context.Background(), id, AuthContext{})
	if err != nil {
		t.Fatal(err)
	}

	// Establish the etag via a GET.
	getReq := httptest.NewRequest(http.MethodGet, dl, nil)
	getRR := httptest.NewRecorder()
	if err := svc.Download(getRR, getReq, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	etag := getRR.Header().Get("ETag")

	// HEAD with If-None-Match matching → 304.
	h304 := httptest.NewRequest(http.MethodHead, dl, nil)
	h304.Header.Set("If-None-Match", etag)
	rr304 := httptest.NewRecorder()
	if err := svc.Download(rr304, h304, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rr304.Code != http.StatusNotModified {
		t.Fatalf("expected 304 for conditional HEAD, got %d", rr304.Code)
	}
	if rr304.Body.Len() != 0 {
		t.Fatal("expected empty body for 304 HEAD")
	}

	// HEAD with If-Match mismatch → 412.
	h412 := httptest.NewRequest(http.MethodHead, dl, nil)
	h412.Header.Set("If-Match", `"nope"`)
	rr412 := httptest.NewRecorder()
	if err := svc.Download(rr412, h412, id, AuthContext{}); err != nil {
		t.Fatal(err)
	}
	if rr412.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 for conditional HEAD, got %d", rr412.Code)
	}
}

// TestValidateBasePathStrict verifies that BasePath is validated as a clean
// absolute URL path and rejects ambiguous/metagharacters.
func TestValidateBasePathStrict(t *testing.T) {
	valid := []string{"/api/pbvex/storage", "/storage", "/custom/storage-files", "/api2/storage", "/ap"}
	for _, p := range valid {
		if err := validateBasePath(p); err != nil {
			t.Fatalf("expected %q valid: %v", p, err)
		}
	}
	invalid := []string{
		"", "relative/path", "/api?x=1", "/api#frag", "/api/{id}", "/api/{x}",
		"/api/<weird>", "/path with space", "/api%2F", "/files/%2e/x", "/files/%2E%2E/x", "/double//slash", "/",
		"/api\\back", "/api`tick", `/api"quote`, "/api|pipe", "/trailing/", "/./files", "/files/.", "/files/./x", "/files/..", "/files/../x",
		"/api", "/api/other", "/api/pbvex", "/api/pbvex/call", "/api/pbvex/call/nested",
		"/api/pbvex/deployments", "/api/pbvex/deployments/id", "/api/pbvex/realtime",
		"/api/pbvex/jobs", "/api/pbvex/jobs/id", "/api/pbvex/storage/id",
	}
	for _, p := range invalid {
		if err := validateBasePath(p); err == nil {
			t.Fatalf("expected %q to be rejected", p)
		}
	}

	// End-to-end: an invalid BasePath must fail config normalization (fail closed).
	if _, err := NormalizeConfig(Config{MaxFileSize: 1 << 20, BasePath: "/api/{id}"}); err == nil {
		t.Fatal("expected NormalizeConfig to reject brace metacharacters in BasePath")
	}
}

func TestStoragePathAndBaseURLValidation(t *testing.T) {
	validPrefixes := []string{"storage", "pbvex/storage-v2", "tenant_1.files"}
	for _, value := range validPrefixes {
		if err := validateFileStoragePrefix(value); err != nil {
			t.Fatalf("valid prefix %q: %v", value, err)
		}
	}
	for _, value := range []string{"", "/storage", "storage/", "a//b", ".", "..", "a/../b", `a\\b`, "a%2fb", "a b"} {
		if err := validateFileStoragePrefix(value); err == nil {
			t.Fatalf("expected invalid prefix %q", value)
		}
	}

	for _, value := range []string{"https://files.example.com", "https://files.example.com/proxy/v1", "http://127.0.0.1:8090/"} {
		if _, err := NormalizeConfig(Config{BaseURL: value}); err != nil {
			t.Fatalf("valid base url %q: %v", value, err)
		}
	}
	for _, value := range []string{
		"https://user:pass@example.com", "https://example.com/?query=1", "https://example.com/#fragment",
		"https://example.com/a/../b", "https://example.com/a//b", "https://example.com/%2e%2e/secret",
	} {
		if _, err := NormalizeConfig(Config{BaseURL: value}); err == nil {
			t.Fatalf("expected invalid base url %q", value)
		}
	}
}

func TestStopIsIdempotentUnderConcurrency(t *testing.T) {
	_, svc := newStorageTestApp(t)
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- svc.Stop()
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestCleanupDoesNotFinalizeUploadingRecord verifies that a durable uploading
// reservation is never finalized by cleanup (its stage blob may not be ready),
// covering both the reservation→stream and stream→persist windows.
func TestCleanupDoesNotFinalizeUploadingRecord(t *testing.T) {
	app, svc := newStorageTestApp(t)

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	stageKey := svc.stageFileKey(storageID, "attempt")
	// Simulate a reservation that reached persist (stage blob exists) but never
	// committed to staged — e.g. a crash in the stream→persist window.
	body := []byte("partial")
	if _, _, err := svc.streamToFile(bytes.NewReader(body), stageKey, "x.txt", int64(len(body))+1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.CreateFile(schema.WithInternalContext(context.Background()), app, FileRecord{
		StorageID: storageID, ContentType: "text/plain", FileKey: stageKey,
		Filename: "x.txt", Status: statusUploading, Owner: "attempt",
		LeaseUntil: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	// Cleanup must NOT finalize (activate) an uploading record, even though a
	// stage blob exists. The lease is fresh, so it stays uploading.
	if err := svc.RunCleanup(); err != nil {
		t.Fatal(err)
	}
	rec, err := svc.repo.GetFileByIDAnyStatus(schema.WithInternalContext(context.Background()), app, storageID)
	if err != nil {
		t.Fatalf("expected uploading record to remain, got %v", err)
	}
	if got := rec.GetString(schema.FieldStorageStatus); got != statusUploading {
		t.Fatalf("expected uploading (not finalized), got %q", got)
	}
	// No active download URL should resolve.
	if dl, _ := svc.GetURL(context.Background(), storageID, AuthContext{}); dl != "" {
		t.Fatalf("expected no download url for uploading record, got %s", dl)
	}
}

// TestCleanupReclaimsExpiredUploading verifies that an uploading reservation
// whose lease has expired (without renewal) is hard-deleted, releasing its
// capacity and storage id.
func TestCleanupReclaimsExpiredUploading(t *testing.T) {
	app, svc := newStorageTestApp(t)
	svc.config.MaxFiles = 2

	// Create an uploading record whose lease already expired.
	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.repo.CreateFile(schema.WithInternalContext(context.Background()), app, FileRecord{
		StorageID: storageID, ContentType: "text/plain", FileKey: svc.stageFileKey(storageID, "a"),
		Filename: "x.txt", Status: statusUploading, Owner: "owner-a",
		LeaseUntil: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.RunCleanup(); err != nil {
		t.Fatal(err)
	}

	// The record must be gone (hard-deleted), freeing the unique storage id.
	if _, err := svc.repo.GetFileByIDAnyStatus(schema.WithInternalContext(context.Background()), app, storageID); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("expected expired uploading record reclaimed, got %v", err)
	}
	// Capacity is freed: a fresh upload using the same storage id must reserve.
	if _, err := svc.repo.CreateFile(schema.WithInternalContext(context.Background()), app, FileRecord{
		StorageID: storageID, ContentType: "text/plain", FileKey: svc.stageFileKey(storageID, "b"),
		Filename: "x.txt", Status: statusUploading,
	}); err != nil {
		t.Fatalf("expected storage id reusable after reclaim: %v", err)
	}
}

// TestReserveBeforeReadNoOversubscription verifies that capacity is reserved
// before the request body is read: under a full cap, rejected uploads never
// start reading, and the number of uploading reservations never exceeds MaxFiles.
func TestReserveBeforeReadNoOversubscription(t *testing.T) {
	app, svc := newStorageTestApp(t)
	svc.config.MaxFiles = 3

	const n = 8
	tokens := make([]string, n)
	for i := range tokens {
		u, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = extractToken(u)
	}

	// Gated readers block until released so successful reservations overlap.
	gate := make(chan struct{})
	readers := make([]*gatedReader, n)
	for i := range readers {
		readers[i] = &gatedReader{data: []byte("body"), gate: gate}
	}

	// Track how many readers were actually read from (reservation must precede read).
	var peak int64
	var mu sync.Mutex
	go func() {
		for {
			select {
			case <-gate:
				return
			default:
				if c, err := svc.repo.GetActiveFilesCount(schema.WithInternalContext(context.Background()), app); err == nil {
					mu.Lock()
					if c > peak {
						peak = c
					}
					mu.Unlock()
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	var success, full int64
	var readsAttempted int64
	var wg sync.WaitGroup
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			readers[i].onRead = func() { atomic.AddInt64(&readsAttempted, 1) }
			_, err := svc.Upload(context.Background(), tokens[i], readers[i], "text/plain", "x.txt", int64(len(readers[i].data)))
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

	// Give reservations a moment to land, then release the gate.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	mu.Lock()
	p := peak
	mu.Unlock()

	if p > svc.config.MaxFiles {
		t.Fatalf("oversubscription: peak %d exceeded cap %d", p, svc.config.MaxFiles)
	}
	if success != svc.config.MaxFiles {
		t.Fatalf("expected %d successful uploads, got %d", svc.config.MaxFiles, success)
	}
	if full != int64(n)-svc.config.MaxFiles {
		t.Fatalf("expected %d storage-full, got %d", int64(n)-svc.config.MaxFiles, full)
	}
	// Rejected uploads must not have read their body (reservation precedes read).
	if got := atomic.LoadInt64(&readsAttempted); got > svc.config.MaxFiles {
		t.Fatalf("expected at most %d body reads (only reserved uploads), got %d", svc.config.MaxFiles, got)
	}
}

type gatedReader struct {
	data   []byte
	gate   chan struct{}
	i      int
	onRead func()
}

func (g *gatedReader) Read(p []byte) (int, error) {
	select {
	case <-g.gate:
	default:
		if g.onRead != nil {
			g.onRead()
		}
		select {
		case <-g.gate:
		}
	}
	if g.i >= len(g.data) {
		return 0, io.EOF
	}
	n := copy(p, g.data[g.i:])
	g.i += n
	return n, nil
}

// TestCommitUploadClassifiesContention verifies that a CAS failure in
// commitUpload (ConsumeToken after slow staging) surfaces ErrTokenClaimFailed so
// the caller classifies the token state instead of returning a generic 500.
func TestCommitUploadClassifiesContention(t *testing.T) {
	app, svc := newStorageTestApp(t)

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	token, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := HashToken(token)
	attempt := "attempt-contention"

	// Create + claim the token.
	if _, err := svc.repo.CreateToken(schema.WithInternalContext(context.Background()), app, TokenRecord{
		TokenHash: tokenHash, StorageID: storageID,
		ExpiresAt: time.Now().UTC().Add(time.Hour), MaxSize: 1 << 20,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.ClaimToken(schema.WithInternalContext(context.Background()), app, tokenHash, attempt, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	_, err = svc.repo.CreateFile(schema.WithInternalContext(context.Background()), app, FileRecord{
		StorageID: storageID, ContentType: "text/plain", FileKey: svc.stageFileKey(storageID, attempt),
		Filename: "x.txt", Status: statusUploading, Owner: attempt,
		LeaseUntil: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the claim being lost during slow staging.
	if err := svc.repo.ReleaseClaim(schema.WithInternalContext(context.Background()), app, tokenHash, attempt); err != nil {
		t.Fatal(err)
	}

	// commitUpload must surface the CAS failure, not mask it as internal.
	err = svc.commitUpload(schema.WithInternalContext(context.Background()), app, tokenHash, attempt, storageID, "abc", 3, svc.stageFileKey(storageID, attempt))
	if !errors.Is(err, ErrTokenClaimFailed) {
		t.Fatalf("expected ErrTokenClaimFailed for contention, got %v", err)
	}

	// The Upload-level classification of this state must be a precise (non-500) code.
	classified := svc.classifyTokenFailure(schema.WithInternalContext(context.Background()), app, tokenHash)
	var ue *UploadError
	if !errors.As(classified, &ue) || ue.Code == ErrorCodeInternal {
		t.Fatalf("expected classified (non-internal) error for contention, got %v", classified)
	}
}

// TestLeaseRenewalPreventsReclaim proves the durable owner/lease contract:
// while an upload actively renews its lease, cleanup cannot reclaim the
// reservation (renewal-vs-cleanup CAS interleaving); once renewal stops, the
// expired lease is reclaimed atomically.
func TestLeaseRenewalPreventsReclaim(t *testing.T) {
	app, svc := newStorageTestApp(t)
	svc.config.UploadLeaseInterval = 40 * time.Millisecond
	ctx := schema.WithInternalContext(context.Background())

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	owner := "owner-renew"
	if _, err := svc.repo.CreateFile(ctx, app, FileRecord{
		StorageID: storageID, ContentType: "text/plain", FileKey: svc.stageFileKey(storageID, owner),
		Filename: "x.txt", Status: statusUploading, Owner: owner,
		LeaseUntil: time.Now().UTC().Add(svc.leaseInterval()),
	}); err != nil {
		t.Fatal(err)
	}

	// Renew the lease actively while cleanup runs. The CAS renewal keeps the
	// lease ahead of every reclaim attempt.
	stop := svc.startUploadLeaseRenewer(storageID, owner)

	for i := 0; i < 6; i++ {
		time.Sleep(30 * time.Millisecond) // longer than a single lease interval
		if err := svc.RunCleanup(); err != nil {
			stop()
			t.Fatalf("cleanup failed: %v", err)
		}
		if _, err := svc.repo.GetFileByIDAnyStatus(ctx, app, storageID); err != nil {
			stop()
			t.Fatalf("cleanup reclaimed an actively-renewed upload at iter %d: %v", i, err)
		}
	}

	// Stop renewing; once the lease expires, cleanup reclaims the reservation.
	stop()
	time.Sleep(2 * svc.leaseInterval())
	reclaimed, err := svc.recoverUploading(ctx)
	if err != nil {
		t.Fatalf("recoverUploading failed: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("expected 1 reclaimed after renewal stopped, got %d", reclaimed)
	}
	if _, err := svc.repo.GetFileByIDAnyStatus(ctx, app, storageID); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("expected record gone after reclaim, got %v", err)
	}
}

// TestCleanupDuringSlowUpload drives the full Upload path with a gated slow
// reader and a tiny lease: cleanup runs while staging is blocked yet cannot
// reclaim the reservation because Upload renews its lease. Releasing the gate
// lets the upload complete successfully.
func TestCleanupDuringSlowUpload(t *testing.T) {
	app, svc := newStorageTestApp(t)
	svc.config.UploadLeaseInterval = 40 * time.Millisecond

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)

	gate := make(chan struct{})
	reader := &gatedReader{data: bytes.Repeat([]byte("x"), 256), gate: gate}

	type uploadResult struct {
		id  string
		err error
	}
	res := make(chan uploadResult, 1)
	go func() {
		id, err := svc.Upload(context.Background(), token, reader, "text/plain", "x.txt", int64(len(reader.data)))
		res <- uploadResult{id, err}
	}()

	// Wait for the uploading reservation to appear.
	ctx := schema.WithInternalContext(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := svc.repo.GetActiveFilesCount(ctx, app); err == nil && c > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c, _ := svc.repo.GetActiveFilesCount(ctx, app); c == 0 {
		t.Fatal("expected uploading reservation to be created")
	}

	// While staging is blocked on the gated reader, run cleanup well past a
	// single lease interval; the renewal loop must keep the reservation alive.
	for i := 0; i < 5; i++ {
		time.Sleep(30 * time.Millisecond)
		if err := svc.RunCleanup(); err != nil {
			t.Fatalf("cleanup failed during slow upload: %v", err)
		}
		if c, _ := svc.repo.GetActiveFilesCount(ctx, app); c == 0 {
			t.Fatalf("cleanup reclaimed an active upload at iter %d", i)
		}
	}

	// Release the gate; the upload completes and commits despite prior cleanup.
	close(gate)
	r := <-res
	if r.err != nil {
		t.Fatalf("upload failed after slow staging: %v", r.err)
	}
	if r.id == "" {
		t.Fatal("expected storage id")
	}
}

// TestReleaseReservationOwnershipCAS verifies that release only deletes the
// reservation owned by the given owner; a different owner (or a reclaimed
// record) is untouched.
func TestReleaseReservationOwnershipCAS(t *testing.T) {
	app, svc := newStorageTestApp(t)
	ctx := schema.WithInternalContext(context.Background())

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.CreateFile(ctx, app, FileRecord{
		StorageID: storageID, ContentType: "text/plain", FileKey: svc.stageFileKey(storageID, "a"),
		Filename: "x.txt", Status: statusUploading, Owner: "owner-real",
		LeaseUntil: time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	// A release with the wrong owner must not delete the reservation.
	if err := svc.releaseReservation(storageID, "owner-wrong"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.GetFileByIDAnyStatus(ctx, app, storageID); err != nil {
		t.Fatalf("reservation should remain after wrong-owner release: %v", err)
	}

	// The correct owner release deletes it.
	if err := svc.releaseReservation(storageID, "owner-real"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.GetFileByIDAnyStatus(ctx, app, storageID); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("expected reservation gone after owner release, got %v", err)
	}
}

// TestLeaseSurvivesCanceledContextDuringStaging proves the renewer's lifetime
// follows the actual staging operation, not the request context: a canceled
// context does not stop renewal while a gated reader keeps staging alive, so
// cleanup cannot reclaim the reservation. After the gate is released the
// operation finishes and the reservation is released.
func TestLeaseSurvivesCanceledContextDuringStaging(t *testing.T) {
	app, svc := newStorageTestApp(t)
	svc.config.UploadLeaseInterval = 40 * time.Millisecond

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)

	gate := make(chan struct{})
	reader := &gatedReader{data: bytes.Repeat([]byte("x"), 256), gate: gate}

	ctx, cancel := context.WithCancel(context.Background())
	type uploadResult struct{ err error }
	res := make(chan uploadResult, 1)
	go func() {
		_, err := svc.Upload(ctx, token, reader, "text/plain", "x.txt", int64(len(reader.data)))
		res <- uploadResult{err}
	}()

	internalCtx := schema.WithInternalContext(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := svc.repo.GetActiveFilesCount(internalCtx, app); err == nil && c > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c, _ := svc.repo.GetActiveFilesCount(internalCtx, app); c == 0 {
		t.Fatal("expected uploading reservation to be created")
	}

	// Cancel the request context while staging is blocked on the gated reader.
	// The renewal loop must keep the lease alive regardless of ctx cancellation.
	cancel()

	// Run cleanup well past the lease interval; the reservation must survive.
	for i := 0; i < 5; i++ {
		time.Sleep(30 * time.Millisecond)
		if err := svc.RunCleanup(); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
		if c, _ := svc.repo.GetActiveFilesCount(internalCtx, app); c == 0 {
			t.Fatalf("cleanup reclaimed reservation during blocked staging despite active renewal (iter %d)", i)
		}
	}

	// Release the gate: staging finishes, persist runs, renewal stops (joined),
	// then commitUpload may fail due to canceled ctx — either way the goroutine
	// returns and the reservation is released.
	close(gate)
	select {
	case <-res:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not return after gate release")
	}

	// No uploading records should remain after the upload attempt finishes.
	time.Sleep(50 * time.Millisecond)
	records, _ := svc.repo.GetFilesByStatus(internalCtx, app, statusUploading)
	if len(records) > 0 {
		t.Fatalf("expected no uploading records after upload finished, got %d", len(records))
	}
}

// TestLeaseSurvivesSlowPersist proves the renewer keeps the lease alive while a
// backend persist is blocked: cleanup runs past the lease interval but cannot
// reclaim until persist completes and the renewer is joined.
func TestLeaseSurvivesSlowPersist(t *testing.T) {
	app, svc := newStorageTestApp(t)
	svc.config.UploadLeaseInterval = 40 * time.Millisecond

	persistStarted := make(chan struct{})
	persistGate := make(chan struct{})
	svc.persistHook = func() {
		close(persistStarted)
		<-persistGate
	}

	url, err := svc.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	token := extractToken(url)
	body := bytes.Repeat([]byte("x"), 256)

	type uploadResult struct {
		id  string
		err error
	}
	res := make(chan uploadResult, 1)
	go func() {
		id, err := svc.Upload(context.Background(), token, bytes.NewReader(body), "text/plain", "x.txt", int64(len(body)))
		res <- uploadResult{id, err}
	}()

	internalCtx := schema.WithInternalContext(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-persistStarted:
		default:
			time.Sleep(5 * time.Millisecond)
			continue
		}
		break
	}
	select {
	case <-persistStarted:
	default:
		t.Fatal("expected persist to have started (blocked on persistGate)")
	}

	// Persist is now blocked on persistGate. Run cleanup well past the lease
	// interval; the reservation must survive because the renewal loop is active.
	for i := 0; i < 5; i++ {
		time.Sleep(30 * time.Millisecond)
		if err := svc.RunCleanup(); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
		if c, _ := svc.repo.GetActiveFilesCount(internalCtx, app); c == 0 {
			t.Fatalf("cleanup reclaimed reservation during blocked persist (iter %d)", i)
		}
	}

	// Release the persist gate: persist completes, renewal stops (joined),
	// commit and finalize proceed normally.
	close(persistGate)
	select {
	case r := <-res:
		if r.err != nil {
			t.Fatalf("upload failed after slow persist: %v", r.err)
		}
		if r.id == "" {
			t.Fatal("expected storage id")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not return after persist gate release")
	}
}

// TestEventualReclaimAfterNoRenewer proves that a reservation whose renewal
// stopped (simulated crash) is eventually reclaimed by cleanup once the lease
// expires.
func TestEventualReclaimAfterNoRenewer(t *testing.T) {
	app, svc := newStorageTestApp(t)
	svc.config.UploadLeaseInterval = 40 * time.Millisecond
	ctx := schema.WithInternalContext(context.Background())

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}
	owner := "crashed-owner"
	if _, err := svc.repo.CreateFile(ctx, app, FileRecord{
		StorageID: storageID, ContentType: "text/plain", FileKey: svc.stageFileKey(storageID, owner),
		Filename: "x.txt", Status: statusUploading, Owner: owner,
		LeaseUntil: time.Now().UTC().Add(svc.leaseInterval()),
	}); err != nil {
		t.Fatal(err)
	}

	// No renewal goroutine — simulates a crashed process.
	time.Sleep(2 * svc.leaseInterval())

	reclaimed, err := svc.recoverUploading(ctx)
	if err != nil {
		t.Fatalf("recoverUploading failed: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("expected 1 reclaimed after no-renewer crash, got %d", reclaimed)
	}
	if _, err := svc.repo.GetFileByIDAnyStatus(ctx, app, storageID); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("expected record gone after reclaim, got %v", err)
	}
}

// TestTinyLeaseIntervalNoPanic verifies that a 1-nanosecond lease interval does
// not panic NewTicker (cadence is clamped to a strictly positive value).
func TestTinyLeaseIntervalNoPanic(t *testing.T) {
	_, svc := newStorageTestApp(t)
	svc.config.UploadLeaseInterval = 1 * time.Nanosecond

	stop := svc.startUploadLeaseRenewer("test-id", "test-owner")
	time.Sleep(10 * time.Millisecond)
	stop()
}

// TestDeleteUploadingOwnerCAS proves that a stale cleanup which snapshotted a
// previous owner cannot delete a reservation now owned by a different attempt:
// the id+status+owner+lease CAS causes zero rows affected when the owner changed.
func TestDeleteUploadingOwnerCAS(t *testing.T) {
	app, svc := newStorageTestApp(t)
	ctx := schema.WithInternalContext(context.Background())

	storageID, err := GenerateStorageID()
	if err != nil {
		t.Fatal(err)
	}

	// Create an uploading reservation with an expired lease, owned by A.
	created, err := svc.repo.CreateFile(ctx, app, FileRecord{
		StorageID: storageID, ContentType: "text/plain", FileKey: svc.stageFileKey(storageID, "owner-A"),
		Filename: "x.txt", Status: statusUploading, Owner: "owner-A",
		LeaseUntil: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate an owner takeover: the record now belongs to owner-B.
	created.Set(schema.FieldStorageOwner, "owner-B")
	if err := app.SaveWithContext(ctx, created); err != nil {
		t.Fatal(err)
	}

	// Stale cleanup that snapshotted owner-A must NOT delete the record.
	gone, err := svc.repo.DeleteUploadingIfLeaseExpired(ctx, app, created.Id, "owner-A", time.Now().UTC())
	if err != nil {
		t.Fatalf("DeleteUploadingIfLeaseExpired failed: %v", err)
	}
	if gone {
		t.Fatal("stale cleanup deleted a reservation now owned by another attempt")
	}

	// The record must still exist, now owned by owner-B.
	rec, err := svc.repo.GetFileByIDAnyStatus(ctx, app, storageID)
	if err != nil {
		t.Fatalf("expected record to survive stale cleanup: %v", err)
	}
	if owner := rec.GetString(schema.FieldStorageOwner); owner != "owner-B" {
		t.Fatalf("expected owner-B, got %q", owner)
	}

	// Cleanup with the correct owner (owner-B) reclaims it.
	gone, err = svc.repo.DeleteUploadingIfLeaseExpired(ctx, app, created.Id, "owner-B", time.Now().UTC())
	if err != nil {
		t.Fatalf("DeleteUploadingIfLeaseExpired failed: %v", err)
	}
	if !gone {
		t.Fatal("expected cleanup to reclaim with correct owner")
	}
}
