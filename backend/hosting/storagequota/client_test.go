package storagequota

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/hosting"
)

// newQuotaTestServer binds a reference service on a fresh Unix socket and
// returns a connected client plus the service for assertions.
func newQuotaTestServer(t *testing.T, limit int, capacityBytes int64) (*Client, *ReferenceQuotaService) {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "quota.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewReferenceQuotaService(limit, capacityBytes)
	server := &http.Server{Handler: svc, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = ln.Close()
	})

	client, err := NewClient(hosting.Config{
		Enabled:     true,
		SocketPath:  socket,
		Timeout:     2 * time.Second,
		MaxInFlight: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, svc
}

func TestDisabledConfigIsRejected(t *testing.T) {
	if _, err := NewClient(hosting.Config{}); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected disabled config rejection, got %v", err)
	}
	if _, err := NewClient(hosting.Config{Enabled: true, SocketPath: "relative"}); err == nil {
		t.Fatal("expected relative socket path rejection")
	}
}

func TestHandshakeListsStorageCapability(t *testing.T) {
	client, _ := newQuotaTestServer(t, 100, 1<<20)
	hello, err := client.Handshake(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hello.Version != Version || hello.Implementation == "" {
		t.Fatalf("unexpected hello: %+v", hello)
	}
	found := false
	for _, c := range hello.Capabilities {
		if c == CapabilityStorageReserve {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s capability, got %v", CapabilityStorageReserve, hello.Capabilities)
	}
}

func TestReserveSettleReleaseCreditRoundtrip(t *testing.T) {
	client, svc := newQuotaTestServer(t, 100, 1000)
	ctx := context.Background()

	d, err := client.ReserveStorage(ctx, ReserveStorageRequest{
		RequestID: "rq-1", Purpose: PurposeUpload, StorageID: "pbv_abc", Key: "storage/pbv_abc/blob", Bytes: 400,
	})
	if err != nil || !d.Allowed || d.ReservationID == "" {
		t.Fatalf("reserve failed: %v %+v", err, d)
	}
	if d.Code != "allowed" || d.PolicyVersion == "" {
		t.Fatalf("unexpected decision: %+v", d)
	}

	// A settle above the reserved bound is clamped.
	a, err := client.SettleStorage(ctx, SettleStorageRequest{ReservationID: d.ReservationID, Bytes: 500})
	if err != nil || a.ChargedBytes != 400 {
		t.Fatalf("settle: %v ack=%+v", err, a)
	}
	if got := svc.UsedBytes(); got != 400 {
		t.Fatalf("expected used 400, got %d", got)
	}

	// Settled reservations are terminal: a release conflicts.
	if _, err := client.ReleaseStorage(ctx, ReleaseStorageRequest{ReservationID: d.ReservationID}); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected release-after-settle conflict, got %v", err)
	}

	// Freed bytes are credited by event identity.
	if _, err := client.CreditStorage(ctx, CreditStorageRequest{EventID: "e-1", StorageID: "pbv_abc", Key: "storage/pbv_abc/blob", Bytes: 400}); err != nil {
		t.Fatal(err)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("expected used 0 after credit, got %d", got)
	}

	// A reserved-then-released reservation never charges.
	d2, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-2", Purpose: PurposeVariant, Bytes: 300})
	if err != nil || !d2.Allowed {
		t.Fatalf("reserve 2: %v %+v", err, d2)
	}
	if _, err := client.ReleaseStorage(ctx, ReleaseStorageRequest{ReservationID: d2.ReservationID}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SettleStorage(ctx, SettleStorageRequest{ReservationID: d2.ReservationID, Bytes: 1}); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected settle-after-release conflict, got %v", err)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("released reservation must not charge, used=%d", got)
	}
}

func TestReserveIdempotentReplayAndConflict(t *testing.T) {
	client, _ := newQuotaTestServer(t, 100, 1000)
	ctx := context.Background()
	req := ReserveStorageRequest{RequestID: "rq-1", Purpose: PurposeUpload, Bytes: 100}

	d1, err := client.ReserveStorage(ctx, req)
	if err != nil || !d1.Allowed {
		t.Fatalf("first reserve: %v", err)
	}
	d2, err := client.ReserveStorage(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if d1.ReservationID != d2.ReservationID {
		t.Fatalf("expected identical replay decision, got %+v vs %+v", d1, d2)
	}

	// Same request ID with a different body conflicts (HTTP 409 maps to
	// ErrUnavailable; it must never be read as a permission decision).
	req.Bytes = 999
	if _, err := client.ReserveStorage(ctx, req); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestCapacityExhaustedDeniesAndRecovers(t *testing.T) {
	client, svc := newQuotaTestServer(t, 100, 500)
	ctx := context.Background()

	d, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-1", Purpose: PurposeUpload, Bytes: 600})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.Code != CodeQuotaExhausted || d.ReservationID != "" {
		t.Fatalf("expected quota_exhausted denial, got %+v", d)
	}

	svc.SetCapacity(1000)
	d2, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-2", Purpose: PurposeUpload, Bytes: 600})
	if err != nil || !d2.Allowed {
		t.Fatalf("expected allow after capacity raise: %v %+v", err, d2)
	}
}

func TestCreditDedupConflictAndFloor(t *testing.T) {
	client, svc := newQuotaTestServer(t, 100, 1000)
	ctx := context.Background()

	if _, err := client.CreditStorage(ctx, CreditStorageRequest{EventID: "e-1", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreditStorage(ctx, CreditStorageRequest{EventID: "e-1", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("duplicate credit must dedupe, used=%d", got)
	}
	if _, err := client.CreditStorage(ctx, CreditStorageRequest{EventID: "e-1", Bytes: 200}); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected conflicting credit to conflict, got %v", err)
	}
	// Credits floor usage at zero.
	if _, err := client.CreditStorage(ctx, CreditStorageRequest{EventID: "e-2", Bytes: 50}); err != nil {
		t.Fatal(err)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("expected floored usage, got %d", got)
	}
}

func TestConcurrentReservesNeverExceedCapacity(t *testing.T) {
	const (
		capacity  = int64(1000)
		perTicket = int64(100)
		total     = 40
	)
	client, svc := newQuotaTestServer(t, 1000, capacity)
	// Raise the local in-flight allowance so the 40 concurrent attempts
	// exercise service capacity, not client saturation.
	client.slots = make(chan struct{}, total)
	ctx := context.Background()

	var mu sync.Mutex
	var allowed int
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d, err := client.ReserveStorage(ctx, ReserveStorageRequest{
				RequestID: fmt.Sprintf("rq-%d", i),
				Purpose:   PurposeUpload,
				Bytes:     perTicket,
			})
			if err != nil {
				t.Error(err)
				return
			}
			if d.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else if d.Code != CodeQuotaExhausted {
				t.Errorf("unexpected denial code %q", d.Code)
			}
		}(i)
	}
	wg.Wait()

	if int64(allowed)*perTicket > capacity {
		t.Fatalf("allowed reservations %d exceed capacity %d", allowed, capacity)
	}
	if allowed != 10 {
		t.Fatalf("expected exactly 10 allowed reservations, got %d", allowed)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("inflight-only reservations must not be charged yet, used=%d", got)
	}
}

func TestRecordLimitReturnsUnavailable(t *testing.T) {
	client, _ := newQuotaTestServer(t, 2, 1<<20)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := client.ReserveStorage(ctx, ReserveStorageRequest{
			RequestID: fmt.Sprintf("rq-%d", i), Purpose: PurposeUpload, Bytes: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.ReserveStorage(ctx, ReserveStorageRequest{
		RequestID: "rq-overflow", Purpose: PurposeUpload, Bytes: 1,
	}); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected 503 saturation as ErrUnavailable, got %v", err)
	}
}

func TestMalformedResponsesFailAsProtocolErrors(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "bad.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/hello":
			_, _ = w.Write([]byte(`{"version":"2","implementation":"x","capabilities":[]}`))
		case "/v1/storage/reserve":
			_, _ = w.Write([]byte(`{"allowed":true,"code":"allowed","policyVersion":"p1","reservationId":"r1","extra":1}`))
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })

	client, err := NewClient(hosting.Config{Enabled: true, SocketPath: socket, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	ctx := context.Background()

	if _, err := client.Handshake(ctx); !errors.Is(err, hosting.ErrProtocol) {
		t.Fatalf("expected version rejection, got %v", err)
	}
	if _, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq", Purpose: PurposeUpload, Bytes: 1}); !errors.Is(err, hosting.ErrProtocol) {
		t.Fatalf("expected unknown-field rejection, got %v", err)
	}
	if _, err := client.CreditStorage(ctx, CreditStorageRequest{EventID: "e"}); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected non-200 as unavailable, got %v", err)
	}
}

func TestLocalValidationRejectsInvalidRequests(t *testing.T) {
	client, _ := newQuotaTestServer(t, 10, 1000)
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{"reserve empty request id", func() error {
			_, err := client.ReserveStorage(ctx, ReserveStorageRequest{Purpose: PurposeUpload, Bytes: 1})
			return err
		}},
		{"reserve unknown purpose", func() error {
			_, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq", Purpose: "other", Bytes: 1})
			return err
		}},
		{"reserve negative bytes", func() error {
			_, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq", Purpose: PurposeUpload, Bytes: -1})
			return err
		}},
		{"settle empty reservation", func() error {
			_, err := client.SettleStorage(ctx, SettleStorageRequest{Bytes: 1})
			return err
		}},
		{"release bad token", func() error {
			_, err := client.ReleaseStorage(ctx, ReleaseStorageRequest{ReservationID: "bad token!"})
			return err
		}},
		{"credit negative bytes", func() error {
			_, err := client.CreditStorage(ctx, CreditStorageRequest{EventID: "e", Bytes: -5})
			return err
		}},
	}
	for _, tc := range cases {
		if err := tc.call(); !errors.Is(err, hosting.ErrProtocol) {
			t.Fatalf("%s: expected ErrProtocol, got %v", tc.name, err)
		}
	}
}

func TestClientSaturationReturnsBusyWithoutQueue(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "slow.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"allowed":true,"code":"allowed","policyVersion":"p1","reservationId":"r1"}`))
	}), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })

	client, err := NewClient(hosting.Config{Enabled: true, SocketPath: socket, Timeout: 2 * time.Second, MaxInFlight: 1})
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	t.Cleanup(func() {
		client.Close()
		once.Do(func() { close(release) })
	})
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-1", Purpose: PurposeUpload, Bytes: 1})
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	if _, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-2", Purpose: PurposeUpload, Bytes: 1}); !errors.Is(err, hosting.ErrBusy) {
		t.Fatalf("expected ErrBusy saturation, got %v", err)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatalf("first call should succeed: %v", err)
	}
}
