package hosting

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.Handler) (*Client, *atomic.Int32) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var connections atomic.Int32
	s := &http.Server{Handler: h, ConnState: func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}}
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	c, err := NewClient(Config{Enabled: true, SocketPath: path, Timeout: 100 * time.Millisecond, MaxInFlight: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, &connections
}
func TestLifecycleAndDynamicPolicy(t *testing.T) {
	s := NewReferenceService(5)
	if err := s.SetPolicy("p1", map[string]bool{FunctionExecute: true}); err != nil {
		t.Fatal(err)
	}
	c, connections := testClient(t, s)
	ctx := context.Background()
	if _, err := c.Handshake(ctx); err != nil {
		t.Fatal(err)
	}
	r := AdmissionRequest{RequestID: NewID(), Capability: FunctionExecute, Operation: Operation{ID: NewID(), RootID: NewID(), SessionID: NewID(), Kind: "query", Origin: "public"}}
	d, err := c.Admit(ctx, r)
	if err != nil || !d.Allowed {
		t.Fatalf("admit: %+v %v", d, err)
	}
	again, err := c.Admit(ctx, r)
	if err != nil || again != d {
		t.Fatal("admission not idempotent", err)
	}
	changed := r
	changed.Operation.Kind = "mutation"
	if _, err := c.Admit(ctx, changed); err == nil {
		t.Fatal("conflicting request accepted")
	}
	e := Event{EventID: NewID(), Sequence: 1, Operation: r.Operation, ReservationID: d.ReservationID, PolicyVersion: d.PolicyVersion, Phase: "started", At: time.Now().UTC()}
	if err := c.Report(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := c.Report(ctx, e); err != nil {
		t.Fatal("duplicate", err)
	}
	e.Phase = "completed"
	e.Outcome = "success"
	if err := c.Report(ctx, e); err == nil {
		t.Fatal("event conflict accepted")
	}
	e.EventID = NewID()
	e.Sequence = 2
	if err := c.Report(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPolicy("p2", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Require(ctx, FunctionExecute); err == nil {
		t.Fatal("policy change not seen")
	}
	if connections.Load() != 1 {
		t.Fatalf("connections = %d; expected persistent transport", connections.Load())
	}
}
func TestFailClosed(t *testing.T) {
	for name, h := range map[string]http.Handler{
		"version": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"version":"2","implementation":"test","capabilities":[]}`))
		}),
		"unknown": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"version":"1","implementation":"test","capabilities":[],"extra":true}`))
		}),
		"oversize": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(make([]byte, MaxPayload+1)) }),
		"timeout":  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }),
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := testClient(t, h)
			if _, err := c.Handshake(context.Background()); err == nil {
				t.Fatal("accepted invalid/unavailable provider")
			}
		})
	}
	c, err := NewClient(Config{Enabled: true, SocketPath: filepath.Join(t.TempDir(), "absent")})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Require(context.Background(), FunctionExecute); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
func TestStateAndCapacity(t *testing.T) {
	s := NewReferenceService(1)
	s.SetPolicy("p", map[string]bool{FunctionExecute: true})
	c, _ := testClient(t, s)
	ctx := context.Background()
	r := AdmissionRequest{RequestID: NewID(), Capability: FunctionExecute, Operation: Operation{ID: NewID(), RootID: NewID(), SessionID: NewID(), Kind: "query", Origin: "public"}}
	d, err := c.Admit(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	e := Event{EventID: NewID(), Sequence: 1, Operation: r.Operation, ReservationID: d.ReservationID, PolicyVersion: d.PolicyVersion, Phase: "completed", Outcome: "error", At: time.Now()}
	if c.Report(ctx, e) == nil {
		t.Fatal("completion before start accepted")
	}
	e.Phase = "released"
	e.Outcome = "canceled"
	if err := c.Report(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.EventID = NewID()
	e.Sequence = 2
	e.Phase = "started"
	e.Outcome = ""
	if c.Report(ctx, e) == nil {
		t.Fatal("start after release accepted")
	}
	r.RequestID = NewID()
	r.Operation.ID = NewID()
	if _, err := c.Admit(ctx, r); err == nil {
		t.Fatal("capacity not bounded")
	}
}
func TestConfiguration(t *testing.T) {
	for _, cfg := range []Config{{Enabled: true}, {Enabled: true, SocketPath: "relative"}, {Enabled: true, SocketPath: "/socket", Timeout: 31 * time.Second}, {Enabled: true, SocketPath: "/socket", MaxInFlight: -1}} {
		if cfg.Validate() == nil {
			t.Fatal("invalid config accepted", cfg)
		}
	}
	if (Config{}).Validate() != nil {
		t.Fatal("disabled config changed")
	}
}

func TestAdmissionIdempotencyAcrossPolicyChange(t *testing.T) {
	s := NewReferenceService(5)
	if err := s.SetPolicy("p1", map[string]bool{FunctionExecute: true}); err != nil {
		t.Fatal(err)
	}
	c, _ := testClient(t, s)
	ctx := context.Background()
	r := AdmissionRequest{RequestID: NewID(), Capability: FunctionExecute, Operation: Operation{ID: NewID(), RootID: NewID(), SessionID: NewID(), Kind: "query", Origin: "public"}}
	d, err := c.Admit(ctx, r)
	if err != nil || !d.Allowed {
		t.Fatalf("admit: %+v %v", d, err)
	}
	if err := s.SetPolicy("p2", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	again, err := c.Admit(ctx, r)
	if err != nil || again != d {
		t.Fatalf("retry after policy change must return the original decision: %+v %v", again, err)
	}
}

func TestMalformedOutcomes(t *testing.T) {
	valid := AdmissionRequest{RequestID: NewID(), Capability: FunctionExecute, Operation: Operation{ID: NewID(), RootID: NewID(), SessionID: NewID(), Kind: "query", Origin: "public"}}
	for name, tc := range map[string]struct {
		body string
		call func(c *Client) error
	}{
		"allowWithoutReservation": {
			body: `{"allowed":true,"code":"allowed","policyVersion":"p1"}`,
			call: func(c *Client) error { _, err := c.Admit(context.Background(), valid); return err },
		},
		"decisionWithoutPolicyVersion": {
			body: `{"allowed":false,"code":"denied"}`,
			call: func(c *Client) error { _, err := c.Check(context.Background(), FunctionExecute); return err },
		},
		"ackMismatch": {
			body: `{"eventId":"other"}`,
			call: func(c *Client) error {
				return c.Report(context.Background(), Event{EventID: NewID(), Sequence: 1, Operation: valid.Operation, ReservationID: NewID(), PolicyVersion: "p", Phase: "started", At: time.Now()})
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tc.body))
			}))
			if err := tc.call(c); !errors.Is(err, ErrProtocol) {
				t.Fatalf("err = %v, want ErrProtocol", err)
			}
		})
	}
}

