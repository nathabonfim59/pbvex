package storage

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/nathabonfim59/pbvex/backend/hosting/storagequota"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// newHostedQuotaPair wires a reference quota service on a Unix socket to a
// hosted observer, mirroring the parent wiring seam.
func newHostedQuotaPair(t *testing.T, capacityBytes int64) (QuotaObserver, *storagequota.ReferenceQuotaService) {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "quota.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	svc := storagequota.NewReferenceQuotaService(1000, capacityBytes)
	server := &http.Server{Handler: svc, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = ln.Close()
	})

	client, err := storagequota.NewClient(hosting.Config{
		Enabled:    true,
		SocketPath: socket,
		Timeout:    2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return NewHostedQuotaObserver(client), svc
}

func TestHostedObserverNilClient(t *testing.T) {
	if obs := NewHostedQuotaObserver(nil); obs != nil {
		t.Fatal("expected nil observer for nil client")
	}
}

func TestHostedObserverUnavailableFailsClosed(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "missing.sock")
	client, err := storagequota.NewClient(hosting.Config{
		Enabled:    true,
		SocketPath: socket,
		Timeout:    500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	obs := NewHostedQuotaObserver(client)

	_, err = obs.Reserve(context.Background(), QuotaReservationRequest{
		OpID: "op-1", Purpose: QuotaPurposeUpload, WorstCase: 10,
	})
	if !errors.Is(err, ErrQuotaUnavailable) {
		t.Fatalf("expected ErrQuotaUnavailable for dead socket, got %v", err)
	}
}

func TestHostedObserverDenialMapping(t *testing.T) {
	obs, svc := newHostedQuotaPair(t, 10)
	_, err := obs.Reserve(context.Background(), QuotaReservationRequest{
		OpID: "op-1", Purpose: QuotaPurposeUpload, WorstCase: 100,
	})
	if !errors.Is(err, ErrQuotaDenied) {
		t.Fatalf("expected ErrQuotaDenied, got %v", err)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("denied reservation must not charge, used=%d", got)
	}
}

func TestHostedUploadEndToEndDeniedThenAllowed(t *testing.T) {
	obs, svc := newHostedQuotaPair(t, 4096)
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	svcStorage, err := NewService(app, NewRepo(), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	svcStorage.SetQuotaObserver(obs)

	// Worst-case (64 MiB default cap) exceeds the tiny host capacity.
	uploadURL, err := svcStorage.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svcStorage.Upload(context.Background(), extractToken(uploadURL), bytes.NewReader([]byte("hi")), "text/plain", "a.txt", 2)
	var uploadErr *UploadError
	if !errors.As(err, &uploadErr) || uploadErr.Code != ErrorCodeStorageFull {
		t.Fatalf("expected storage_full under host capacity, got %v", err)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("denied upload must not charge the host ledger, used=%d", got)
	}

	// A host allowing the real worst-case bound admits the upload and the
	// actual bytes are settled afterwards.
	app2, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app2.Cleanup)
	if err := schema.Bootstrap(app2); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MaxFileSize = 1024
	svcStorage2, err := NewService(app2, NewRepo(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	svcStorage2.SetQuotaObserver(obs)
	uploadURL2, err := svcStorage2.GenerateUploadURL(context.Background(), AuthContext{})
	if err != nil {
		t.Fatal(err)
	}
	id, err := svcStorage2.Upload(context.Background(), extractToken(uploadURL2), bytes.NewReader([]byte("hello hosted")), "text/plain", "b.txt", 12)
	if err != nil {
		t.Fatalf("expected hosted upload to succeed: %v", err)
	}
	if id == "" {
		t.Fatal("expected storage id")
	}
	if got := svc.UsedBytes(); got != 12 {
		t.Fatalf("expected settled actual 12 on the host ledger, got %d", got)
	}

	// Deleting the object credits the host ledger.
	if err := svcStorage2.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("expected ledger back to zero after deletion credit, got %d", got)
	}
}
