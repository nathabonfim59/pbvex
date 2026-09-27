package storagequota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/hosting"
)

// newQuotaTestServer binds a reference service on a fresh Unix socket and
// returns a connected client plus the service for assertions. An optional
// final argument overrides the client's local MaxInFlight budget.
func newQuotaTestServer(t *testing.T, limit int, capacityBytes int64, maxInFlight ...int) (*Client, *ReferenceQuotaService) {
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

	inFlight := 8
	if len(maxInFlight) > 0 && maxInFlight[0] > 0 {
		inFlight = maxInFlight[0]
	}
	client, err := NewClient(hosting.Config{
		Enabled:     true,
		SocketPath:  socket,
		Timeout:     2 * time.Second,
		MaxInFlight: inFlight,
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

	// A settle above the reserved bound is rejected as a protocol
	// conflict: the reservation and its usage stay reserved for
	// reconciliation instead of being acknowledged at an undercount.
	if _, err := client.SettleStorage(ctx, SettleStorageRequest{ReservationID: d.ReservationID, Bytes: 500}); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected above-bound settle rejection, got %v", err)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("above-bound settlement must not charge, used=%d", got)
	}

	// The preserved reservation settles to its exact stored count.
	a, err := client.SettleStorage(ctx, SettleStorageRequest{ReservationID: d.ReservationID, Bytes: 400})
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

func TestSettleAboveBoundKeepsReservationAndCapacityForReconciliation(t *testing.T) {
	client, svc := newQuotaTestServer(t, 100, 500)
	ctx := context.Background()

	d, err := client.ReserveStorage(ctx, ReserveStorageRequest{
		RequestID: "rq-1", Purpose: PurposeUpload, Bytes: 200,
	})
	if err != nil || !d.Allowed {
		t.Fatalf("reserve: %v %+v", err, d)
	}
	// A settlement above the reserved bound is a protocol error: the
	// provider must never let the caller raise its own charge, and it must
	// not silently acknowledge an undercount either. The reservation (and
	// its held capacity) stays reserved for provider reconciliation.
	if _, err := client.SettleStorage(ctx, SettleStorageRequest{ReservationID: d.ReservationID, Bytes: 1 << 20}); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected above-bound settlement rejection, got %v", err)
	}
	if got := svc.UsedBytes(); got != 0 {
		t.Fatalf("above-bound settlement must not charge, used=%d", got)
	}
	// The rejected reservation still holds its inflight bound: a second
	// 200-byte reservation fits, a third does not.
	d2, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-2", Purpose: PurposeUpload, Bytes: 200})
	if err != nil || !d2.Allowed {
		t.Fatalf("expected 200 to fit the remaining capacity: %v %+v", err, d2)
	}
	d3, err := client.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-3", Purpose: PurposeUpload, Bytes: 200})
	if err != nil || d3.Allowed || d3.Code != CodeQuotaExhausted {
		t.Fatalf("expected capacity exhaustion, got %v %+v", err, d3)
	}
	// The preserved reservation still settles to its exact stored count.
	a, err := client.SettleStorage(ctx, SettleStorageRequest{ReservationID: d.ReservationID, Bytes: 200})
	if err != nil || a.ChargedBytes != 200 {
		t.Fatalf("settle after rejection: %v ack=%+v", err, a)
	}
	if got := svc.UsedBytes(); got != 200 {
		t.Fatalf("expected used 200 after exact settlement, got %d", got)
	}
}

func TestConcurrentReservesNeverExceedCapacity(t *testing.T) {
	const (
		capacity  = int64(1000)
		perTicket = int64(100)
		total     = 40
	)
	client, svc := newQuotaTestServer(t, 1000, capacity, total)
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

// newComposedQuotaServer serves one socket with the policy reference
// service and the storage quota routes behind a single handler — the same
// composition as backend/examples/policy-service. The returned counter
// tracks how many transport connections the server has accepted.
func newComposedQuotaServer(t *testing.T, maxInFlight int) (*hosting.Client, *hosting.ReferenceService, *ReferenceQuotaService, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "combined.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	policy := hosting.NewReferenceService(64)
	if err := policy.SetPolicy("p1", map[string]bool{hosting.FunctionExecute: true}); err != nil {
		t.Fatal(err)
	}
	quota := NewReferenceQuotaService(64, 1<<20)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/hello":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(hosting.Hello{
				Version:        hosting.Version,
				Implementation: "test-composed",
				Capabilities:   []string{hosting.FunctionExecute, CapabilityStorageReserve},
			})
		case strings.HasPrefix(r.URL.Path, "/v1/storage/"):
			quota.ServeHTTP(w, r)
		default:
			policy.ServeHTTP(w, r)
		}
	})
	var connections atomic.Int32
	server := &http.Server{Handler: handler, ConnState: func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close(); _ = ln.Close() })

	root, err := hosting.NewClient(hosting.Config{Enabled: true, SocketPath: socket, Timeout: 2 * time.Second, MaxInFlight: maxInFlight})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	return root, policy, quota, &connections
}