func TestClientCapacityErrBusy(t *testing.T) {
	s := NewReferenceService(5)
	if err := s.SetPolicy("p", map[string]bool{FunctionExecute: true}); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	arrived := make(chan struct{}, 8)
	blocked := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/check" {
			arrived <- struct{}{}
			<-gate
		}
		s.ServeHTTP(w, r)
	})
	path := filepath.Join(t.TempDir(), "p.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: blocked}
	go server.Serve(l)
	t.Cleanup(func() { server.Close() })
	c, err := NewClient(Config{Enabled: true, SocketPath: path, Timeout: 2 * time.Second, MaxInFlight: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	ctx := context.Background()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- c.Require(ctx, FunctionExecute) }()
	}
	<-arrived
	<-arrived
	busy := make(chan error, 1)
	go func() { busy <- c.Require(ctx, FunctionExecute) }()
	select {
	case err := <-busy:
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("saturation err = %v, want ErrBusy", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("saturated call did not fail fast")
	}
	close(gate)
	for i := 0; i < 2; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("blocked calls did not finish")
		}
	}
	if err := c.Require(ctx, FunctionExecute); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilityNamespaces(t *testing.T) {
	s := NewReferenceService(5)
	c, _ := testClient(t, s)
	ctx := context.Background()
	if err := c.Require(ctx, EnvironmentRead+"/API_KEY"); err == nil {
		t.Fatal("environment capability allowed without explicit grant")
	}
	if err := s.SetPolicy("env", map[string]bool{EnvironmentRead + "/API_KEY": true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Require(ctx, EnvironmentRead+"/API_KEY"); err != nil {
		t.Fatal("explicit per-name grant denied", err)
	}
	if err := c.Require(ctx, EnvironmentRead+"/OTHER"); err == nil {
		t.Fatal("grant leaked to sibling name")
	}
	if !ValidToken(EnvironmentRead + "/DB_PASSWORD_1.x:y/z") {
		t.Fatal("valid namespaced capability rejected")
	}
	if ValidToken("") || ValidToken(strings.Repeat("x", 129)) {
		t.Fatal("invalid token accepted")
	}
}

func TestPersistentTransportAcrossRejections(t *testing.T) {
	// Rejection bodies like the ones the reference service sends must be fully
	// consumed so the transport keeps the single policy socket connection.
	c, connections := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"code":"rejected"}`))
	}))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := c.Require(ctx, FunctionExecute); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("rejection %d err = %v, want ErrUnavailable", i, err)
		}
	}
	if connections.Load() != 1 {
		t.Fatalf("connections = %d; expected persistent transport across rejections", connections.Load())
	}
}

func TestCallEndpointPathConstraints(t *testing.T) {
	// The exported Call seam must only reach this client's own /v1 socket
	// tree. A path that could name any other destination (traversal,
	// authority, query, fragment, empty or oversized) is a protocol error
	// rejected before any bytes are dialed.
	rejecting, connections := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request for path %q", r.URL.Path)
	}))
	ctx := context.Background()
	unsafe := []string{
		"",
		"/hello",
		"hello/",
		"/hello/",
		"//example.com/hello",
		"..",
		"../hello",
		"hello/../check",
		"hello/..",
		"hello?x=1",
		"hello#frag",
		"hello bar",
		"hello%2Fbar",
		"hello\tbar",
		strings.Repeat("a", 129),
		strings.Repeat("ab/", 400),
	}
	for _, path := range unsafe {
		if err := rejecting.Call(ctx, path, struct{}{}, new(Hello)); !errors.Is(err, ErrProtocol) {
			t.Fatalf("Call(%q) err = %v, want ErrProtocol", path, err)
		}
	}
	if connections.Load() != 0 {
		t.Fatalf("unsafe paths dialed %d connections; want 0", connections.Load())
	}

	// Positive control: a safe relative endpoint reaches the policy socket
	// under the /v1 root and decodes with the shared strictness.
	c, _ := testClient(t, NewReferenceService(5))
	hello, err := c.Handshake(ctx)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if hello.Version != Version {
		t.Fatalf("unexpected hello version %q", hello.Version)
	}
	if err := c.Call(ctx, "hello", struct{}{}, new(Hello)); err != nil {
		t.Fatalf("Call(hello) err = %v", err)
	}
	var decision Decision
	if err := c.Call(ctx, "check", CheckRequest{Capability: "settings.storage.write"}, &decision); err != nil {
		t.Fatalf("Call(check) err = %v", err)
	}
	if decision.Allowed {
		t.Fatal("unexpected allow from the default reference policy")
	}
}
