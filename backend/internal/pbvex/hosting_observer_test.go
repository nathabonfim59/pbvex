package pbvex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
)

// recordingService wraps the reference policy service and records the exact
// admission requests and events the adapter sends.
type recordingService struct {
	inner    http.Handler
	mu       sync.Mutex
	admitLog []hosting.AdmissionRequest
	eventLog []hosting.Event
}

func (r *recordingService) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/v1/admit" || req.URL.Path == "/v1/events" {
		body, _ := io.ReadAll(req.Body)
		_ = req.Body.Close()
		r.mu.Lock()
		switch req.URL.Path {
		case "/v1/admit":
			var a hosting.AdmissionRequest
			if json.Unmarshal(body, &a) == nil {
				r.admitLog = append(r.admitLog, a)
			}
		case "/v1/events":
			var e hosting.Event
			if json.Unmarshal(body, &e) == nil {
				r.eventLog = append(r.eventLog, e)
			}
		}
		r.mu.Unlock()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	r.inner.ServeHTTP(w, req)
}

func (r *recordingService) admits() []hosting.AdmissionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hosting.AdmissionRequest(nil), r.admitLog...)
}

func (r *recordingService) events() []hosting.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hosting.Event(nil), r.eventLog...)
}

// failingEvents fails /v1/events a fixed number of times. With hang set, the
// request still reaches the service after the client deadline, producing a
// genuinely uncertain acknowledgement.
type failingEvents struct {
	inner http.Handler
	fails atomic.Int64
	hang  time.Duration
}

func (f *failingEvents) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/v1/events" && f.fails.Add(-1) >= 0 {
		if f.hang > 0 {
			time.Sleep(f.hang)
			f.inner.ServeHTTP(w, req)
			return
		}
		panic(http.ErrAbortHandler)
	}
	f.inner.ServeHTTP(w, req)
}

func observerTestObserver(t *testing.T, wrapper func(http.Handler) http.Handler, cfg hosting.Config) (*hostingExecutionObserver, *hosting.ReferenceService, *recordingService) {
	t.Helper()
	service := hosting.NewReferenceService(100)
	recording := &recordingService{inner: service}
	var handler http.Handler = recording
	if wrapper != nil {
		handler = wrapper(handler)
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = filepath.Join(t.TempDir(), "p.sock")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 250 * time.Millisecond
	}
	l, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(l)
	t.Cleanup(func() { server.Close() })
	cfg.Enabled = true
	client, err := newHostingClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	if err := service.SetPolicy("p1", map[string]bool{hosting.FunctionExecute: true}); err != nil {
		t.Fatal(err)
	}
	return newHostingExecutionObserver(nil, client, nil), service, recording
}

func observerInfo(id string) runtime.ExecutionInfo {
	return runtime.ExecutionInfo{
		ID: id, RootID: id, DeploymentID: "dep-1",
		FunctionName: "messages/list", FunctionType: deploy.FunctionTypeQuery,
		Namespace: "root", Origin: "call", StartedAt: time.Now(),
	}
}