func TestBorrowedClientReusesRootConnections(t *testing.T) {
	root, _, _, connections := newComposedQuotaServer(t, 8)
	borrowed, err := NewClientFromRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Interleaved policy and storage calls share one persistent transport:
	// no connection duplication between the two protocols.
	for i := 0; i < 5; i++ {
		if _, err := root.Check(ctx, hosting.FunctionExecute); err != nil {
			t.Fatalf("policy check %d: %v", i, err)
		}
		d, err := borrowed.ReserveStorage(ctx, ReserveStorageRequest{
			RequestID: fmt.Sprintf("rq-%d", i), Purpose: PurposeUpload, Bytes: 1,
		})
		if err != nil || !d.Allowed {
			t.Fatalf("storage reserve %d: %v %+v", i, err, d)
		}
	}
	if n := connections.Load(); n != 1 {
		t.Fatalf("expected one shared persistent connection, got %d", n)
	}

	// A borrowed client must not close the shared root: both protocols
	// keep working after its Close, which is a deliberate no-op.
	borrowed.Close()
	if _, err := root.Check(ctx, hosting.FunctionExecute); err != nil {
		t.Fatalf("borrowed Close must not close the shared root: %v", err)
	}
	if _, err := borrowed.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-after-close", Purpose: PurposeUpload, Bytes: 1}); err != nil {
		t.Fatalf("borrowed client must keep working after Close: %v", err)
	}
	if n := connections.Load(); n != 1 {
		t.Fatalf("expected the shared connection to survive the borrowed Close, got %d connections", n)
	}
}

func TestBorrowedClientSharesRootInFlightBudget(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "shared.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	release := make(chan struct{})
	var connections atomic.Int32
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ConnState: func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/check":
			// Hold the only slot so the concurrent storage call must
			// observe the shared budget.
			<-release
			_, _ = w.Write([]byte(`{"allowed":true,"code":"allowed","policyVersion":"p1"}`))
		case "/v1/storage/reserve":
			_, _ = w.Write([]byte(`{"allowed":true,"code":"allowed","policyVersion":"p1","reservationId":"r1"}`))
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	})}
	go func() { _ = server.Serve(ln) }()
	var once sync.Once
	t.Cleanup(func() {
		_ = server.Close()
		once.Do(func() { close(release) })
	})

	root, err := hosting.NewClient(hosting.Config{Enabled: true, SocketPath: socket, Timeout: 2 * time.Second, MaxInFlight: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	borrowed, err := NewClientFromRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := root.Check(ctx, hosting.FunctionExecute)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	// The storage client saturates on the ROOT budget: it must return
	// ErrBusy instead of opening a second transport connection.
	if _, err := borrowed.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-1", Purpose: PurposeUpload, Bytes: 1}); !errors.Is(err, hosting.ErrBusy) {
		t.Fatalf("expected shared ErrBusy saturation, got %v", err)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatalf("policy call should succeed: %v", err)
	}
	// After the slot frees, the storage client uses the same connection.
	if _, err := borrowed.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq-2", Purpose: PurposeUpload, Bytes: 1}); err != nil {
		t.Fatalf("reserve after slot freed: %v", err)
	}
	if n := connections.Load(); n != 1 {
		t.Fatalf("expected a single shared transport connection, got %d", n)
	}
}

func TestBorrowedClientInvalidResponseFailsClosed(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "bad.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var connections atomic.Int32
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ConnState: func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/storage/reserve":
			// Unknown field: must map to ErrProtocol, never permission.
			_, _ = w.Write([]byte(`{"allowed":true,"code":"allowed","policyVersion":"p1","reservationId":"r1","extra":1}`))
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	})}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })

	root, err := hosting.NewClient(hosting.Config{Enabled: true, SocketPath: socket, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	borrowed, err := NewClientFromRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := borrowed.ReserveStorage(ctx, ReserveStorageRequest{RequestID: "rq", Purpose: PurposeUpload, Bytes: 1}); !errors.Is(err, hosting.ErrProtocol) {
		t.Fatalf("expected malformed response as ErrProtocol, got %v", err)
	}
}

func TestNewClientFromRootNil(t *testing.T) {
	if _, err := NewClientFromRoot(nil); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("expected nil root rejection, got %v", err)
	}
}
