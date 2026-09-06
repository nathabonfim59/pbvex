package hosting

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
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
