package pbvex

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/realtime"
	"github.com/pocketbase/pocketbase/core"
)

type sessionEvent struct {
	ID           string          `json:"id"`
	Op           string          `json:"op"`
	Payload      json.RawMessage `json:"payload"`
	MaxEventSize int64           `json:"maxEventSize"`
}

type sessionStream struct {
	id     string
	body   io.ReadCloser
	reader *bufio.Reader
	events chan sessionEvent
}

// openSession opens a multiplexed session and reads its announcement.
func openSession(t *testing.T, serverURL, token string) *sessionStream {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/pbvex/realtime/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("session request failed: %v", err)
	}
	t.Cleanup(func() { res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}

	s := &sessionStream{body: res.Body, reader: bufio.NewReader(res.Body), events: make(chan sessionEvent, 64)}
	go func() {
		defer close(s.events)
		for {
			line, err := s.reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var env struct {
				Data sessionEvent `json:"data"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &env) != nil {
				continue
			}
			s.events <- env.Data
		}
	}()

	first := s.next(t)
	if first.Op != "session" || len(first.ID) != 64 {
		t.Fatalf("expected session announcement first, got %+v", first)
	}
	s.id = first.ID
	return s
}

func (s *sessionStream) next(t *testing.T) sessionEvent {
	t.Helper()
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				t.Fatal("session stream closed")
			}
			if ev.Op == "ping" {
				continue
			}
			return ev
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for session event")
		}
	}
}

// waitClosed reports whether the stream ends within the timeout.
func (s *sessionStream) waitClosed(timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-s.events:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

type sessionSub struct {
	path string
	args string
}

func (s sessionSub) id(t *testing.T) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s.args), &v); err != nil {
		t.Fatal(err)
	}
	return realtime.DeriveSubscriptionID("v1", s.path, v)
}

func sessionControl(t *testing.T, serverURL, token, sessionID string, subscribe []sessionSub, unsubscribe []string) int {
	t.Helper()
	entries := make([]map[string]any, 0, len(subscribe))
	for _, sub := range subscribe {
		var args any
		if err := json.Unmarshal([]byte(sub.args), &args); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, map[string]any{"id": sub.id(t), "path": sub.path, "args": args})
	}
	body := map[string]any{"session": sessionID, "subscribe": entries}
	if unsubscribe != nil {
		body["unsubscribe"] = unsubscribe
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/pbvex/realtime/session/subscriptions", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("control request failed: %v", err)
	}
	res.Body.Close()
	return res.StatusCode
}

// collectMessages reads events until it has one message per id in want.
func collectMessages(t *testing.T, s *sessionStream, want ...string) map[string]string {
	t.Helper()
	got := map[string]string{}
	for len(got) < len(want) {
		ev := s.next(t)
		if ev.Op != "message" {
			continue
		}
		got[ev.ID] = string(ev.Payload)
	}
	for _, id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing message for %s; got %v", id, got)
		}
	}
	return got
}

const sessionBundle = `__pbvex.registerFunction({name:"hello",type:"query",visibility:"public",modulePath:"hello",exportName:"default"}, function(ctx,args) { return "Hello, " + args.name + "!"; });
__pbvex.registerFunction({name:"random",type:"query",visibility:"public",modulePath:"random",exportName:"default"}, function(ctx,args) { return Math.random(); });`

func startSessionServer(t *testing.T) (string, func(email string)) {
	t.Helper()
	app, service := newTestApp(t)
	fns := []any{functionDescriptor("hello", "query", "public"), functionDescriptor("random", "query", "public")}
	resp, err := service.Upload(uploadRequestWithFunctions("session", sessionBundle, fns, nil))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}
	server := startRealtimeServer(t, app, service)
	t.Cleanup(server.Close)

	createUser := func(email string) {
		col, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			t.Fatal(err)
		}
		record := core.NewRecord(col)
		record.Set("email", email)
		record.SetPassword("12345678")
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}
	}
	return server.URL, createUser
}

func TestRealtimeSessionMultiplexesSubscriptions(t *testing.T) {
	serverURL, createUser := startSessionServer(t)
	s := openSession(t, serverURL, "")

	// More distinct subscriptions than a browser's six HTTP/1.1 connections,
	// all on one stream.
	subs := []sessionSub{}
	ids := []string{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		sub := sessionSub{path: "hello", args: `{"name":"` + name + `"}`}
		subs = append(subs, sub)
		ids = append(ids, sub.id(t))
	}
	random := sessionSub{path: "random", args: `{}`}
	subs = append(subs, random)
	ids = append(ids, random.id(t))

	if status := sessionControl(t, serverURL, "", s.id, subs, nil); status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	got := collectMessages(t, s, ids...)
	if got[ids[0]] != `"Hello, a!"` || got[ids[7]] != `"Hello, h!"` {
		t.Fatalf("unexpected payloads: %v", got)
	}

	// An invalidation reruns the queries; only the changed result is resent.
	createUser("session-invalidate@example.com")
	ev := s.next(t)
	if ev.Op != "message" || ev.ID != random.id(t) {
		t.Fatalf("expected random rerun, got %+v", ev)
	}

	// After unsubscribing, invalidations no longer reach the removed query.
	if status := sessionControl(t, serverURL, "", s.id, nil, []string{random.id(t)}); status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
	again := sessionSub{path: "hello", args: `{"name":"again"}`}
	createUser("session-invalidate-2@example.com")
	if status := sessionControl(t, serverURL, "", s.id, []sessionSub{again}, nil); status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
	for {
		ev := s.next(t)
		if ev.ID == random.id(t) && ev.Op == "message" {
			t.Fatalf("unsubscribed query still received %s", ev.Payload)
		}
		if ev.ID == again.id(t) && ev.Op == "message" {
			break
		}
	}
}

func TestRealtimeSessionReportsInvalidSubscriptionOnStream(t *testing.T) {
	serverURL, _ := startSessionServer(t)
	s := openSession(t, serverURL, "")

	missing := sessionSub{path: "missing", args: `{}`}
	ok := sessionSub{path: "hello", args: `{"name":"ok"}`}
	if status := sessionControl(t, serverURL, "", s.id, []sessionSub{missing, ok}, nil); status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
	got := collectMessages(t, s, missing.id(t), ok.id(t))
	if !strings.Contains(got[missing.id(t)], `"code":"not_found"`) {
		t.Fatalf("expected not_found error payload, got %s", got[missing.id(t)])
	}
	if got[ok.id(t)] != `"Hello, ok!"` {
		t.Fatalf("valid subscription was affected: %s", got[ok.id(t)])
	}
}

func TestRealtimeSessionControlRejectsUnknownSession(t *testing.T) {
	serverURL, _ := startSessionServer(t)
	unknown := strings.Repeat("ab", 32)
	if status := sessionControl(t, serverURL, "", unknown, []sessionSub{{path: "hello", args: `{"name":"x"}`}}, nil); status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
	if status := sessionControl(t, serverURL, "", "not-a-session", nil, nil); status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestRealtimeSessionIsBoundToAuthIdentity(t *testing.T) {
	_, _, token, _, server := setupBearerAuthE2E(t)
	s := openSession(t, server.URL, token)
	sub := sessionSub{path: "whoRealtime", args: `{}`}

	// A request without the session's credentials cannot drive it.
	if status := sessionControl(t, server.URL, "", s.id, []sessionSub{sub}, nil); status != http.StatusForbidden {
		t.Fatalf("expected 403 for anonymous control, got %d", status)
	}

	if status := sessionControl(t, server.URL, token, s.id, []sessionSub{sub}, nil); status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
	got := collectMessages(t, s, sub.id(t))
	if !strings.Contains(got[sub.id(t)], `"tokenIdentifier":"`) || strings.Contains(got[sub.id(t)], `"tokenIdentifier":null`) {
		t.Fatalf("query did not run as the session identity: %s", got[sub.id(t)])
	}
}

func TestRealtimeSessionClosesOnActivation(t *testing.T) {
	app, service := newTestApp(t)
	resp1, err := service.Upload(uploadRequest("session1", testBundleJS, functionDescriptor("hello", "query", "public"), nil))
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := service.Upload(uploadRequest("session2", testBundleJS, functionDescriptor("hello", "query", "public"), nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(resp1.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	server := startRealtimeServer(t, app, service)
	defer server.Close()

	s := openSession(t, server.URL, "")
	sub := sessionSub{path: "hello", args: `{"name":"x"}`}
	if status := sessionControl(t, server.URL, "", s.id, []sessionSub{sub}, nil); status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
	collectMessages(t, s, sub.id(t))

	if _, err := service.Activate(resp2.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	if !s.waitClosed(5 * time.Second) {
		t.Fatal("session stream stayed open after activation")
	}
	// The session is gone, so the client must open a new one.
	if status := sessionControl(t, server.URL, "", s.id, []sessionSub{sub}, nil); status != http.StatusNotFound {
		t.Fatalf("expected 404 after activation, got %d", status)
	}
}