func waitForEvents(t *testing.T, recording *recordingService, count int) []hosting.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		events := recording.events()
		if len(events) >= count {
			return events
		}
		if time.Now().After(deadline) {
			t.Fatalf("events = %d, want %d", len(events), count)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestObserverAdmitsStartsAndCompletes(t *testing.T) {
	observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
	info := observerInfo("inv-1")
	if _, err := observer.Begin(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	admits := recording.admits()
	if len(admits) != 1 {
		t.Fatalf("admits = %d", len(admits))
	}
	want := hosting.Operation{ID: "inv-1", RootID: "inv-1", SessionID: admits[0].Operation.SessionID, Kind: "query", Origin: "call", DeploymentID: "dep-1", FunctionName: "messages/list", Namespace: "root"}
	if admits[0].Operation != want || admits[0].Capability != hosting.FunctionExecute {
		t.Fatalf("admission = %+v", admits[0])
	}
	events := recording.events()
	if len(events) != 1 || events[0].Phase != "started" || events[0].Sequence != 1 || events[0].DurationMicros != 0 || events[0].Outcome != "" || events[0].At.IsZero() {
		t.Fatalf("started events = %+v", events)
	}
	observer.End(context.Background(), info, runtime.ExecutionResult{Duration: 1500 * time.Microsecond})
	events = recording.events()
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	last := events[1]
	if last.Phase != "completed" || last.Outcome != "success" || last.Sequence != 2 || last.DurationMicros != 1500 || last.ReservationID != events[0].ReservationID || last.PolicyVersion != events[0].PolicyVersion {
		t.Fatalf("completion = %+v", last)
	}
	// A duplicate End is ignored without duplicating protocol state.
	observer.End(context.Background(), info, runtime.ExecutionResult{Duration: time.Second})
	if len(recording.events()) != 2 {
		t.Fatal("duplicate completion recorded")
	}
}

func TestObserverDenialPreventsExecution(t *testing.T) {
	observer, service, recording := observerTestObserver(t, nil, hosting.Config{})
	if err := service.SetPolicy("deny", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	_, err := observer.Begin(context.Background(), observerInfo("inv-1"))
	var denied *hosting.DeniedError
	if !errors.As(err, &denied) || denied.Code != "denied" {
		t.Fatalf("err = %v", err)
	}
	if events := recording.events(); len(events) != 0 {
		t.Fatalf("events for denied execution: %+v", events)
	}
	if admits := recording.admits(); len(admits) != 1 {
		t.Fatal("denial not persisted as an admission record")
	}
}

func TestObserverStartedFailureDeniesAndLatches(t *testing.T) {
	flaky := &failingEvents{hang: 400 * time.Millisecond}
	flaky.fails.Store(2)
	observer, _, recording := observerTestObserver(t, func(h http.Handler) http.Handler { flaky.inner = h; return flaky }, hosting.Config{Timeout: 150 * time.Millisecond})
	if _, err := observer.Begin(context.Background(), observerInfo("inv-1")); !errors.Is(err, hosting.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	// The hung request may still have reached the service: the started
	// acknowledgement is uncertain, so the reservation is never released.
	events := waitForEvents(t, recording, 1)
	for _, e := range events {
		if e.Phase == "released" {
			t.Fatal("released emitted for an uncertain start")
		}
	}
	// Reporting failure latches the observer: new starts are rejected before
	// any further admission attempt.
	if _, err := observer.Begin(context.Background(), observerInfo("inv-2")); !errors.Is(err, errHostingMeteringUnhealthy) {
		t.Fatalf("latch err = %v", err)
	}
	if admits := recording.admits(); len(admits) != 1 {
		t.Fatalf("latched observer attempted admission: %d", len(admits))
	}
}

func TestObserverCompletionAfterCallerCancellation(t *testing.T) {
	observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
	info := observerInfo("inv-1")
	if _, err := observer.Begin(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observer.End(ctx, info, runtime.ExecutionResult{Duration: 2 * time.Millisecond, Err: context.Canceled})
	events := recording.events()
	if len(events) != 2 || events[1].Phase != "completed" || events[1].Outcome != "canceled" {
		t.Fatalf("events = %+v", events)
	}
}

func TestObserverOutcomeMappingAndClamp(t *testing.T) {
	cases := []struct {
		name    string
		result  runtime.ExecutionResult
		outcome string
		micros  int64
	}{
		{"success", runtime.ExecutionResult{Duration: time.Millisecond}, "success", 1000},
		{"timeout", runtime.ExecutionResult{Err: context.DeadlineExceeded}, "timeout", 0},
		{"wrappedCancel", runtime.ExecutionResult{Err: fmt.Errorf("wrap: %w", context.Canceled)}, "canceled", 0},
		{"error", runtime.ExecutionResult{Err: errors.New("application data must not leak")}, "error", 0},
		{"negativeDurationClamped", runtime.ExecutionResult{Duration: -time.Second}, "success", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
			info := observerInfo("inv-1")
			if _, err := observer.Begin(context.Background(), info); err != nil {
				t.Fatal(err)
			}
			observer.End(context.Background(), info, tc.result)
			events := recording.events()
			if len(events) != 2 || events[1].Outcome != tc.outcome || events[1].DurationMicros != tc.micros {
				t.Fatalf("events = %+v", events)
			}
		})
	}
}

func TestObserverNestedCorrelation(t *testing.T) {
	observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
	root := observerInfo("root-1")
	rootCtx, err := observer.Begin(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	child := observerInfo("child-1")
	child.RootID, child.ParentID = "root-1", "root-1"
	childCtx, err := observer.Begin(rootCtx, child)
	if err != nil {
		t.Fatal(err)
	}
	observer.End(childCtx, child, runtime.ExecutionResult{Err: errors.New("child failed")})
	observer.End(rootCtx, root, runtime.ExecutionResult{Duration: time.Millisecond})
	admits := recording.admits()
	if len(admits) != 2 {
		t.Fatalf("admits = %d", len(admits))
	}
	if admits[1].Operation.RootID != "root-1" || admits[1].Operation.ParentID != "root-1" {
		t.Fatalf("child operation = %+v", admits[1].Operation)
	}
	if admits[1].Operation.SessionID != admits[0].Operation.SessionID {
		t.Fatal("session split across nesting")
	}
	events := recording.events()
	if len(events) != 4 {
		t.Fatalf("events = %d", len(events))
	}
	for i, e := range events {
		if e.Sequence != uint64(i+1) {
			t.Fatalf("sequence %d at position %d", e.Sequence, i)
		}
		if e.Operation.SessionID != admits[0].Operation.SessionID {
			t.Fatal("session mismatch in events")
		}
	}
	if events[2].Outcome != "error" || events[3].Outcome != "success" {
		t.Fatalf("outcomes = %+v", events)
	}
}

type fakeExternalObserver struct {
	beginErr   error
	nilContext bool
	background bool
	mu         sync.Mutex
	begins     []runtime.ExecutionInfo
	ends       []runtime.ExecutionInfo
}

type externalObserverKey struct{}

func (f *fakeExternalObserver) Begin(ctx context.Context, info runtime.ExecutionInfo) (context.Context, error) {
	f.mu.Lock()
	f.begins = append(f.begins, info)
	f.mu.Unlock()
	if f.beginErr != nil {
		return ctx, f.beginErr
	}
	if f.nilContext {
		return nil, nil
	}
	if f.background {
		// An unrelated context: the adapter must not let it erase the
		// caller's cancellation or deadline.
		return context.Background(), nil
	}
	return context.WithValue(ctx, externalObserverKey{}, true), nil
}

func (f *fakeExternalObserver) End(ctx context.Context, info runtime.ExecutionInfo, result runtime.ExecutionResult) {
	f.mu.Lock()
	f.ends = append(f.ends, info)
	f.mu.Unlock()
}

func TestObserverExternalComposition(t *testing.T) {
	t.Run("externalDenialPreventsAdmission", func(t *testing.T) {
		external := &fakeExternalObserver{beginErr: errors.New("external denial")}
		observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
		observer.external = external
		_, err := observer.Begin(context.Background(), observerInfo("inv-1"))
		if !errors.Is(err, external.beginErr) {
			t.Fatalf("err = %v", err)
		}
		if len(recording.admits()) != 0 || len(external.ends) != 0 {
			t.Fatal("reserved capacity for externally denied work")
		}
	})
	t.Run("successPairsExternalEnd", func(t *testing.T) {
		external := &fakeExternalObserver{}
		observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
		observer.external = external
		info := observerInfo("inv-1")
		observed, err := observer.Begin(context.Background(), info)
		if err != nil {
			t.Fatal(err)
		}
		observer.End(observed, info, runtime.ExecutionResult{Duration: time.Millisecond})
		if len(external.begins) != 1 || len(external.ends) != 1 {
			t.Fatalf("external begins/ends = %d/%d", len(external.begins), len(external.ends))
		}
		if len(recording.events()) != 2 {
			t.Fatal("hosting lifecycle events missing")
		}
	})
	t.Run("admissionFailurePairsExternalEnd", func(t *testing.T) {
		external := &fakeExternalObserver{}
		observer, service, recording := observerTestObserver(t, nil, hosting.Config{})
		observer.external = external
		if err := service.SetPolicy("deny", map[string]bool{}); err != nil {
			t.Fatal(err)
		}
		_, err := observer.Begin(context.Background(), observerInfo("inv-1"))
		var denied *hosting.DeniedError
		if !errors.As(err, &denied) {
			t.Fatalf("err = %v", err)
		}
		if len(external.ends) != 1 {
			t.Fatalf("external ends = %d", len(external.ends))
		}
		if len(recording.events()) != 0 {
			t.Fatal("events recorded for denied execution")
		}
	})
}

func TestObserverIdentityProjection(t *testing.T) {
	t.Run("invalidIdentityDenies", func(t *testing.T) {
		for _, info := range []runtime.ExecutionInfo{
			{ID: "", RootID: "r", Origin: "call", FunctionType: deploy.FunctionTypeQuery},
			{ID: "inv-1", RootID: "inv-1", Origin: "", FunctionType: deploy.FunctionTypeQuery},
			{ID: "inv-1", RootID: "inv-1", Origin: "call"},
			{ID: "inv-1", RootID: "inv-1", Origin: "call", FunctionType: deploy.FunctionType("bad type")},
		} {
			observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
			if _, err := observer.Begin(context.Background(), info); !errors.Is(err, errInvalidExecutionIdentity) {
				t.Fatalf("err = %v", err)
			}
			if len(recording.admits()) != 0 {
				t.Fatal("malformed identity admitted")
			}
		}
	})
	t.Run("unsafeIdentifiersAreProjected", func(t *testing.T) {
		observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
		if _, err := observer.Begin(context.Background(), observerInfo("inv id with spaces")); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte("inv id with spaces"))
		want := hex.EncodeToString(sum[:])
		admits := recording.admits()
		if len(admits) != 1 || admits[0].Operation.ID != want || !hosting.ValidToken(want) {
			t.Fatalf("operations = %+v", admits)
		}
	})
	t.Run("unsafeOptionalLabelsAreOmitted", func(t *testing.T) {
		observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
		info := observerInfo("inv-1")
		info.FunctionName = "bad name"
		info.Namespace = "bad namespace"
		if _, err := observer.Begin(context.Background(), info); err != nil {
			t.Fatal(err)
		}
		got := recording.admits()[0].Operation
		if got.FunctionName != "" || got.Namespace != "" {
			t.Fatalf("optional labels = %q %q", got.FunctionName, got.Namespace)
		}
	})
	t.Run("bundleLoadsAndMigrationsMapToLabels", func(t *testing.T) {
		observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
		bundle := runtime.ExecutionInfo{ID: "b1", RootID: "b1", DeploymentID: "dep-1", FunctionName: "bundle", Origin: "bundle_load"}
		if _, err := observer.Begin(context.Background(), bundle); err != nil {
			t.Fatal(err)
		}
		migration := runtime.ExecutionInfo{ID: "m1", RootID: "m1", DeploymentID: "dep-1", FunctionName: "add_index:up", Origin: "migration"}
		if _, err := observer.Begin(context.Background(), migration); err != nil {
			t.Fatal(err)
		}
		admits := recording.admits()
		if len(admits) != 2 {
			t.Fatalf("admits = %d", len(admits))
		}
		if admits[0].Operation.Kind != "bundle.load" || admits[1].Operation.Kind != "migration.application" {
			t.Fatalf("kinds = %q %q", admits[0].Operation.Kind, admits[1].Operation.Kind)
		}
	})
}

func TestObserverTrackedBoundAndFallbackSettlement(t *testing.T) {
	observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
	observer.maxTracked = 1
	first := observerInfo("inv-1")
	if _, err := observer.Begin(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Begin(context.Background(), observerInfo("inv-2")); !errors.Is(err, errTrackedExecutionsExhausted) {
		t.Fatalf("bound err = %v", err)
	}
	// Ending with a bare context settles through the bounded fallback table.
	observer.End(context.Background(), first, runtime.ExecutionResult{Duration: time.Millisecond})
	events := recording.events()
	if len(events) != 2 || events[1].Phase != "completed" {
		t.Fatalf("events = %+v", events)
	}
	if _, err := observer.Begin(context.Background(), observerInfo("inv-3")); err != nil {
		t.Fatalf("tracking bound not released: %v", err)
	}
}

func TestObserverSequenceUniqueUnderConcurrency(t *testing.T) {
	observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			info := observerInfo(fmt.Sprintf("inv-%d", i))
			ctx, err := observer.Begin(context.Background(), info)
			if err != nil {
				t.Error(err)
				return
			}
			observer.End(ctx, info, runtime.ExecutionResult{Duration: time.Millisecond})
		}(i)
	}
	wg.Wait()
	events := recording.events()
	if len(events) != 2*n {
		t.Fatalf("events = %d", len(events))
	}
	seen := map[uint64]bool{}
	for _, e := range events {
		if e.Sequence == 0 || seen[e.Sequence] {
			t.Fatalf("sequence %d reused", e.Sequence)
		}
		seen[e.Sequence] = true
	}
}

func TestObserverBusyAdmissionDoesNotLatch(t *testing.T) {
	gate := make(chan struct{})
	arrived := make(chan struct{}, 8)
	gated := &gatedAdmitHandler{gate: gate, arrived: arrived}
	observer, _, _ := observerTestObserver(t, func(h http.Handler) http.Handler { gated.inner = h; return gated }, hosting.Config{MaxInFlight: 1, Timeout: 2 * time.Second})
	done := make(chan error, 1)
	go func() {
		_, err := observer.Begin(context.Background(), observerInfo("inv-1"))
		done <- err
	}()
	<-arrived
	busy := make(chan error, 1)
	go func() {
		_, err := observer.Begin(context.Background(), observerInfo("inv-2"))
		busy <- err
	}()
	select {
	case err := <-busy:
		if !errors.Is(err, deploy.ErrExecutionBusy) || !errors.Is(err, hosting.ErrBusy) {
			t.Fatalf("busy err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("saturated admission did not fail fast")
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatalf("first begin: %v", err)
	}
	if _, err := observer.Begin(context.Background(), observerInfo("inv-3")); err != nil {
		t.Fatalf("busy admission latched the observer: %v", err)
	}
}

type gatedAdmitHandler struct {
	inner   http.Handler
	gate    chan struct{}
	arrived chan struct{}
}

func (h *gatedAdmitHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/v1/admit" {
		h.arrived <- struct{}{}
		<-h.gate
	}
	h.inner.ServeHTTP(w, req)
}

func TestObserverNilExternalContextDenied(t *testing.T) {
	external := &fakeExternalObserver{nilContext: true}
	observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
	observer.external = external
	_, err := observer.Begin(context.Background(), observerInfo("inv-1"))
	if !errors.Is(err, errNilObserverContext) {
		t.Fatalf("err = %v", err)
	}
	if len(external.ends) != 1 {
		t.Fatalf("external ends = %d, want exactly one paired End", len(external.ends))
	}
	if admits := recording.admits(); len(admits) != 0 {
		t.Fatal("admission attempted after nil external context")
	}
}

func TestObserverExternalBackgroundCannotEraseCallerState(t *testing.T) {
	external := &fakeExternalObserver{background: true}
	observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
	observer.external = external

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := observer.Begin(canceled, observerInfo("inv-1")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	if _, err := observer.Begin(deadlineCtx, observerInfo("inv-1")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired deadline err = %v", err)
	}
	if admits := recording.admits(); len(admits) != 0 {
		t.Fatal("canceled caller reached admission")
	}
	if len(external.begins) != 0 {
		t.Fatal("canceled caller reached the external observer")
	}
}

func TestObserverAdmissionStaysBoundToCallerContext(t *testing.T) {
	gate := make(chan struct{})
	arrived := make(chan struct{}, 8)
	gated := &gatedAdmitHandler{gate: gate, arrived: arrived}
	external := &fakeExternalObserver{background: true}
	observer, _, _ := observerTestObserver(t, func(h http.Handler) http.Handler { gated.inner = h; return gated }, hosting.Config{Timeout: 2 * time.Second})
	observer.external = external
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := observer.Begin(ctx, observerInfo("inv-1"))
		done <- err
	}()
	<-arrived
	// The external observer returned an unrelated context; the in-flight
	// admission must still abort when the caller cancels.
	cancel()
	close(gate)
	select {
	case err := <-done:
		if !errors.Is(err, hosting.ErrUnavailable) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("admission ignored caller cancellation")
	}
	if !observer.unhealthy.Load() {
		t.Fatal("uncertain in-flight cancellation was not treated conservatively")
	}
}

func TestObserverUncertainAdmissionThenBusyLatches(t *testing.T) {
	handler := &uncertainThenBusyHandler{
		admitArrivals: make(chan struct{}, 4),
		blockHolding:  make(chan struct{}, 1),
		blockRelease:  make(chan struct{}),
	}
	observer, _, _ := observerTestObserver(t, func(h http.Handler) http.Handler { handler.inner = h; return handler }, hosting.Config{MaxInFlight: 1, Timeout: 2 * time.Second})
	// Widen the retry backoff so the slot-holding blocker can deterministically
	// occupy the client between the failed first attempt and the retry.
	observer.retryBackoff = 250 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		_, err := observer.Begin(context.Background(), observerInfo("inv-1"))
		done <- err
	}()
	<-handler.admitArrivals // first attempt returned 503: the exchange is uncertain
	// Occupy the single client slot before the retried admission runs so the
	// retry observes local saturation on top of prior uncertainty.
	time.Sleep(25 * time.Millisecond)
	checkDone := make(chan error, 1)
	go func() {
		_, err := observer.client.Check(context.Background(), hosting.FunctionExecute)
		checkDone <- err
	}()
	select {
	case <-handler.blockHolding:
	case <-time.After(2 * time.Second):
		t.Fatal("blocker never reached the service")
	}
	select {
	case err := <-done:
		if !errors.Is(err, hosting.ErrBusy) || errors.Is(err, deploy.ErrExecutionBusy) {
			t.Fatalf("err = %v, want uncertain exhaustion without the clean-busy mapping", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("uncertain admission did not finish")
	}
	if !observer.unhealthy.Load() {
		t.Fatal("uncertain exchange followed by saturation did not latch")
	}
	close(handler.blockRelease)
	<-checkDone
	if _, err := observer.Begin(context.Background(), observerInfo("inv-2")); !errors.Is(err, errHostingMeteringUnhealthy) {
		t.Fatalf("latch err = %v", err)
	}
}

type uncertainThenBusyHandler struct {
	inner         http.Handler
	admitArrivals chan struct{}
	blockHolding  chan struct{}
	blockRelease  chan struct{}
	admits        atomic.Int64
}

func (h *uncertainThenBusyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/v1/admit":
		if h.admits.Add(1) == 1 {
			h.admitArrivals <- struct{}{}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	case "/v1/check":
		h.blockHolding <- struct{}{}
		<-h.blockRelease
	}
	h.inner.ServeHTTP(w, req)
}

func TestObserverEndIgnoresForeignReservationContext(t *testing.T) {
	observer, _, recording := observerTestObserver(t, nil, hosting.Config{})
	a, b := observerInfo("inv-a"), observerInfo("inv-b")
	if _, err := observer.Begin(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	ctxB, err := observer.Begin(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	// Ending A under B's context must settle A by identifier, never B.
	observer.End(ctxB, a, runtime.ExecutionResult{Duration: time.Millisecond})
	events := recording.events()
	if len(events) != 3 || events[2].Phase != "completed" || events[2].Operation.ID != "inv-a" {
		t.Fatalf("events = %+v", events)
	}
	observer.End(ctxB, b, runtime.ExecutionResult{Duration: time.Millisecond})
	events = recording.events()
	if len(events) != 4 || events[3].Operation.ID != "inv-b" {
		t.Fatalf("events = %+v", events)
	}
}

func TestHostedEnvironmentResolver(t *testing.T) {
	t.Setenv("PBVEX_TEST_HOST_SECRET", "host-value")
	observer, service, _ := observerTestObserver(t, nil, hosting.Config{})

	resolver := hostedEnvironmentResolver(observer.client, nil, nil)
	if _, _, err := resolver(context.Background(), "PBVEX_TEST_HOST_SECRET"); err == nil {
		t.Fatal("environment read allowed without explicit grant")
	}
	if _, _, err := resolver(context.Background(), ""); err == nil {
		t.Fatal("empty name accepted")
	}
	if _, _, err := resolver(context.Background(), "bad name"); err == nil {
		t.Fatal("invalid capability name accepted without socket round-trip")
	}
	if err := service.SetPolicy("env", map[string]bool{hosting.EnvironmentRead + "/PBVEX_TEST_HOST_SECRET": true}); err != nil {
		t.Fatal(err)
	}
	value, ok, err := resolver(context.Background(), "PBVEX_TEST_HOST_SECRET")
	if err != nil || !ok || value != "host-value" {
		t.Fatalf("resolved %q %v %v", value, ok, err)
	}
	if _, ok, err := resolver(context.Background(), "PBVEX_TEST_UNGRANTED"); err == nil || ok {
		t.Fatalf("ungranted name resolved %v %v", ok, err)
	}
	custom := func(ctx context.Context, name string) (string, bool, error) { return "custom", true, nil }
	composed := hostedEnvironmentResolver(observer.client, nil, custom)
	value, ok, err = composed(context.Background(), "PBVEX_TEST_HOST_SECRET")
	if err != nil || !ok || value != "custom" {
		t.Fatalf("custom resolver not preserved: %q %v %v", value, ok, err)
	}
	_, _, err = composed(context.Background(), "PBVEX_TEST_UNGRANTED")
	var denied *hosting.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("custom resolver bypassed permission: %v", err)
	}
}
