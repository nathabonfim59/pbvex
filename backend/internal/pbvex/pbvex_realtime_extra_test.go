package pbvex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/realtime"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func uploadRequest(deploymentID, bundle string, fn map[string]any, config map[string]any) map[string]any {
	return uploadRequestWithFunctions(deploymentID, bundle, []any{fn}, config)
}

func uploadRequestWithFunctions(deploymentID, bundle string, fns []any, config map[string]any) map[string]any {
	manifest := map[string]any{
		"protocolVersion": "v1",
		"deploymentId":    deploymentID,
		"functions":       fns,
	}
	if config != nil {
		manifest["config"] = config
	}
	return map[string]any{
		"manifest": manifest,
		"bundle":   testBundle(bundle),
		"sha256":   bundleHash(bundle),
		"size":     int64(len(bundle)),
	}
}

func functionDescriptor(name, functionType, visibility string) map[string]any {
	return map[string]any{
		"name":       name,
		"type":       functionType,
		"visibility": visibility,
		"modulePath": name,
		"exportName": "default",
	}
}

func realtimePost(t *testing.T, serverURL, path, args string) *http.Request {
	return realtimePostID(t, serverURL, path, args, "")
}

func realtimePostID(t *testing.T, serverURL, path, args, idOverride string) *http.Request {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		t.Fatalf("invalid args JSON: %v", err)
	}
	id := idOverride
	if id == "" {
		id = realtime.DeriveSubscriptionID("v1", path, v)
	}
	body, err := json.Marshal(map[string]any{"id": id, "path": path, "args": v})
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}
	req, err := http.NewRequest("POST", serverURL+"/api/pbvex/realtime", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	return req
}

func startRealtimeServer(t *testing.T, app *tests.TestApp, service *deploy.Service) *httptest.Server {
	t.Helper()
	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	if err := app.OnServe().Trigger(serveEvent); err != nil {
		t.Fatalf("serve trigger failed: %v", err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatalf("failed to build mux: %v", err)
	}
	return httptest.NewServer(mux)
}

func sseReadLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read SSE line: %v", err)
	}
	return strings.TrimSpace(line)
}

func expectSSEMessage(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	for {
		line := sseReadLine(t, r)
		if line == "" {
			continue
		}
		return line
	}
}

func TestRealtimeEndpointAcceptHeaderParsedAsMediaType(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{"name":"world"}`

	badAccepts := []string{
		"text/plain",
		"text/event-stream;q=0",
		"*/*",
		"text/html, application/json",
	}

	for _, accept := range badAccepts {
		req := realtimePost(t, server.URL, "hello", args)
		req.Header.Set("Accept", accept)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotAcceptable {
			t.Fatalf("expected 406 for %q, got %d", accept, res.StatusCode)
		}
	}

	goodAccepts := []string{
		"text/event-stream",
		"text/event-stream;q=0.9",
		"text/html, text/event-stream;q=0.8",
	}
	for _, accept := range goodAccepts {
		req := realtimePost(t, server.URL, "hello", args)
		req.Header.Set("Accept", accept)
		client := &http.Client{Timeout: 2 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for %q, got %d", accept, res.StatusCode)
		}
	}
}

func TestRealtimeEndpointRejectsWhitespaceAndInvalidPaths(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{"name":"world"}`
	cases := []struct {
		id, path string
	}{
		{"hello :" + args, "hello "},
		{" hello:" + args, "hello"},
		{"hello:" + args + " ", "hello"},
		{"hello:" + args, "a-b"},
		{"hello:" + args, "0abc"},
		{"hello:" + args, "a/b"},
	}

	for _, tc := range cases {
		req := realtimePostID(t, server.URL, tc.path, args, tc.id)
		req.Header.Set("Accept", "text/event-stream")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for id=%q path=%q, got %d", tc.id, tc.path, res.StatusCode)
		}
	}
}

func TestRealtimeEndpointRejectsMismatchedID(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{"name":"world"}`
	req := realtimePostID(t, server.URL, "hello", args, strings.Repeat("0", 64))
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestRealtimeEndpointRejectsNonQueryOrInternalFunctions(t *testing.T) {
	cases := []struct {
		name         string
		functionType string
		visibility   string
	}{
		{"public_mutation", "mutation", "public"},
		{"public_action", "action", "public"},
		{"internal_query", "query", "internal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, service := newTestApp(t)
			bundle := fmt.Sprintf(`__pbvex.registerFunction({name:"%s",type:"%s",visibility:"%s",modulePath:"%s",exportName:"default"}, function(ctx,args) { return "ok"; });`, tc.name, tc.functionType, tc.visibility, tc.name)
			req := uploadRequest("realtime", bundle, functionDescriptor(tc.name, tc.functionType, tc.visibility), nil)
			resp, err := service.Upload(req)
			if err != nil {
				t.Fatalf("upload failed: %v", err)
			}
			if _, err := service.Activate(resp.DeploymentID, true); err != nil {
				t.Fatalf("activate failed: %v", err)
			}

			server := startRealtimeServer(t, app, service)
			defer server.Close()

			args := `{}`
			req2 := realtimePost(t, server.URL, tc.name, args)
			req2.Header.Set("Accept", "text/event-stream")
			res, err := http.DefaultClient.Do(req2)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			res.Body.Close()

			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("expected 404 for %s, got %d", tc.name, res.StatusCode)
			}
		})
	}
}

func TestRealtimeEndpointActiveMaxFunctionArgsBytes(t *testing.T) {
	app, service := newTestApp(t)
	bundle := `__pbvex.registerFunction({name:"echo",type:"query",visibility:"public",modulePath:"echo",exportName:"default"}, function(ctx,args) { return args; });`
	req := uploadRequest("realtime", bundle, functionDescriptor("echo", "query", "public"), map[string]any{
		"maxFunctionArgsBytes": 5,
	})
	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	// "null" is 4 bytes, fits.
	req2 := realtimePost(t, server.URL, "echo", "null")
	req2.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for small args, got %d", res.StatusCode)
	}

	// "{}" is 2 bytes, but {"a":1} is 7 bytes and exceeds 5.
	req2 = realtimePost(t, server.URL, "echo", `{"a":1}`)
	req2.Header.Set("Accept", "text/event-stream")
	res, err = http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized args, got %d", res.StatusCode)
	}
}

func TestRealtimeEndpointActivationAndRollbackInvalidation(t *testing.T) {
	app, service := newTestApp(t)

	bundle1 := `__pbvex.registerFunction({name:"version",type:"query",visibility:"public",modulePath:"version",exportName:"default"}, function(ctx,args) { return "v1"; });`
	bundle2 := `__pbvex.registerFunction({name:"version",type:"query",visibility:"public",modulePath:"version",exportName:"default"}, function(ctx,args) { return "v2"; });`

	resp1, err := service.Upload(uploadRequest("realtime", bundle1, functionDescriptor("version", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload v1 failed: %v", err)
	}
	resp2, err := service.Upload(uploadRequest("realtime2", bundle2, functionDescriptor("version", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload v2 failed: %v", err)
	}
	if _, err := service.Activate(resp1.DeploymentID, true); err != nil {
		t.Fatalf("activate v1 failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	// Helper: open a realtime connection and read subscribe + first message.
	connectAndRead := func(t *testing.T, want string) {
		t.Helper()
		req := realtimePost(t, server.URL, "version", `{}`)
		req.Header.Set("Accept", "text/event-stream")
		client := &http.Client{Timeout: 5 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("realtime request failed: %v", err)
		}
		defer res.Body.Close()
		reader := bufio.NewReader(res.Body)
		_ = expectSSEMessage(t, reader) // subscribe
		line := expectSSEMessage(t, reader)
		if !strings.Contains(line, want) {
			t.Fatalf("expected %q, got %s", want, line)
		}
	}

	// v1 active: connection sees v1.
	connectAndRead(t, `"v1"`)

	// Activate v2: ReconnectAll drops active connections. A new connection
	// resolves v2 and sees v2.
	if _, err := service.Activate(resp2.DeploymentID, true); err != nil {
		t.Fatalf("activate v2 failed: %v", err)
	}
	connectAndRead(t, `"v2"`)

	// Rollback restores v1: ReconnectAll drops again, new connection sees v1.
	if _, err := service.Rollback(resp2.DeploymentID); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	connectAndRead(t, `"v1"`)
}

func TestRealtimeEndpointActivationRaceEnforcesQueryType(t *testing.T) {
	app, service := newTestApp(t)

	queryBundle := `__pbvex.registerFunction({name:"toggle",type:"query",visibility:"public",modulePath:"toggle",exportName:"default"}, function(ctx,args) { return "ok"; });`
	actionBundle := `__pbvex.registerFunction({name:"toggle",type:"action",visibility:"public",modulePath:"toggle",exportName:"default"}, function(ctx,args) { return "ok"; });`

	resp1, err := service.Upload(uploadRequest("realtime", queryBundle, functionDescriptor("toggle", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload query failed: %v", err)
	}
	resp2, err := service.Upload(uploadRequest("realtime2", actionBundle, functionDescriptor("toggle", "action", "public"), nil))
	if err != nil {
		t.Fatalf("upload action failed: %v", err)
	}
	if _, err := service.Activate(resp1.DeploymentID, true); err != nil {
		t.Fatalf("activate query failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	// Helper: open a realtime connection and read subscribe + first message.
	connectAndRead := func(t *testing.T, wantSubstr string, wantStatus int) {
		t.Helper()
		req := realtimePost(t, server.URL, "toggle", `{}`)
		req.Header.Set("Accept", "text/event-stream")
		client := &http.Client{Timeout: 5 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("realtime request failed: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != wantStatus {
			t.Fatalf("expected status %d, got %d", wantStatus, res.StatusCode)
		}
		if wantStatus != http.StatusOK {
			return
		}
		reader := bufio.NewReader(res.Body)
		_ = expectSSEMessage(t, reader) // subscribe
		line := expectSSEMessage(t, reader)
		if !strings.Contains(line, wantSubstr) {
			t.Fatalf("expected %q, got %s", wantSubstr, line)
		}
	}

	// Query active: connection succeeds and sees "ok".
	connectAndRead(t, `"ok"`, http.StatusOK)

	// Activate action: ReconnectAll drops. New connection hits admission
	// which rejects non-query → 404.
	if _, err := service.Activate(resp2.DeploymentID, true); err != nil {
		t.Fatalf("activate action failed: %v", err)
	}
	connectAndRead(t, "", http.StatusNotFound)

	// Rollback restores query: new connection succeeds again.
	if _, err := service.Rollback(resp2.DeploymentID); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	connectAndRead(t, `"ok"`, http.StatusOK)
}

func TestRealtimeEndpointRecordCreateInvalidates(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"random",type:"query",visibility:"public",modulePath:"random",exportName:"default"}, function(ctx,args) { return Math.random(); });`
	resp, err := service.Upload(uploadRequest("realtime", bundle, functionDescriptor("random", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`
	req := realtimePost(t, server.URL, "random", args)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()

	reader := bufio.NewReader(res.Body)
	_ = expectSSEMessage(t, reader) // subscribe
	line1 := expectSSEMessage(t, reader)
	if !strings.Contains(line1, `"message"`) {
		t.Fatalf("expected message, got %s", line1)
	}

	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("failed to find users collection: %v", err)
	}
	record := core.NewRecord(col)
	record.Set("email", "invalidation-test@example.com")
	record.SetPassword("12345678")
	if err := app.Save(record); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	line2 := expectSSEMessage(t, reader)
	if !strings.Contains(line2, `"message"`) {
		t.Fatalf("expected message after create, got %s", line2)
	}
}

func TestRealtimeEndpointFailedMutationDoesNotInvalidate(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"random",type:"query",visibility:"public",modulePath:"random",exportName:"default"}, function(ctx,args) { return Math.random(); });__pbvex.registerFunction({name:"thrower",type:"mutation",visibility:"public",modulePath:"thrower",exportName:"default"}, function(ctx,args) { throw new Error("boom"); });`
	resp, err := service.Upload(uploadRequestWithFunctions("realtime", bundle, []any{
		functionDescriptor("random", "query", "public"),
		functionDescriptor("thrower", "mutation", "public"),
	}, nil))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`
	req := realtimePost(t, server.URL, "random", args)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()

	reader := bufio.NewReader(res.Body)
	_ = expectSSEMessage(t, reader) // subscribe
	line1 := expectSSEMessage(t, reader)
	if !strings.Contains(line1, `"message"`) {
		t.Fatalf("expected message, got %s", line1)
	}

	callURL := server.URL + "/api/pbvex/call"
	body, _ := json.Marshal(map[string]any{"name": "thrower", "args": map[string]any{}})
	callResp, err := http.Post(callURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	callResp.Body.Close()
	if callResp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 from call, got %d", callResp.StatusCode)
	}

	done := make(chan string, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			done <- ""
			return
		}
		done <- strings.TrimSpace(line)
	}()

	select {
	case line := <-done:
		if line != "" && !strings.Contains(line, `"op":"ping"`) {
			t.Fatalf("expected no new message after failed mutation, got %s", line)
		}
	case <-time.After(500 * time.Millisecond):
		// expected: no new message within 500ms
	}
}

func TestRealtimeEndpointCoalescesUnderSlowQuery(t *testing.T) {
	app, service, invalidator := newTestAppWithBroadcaster(t, realtime.Config{PingInterval: 1 * time.Hour})

	bundle := `__pbvex.registerFunction({name:"slow",type:"query",visibility:"public",modulePath:"slow",exportName:"default"}, function(ctx,args) { var end = Date.now() + 500; while (Date.now() < end) {} return Math.random(); });`
	resp, err := service.Upload(uploadRequest("realtime", bundle, functionDescriptor("slow", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`
	req := realtimePost(t, server.URL, "slow", args)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()

	reader := bufio.NewReader(res.Body)
	_ = expectSSEMessage(t, reader) // subscribe

	// Fire several invalidations while the initial slow query is running.
	for i := 0; i < 5; i++ {
		invalidator.InvalidateAll()
	}

	line1 := expectSSEMessage(t, reader)
	if !strings.Contains(line1, `"op":"message"`) {
		t.Fatalf("expected initial message, got %s", line1)
	}

	line2 := expectSSEMessage(t, reader)
	if !strings.Contains(line2, `"op":"message"`) {
		t.Fatalf("expected single re-evaluation after coalescing, got %s", line2)
	}

	// No further message should arrive quickly; the coalesced invalidations
	// should have produced exactly one re-run.
	done := make(chan string, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			done <- ""
			return
		}
		done <- strings.TrimSpace(line)
	}()

	select {
	case line := <-done:
		if line != "" && !strings.Contains(line, `"op":"ping"`) {
			t.Fatalf("expected no extra message after coalescing, got %s", line)
		}
	case <-time.After(500 * time.Millisecond):
		// expected
	}
}

func TestRealtimeEndpointDisconnectCancelsSlowQuery(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"slow",type:"query",visibility:"public",modulePath:"slow",exportName:"default"}, function(ctx,args) { var end = Date.now() + 5000; while (Date.now() < end) {} return "done"; });`
	resp, err := service.Upload(uploadRequest("realtime", bundle, functionDescriptor("slow", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`
	req := realtimePost(t, server.URL, "slow", args)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 200 * time.Millisecond}

	start := time.Now()
	res, err := client.Do(req)
	if err == nil {
		// The server may have flushed headers before the timeout, so drain the
		// body to ensure the client timeout is actually hit.
		if _, err := io.Copy(io.Discard, res.Body); err == nil {
			t.Fatal("expected client timeout error")
		}
		res.Body.Close()
	}
	if time.Since(start) > 1*time.Second {
		t.Fatalf("client timeout took too long: %v", time.Since(start))
	}

	// Give the server a moment to wind down the cancelled goroutine.
	time.Sleep(200 * time.Millisecond)
}

func TestRealtimeEndpointStructuredErrorForTimeout(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"slow",type:"query",visibility:"public",modulePath:"slow",exportName:"default"}, function(ctx,args) { var end = Date.now() + 5000; while (Date.now() < end) {} return "done"; });`
	resp, err := service.Upload(uploadRequest("realtime", bundle, functionDescriptor("slow", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`
	req := realtimePost(t, server.URL, "slow", args)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()

	reader := bufio.NewReader(res.Body)
	_ = expectSSEMessage(t, reader) // subscribe
	line := expectSSEMessage(t, reader)
	if !strings.Contains(line, `"error":true`) {
		t.Fatalf("expected error payload, got %s", line)
	}
	if !strings.Contains(line, `"Function invocation timed out."`) {
		t.Fatalf("expected timeout message, got %s", line)
	}
}

func TestRealtimeEndpointStructuredErrorForReturnValueSize(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"big",type:"query",visibility:"public",modulePath:"big",exportName:"default"}, function(ctx,args) { return "hello"; });`
	resp, err := service.Upload(uploadRequest("realtime", bundle, functionDescriptor("big", "query", "public"), map[string]any{
		"maxReturnValueBytes": 1,
	}))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`
	req := realtimePost(t, server.URL, "big", args)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()

	reader := bufio.NewReader(res.Body)
	_ = expectSSEMessage(t, reader) // subscribe
	line := expectSSEMessage(t, reader)
	if !strings.Contains(line, `"error":true`) {
		t.Fatalf("expected error payload, got %s", line)
	}
	if !strings.Contains(line, `"Return value exceeds configured limit."`) {
		t.Fatalf("expected return-size message, got %s", line)
	}
}

func TestRealtimeEndpointPingFraming(t *testing.T) {
	app, service, _ := newTestAppWithBroadcaster(t, realtime.Config{PingInterval: 100 * time.Millisecond})

	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{"name":"world"}`
	req := realtimePost(t, server.URL, "hello", args)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()

	reader := bufio.NewReader(res.Body)
	_ = expectSSEMessage(t, reader) // subscribe
	_ = expectSSEMessage(t, reader) // message

	for i := 0; i < 3; i++ {
		line := expectSSEMessage(t, reader)
		if !strings.Contains(line, `"op":"ping"`) {
			t.Fatalf("expected ping, got %s", line)
		}
	}
}

func TestRealtimeEndpointRejectsMissingContentType(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{"name":"world"}`
	req := realtimePost(t, server.URL, "hello", args)
	req.Header.Del("Content-Type")
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusUnsupportedMediaType && res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 or 415, got %d", res.StatusCode)
	}
}

func TestRealtimeEndpointRejectsNonJSONBody(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`
	req := realtimePostID(t, server.URL, "hello", args, "")
	req.Header.Set("Accept", "text/event-stream")
	req.Body = io.NopCloser(strings.NewReader("not json"))
	req.ContentLength = int64(len("not json"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestRealtimeEndpointRejectsDuplicateBodyKeys(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`
	var v any
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		t.Fatalf("invalid args JSON: %v", err)
	}
	id := realtime.DeriveSubscriptionID("v1", "hello", v)
	body := fmt.Sprintf(`{"id":%q,"id":%q,"path":"hello","args":{}}`, id, id)
	req, err := http.NewRequest("POST", server.URL+"/api/pbvex/realtime", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestRealtimeEndpointRejectsMismatchedArgs(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{"name":"world"}`
	req := realtimePostID(t, server.URL, "hello", args, strings.Repeat("0", 64))
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestRealtimeEndpointRejectsOversizedBody(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`
	req := realtimePostID(t, server.URL, "hello", args, "")
	req.Header.Set("Accept", "text/event-stream")
	oversized := strings.Repeat("a", int(realtime.DefaultConfig().MaxBodyBytes+1))
	req.Body = io.NopCloser(strings.NewReader(oversized))
	req.ContentLength = int64(len(oversized))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", res.StatusCode)
	}
}

// notFoundBody performs a realtime POST and returns the parsed error envelope
// for a not-found-class case. All public resolution failures must be
// indistinguishable: missing deployment, unknown function, internal function,
// and wrong-type function. The requestId field is necessarily unique per
// request, so it is stripped before comparison.
func notFoundEnvelope(t *testing.T, serverURL, path, args string) map[string]any {
	t.Helper()
	req := realtimePost(t, serverURL, path, args)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for %q, got %d: %s", path, res.StatusCode, string(body))
	}
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("invalid JSON for %q: %v", path, err)
	}
	delete(env, "requestId")
	return env
}

func assertSameNotFound(t *testing.T, label string, got, want map[string]any) {
	t.Helper()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("%s not-found envelope differs from baseline:\n  got=%s\n  want=%s", label, gotJSON, wantJSON)
	}
}

// TestRealtimeEndpointNotFoundIsIndistinguishable asserts that every public
// resolution failure (no active deployment, unknown function, internal query,
// public mutation, public action) collapses to an identical 404 envelope
// (same code and message) so that callers cannot enumerate internal functions.
func TestRealtimeEndpointNotFoundIsIndistinguishable(t *testing.T) {
	app, service := newTestApp(t)

	mixedBundle := `__pbvex.registerFunction({name:"intern",type:"query",visibility:"internal",modulePath:"intern",exportName:"default"}, function(ctx,args) { return "secret"; });__pbvex.registerFunction({name:"mut",type:"mutation",visibility:"public",modulePath:"mut",exportName:"default"}, function(ctx,args) { return 1; });__pbvex.registerFunction({name:"act",type:"action",visibility:"public",modulePath:"act",exportName:"default"}, function(ctx,args) { return 1; });`
	resp, err := service.Upload(uploadRequestWithFunctions("realtime", mixedBundle, []any{
		functionDescriptor("intern", "query", "internal"),
		functionDescriptor("mut", "mutation", "public"),
		functionDescriptor("act", "action", "public"),
	}, nil))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	args := `{}`

	// Baseline: no active deployment at all.
	noActiveApp, noActiveService := newTestApp(t)
	noActiveServer := startRealtimeServer(t, noActiveApp, noActiveService)
	defer noActiveServer.Close()
	baseline := notFoundEnvelope(t, noActiveServer.URL, "hello", args)

	// Unknown function name on an active deployment.
	assertSameNotFound(t, "unknown function", notFoundEnvelope(t, server.URL, "missing", args), baseline)

	// Internal query.
	assertSameNotFound(t, "internal query", notFoundEnvelope(t, server.URL, "intern", args), baseline)

	// Public mutation (wrong type).
	assertSameNotFound(t, "public mutation", notFoundEnvelope(t, server.URL, "mut", args), baseline)

	// Public action (wrong type).
	assertSameNotFound(t, "public action", notFoundEnvelope(t, server.URL, "act", args), baseline)
}

// TestRealtimeEndpointGETCollapsesNotFound verifies the bounded GET fallback
// also collapses resolution failures to the indistinguishable 404 envelope.
func TestRealtimeEndpointGETCollapsesNotFound(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(testUploadRequest("realtime", testBundleJS))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	// Unknown function via GET.
	id := realtime.DeriveSubscriptionID("v1", "missing", map[string]any{})
	getURL := server.URL + "/api/pbvex/realtime?id=" + id + "&path=missing&args=%7B%7D"
	getReq, err := http.NewRequest("GET", getURL, nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	getReq.Header.Set("Accept", "text/event-stream")
	getRes, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	getBody, _ := io.ReadAll(getRes.Body)
	getRes.Body.Close()
	if getRes.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for GET unknown, got %d", getRes.StatusCode)
	}
	var getEnv map[string]any
	if err := json.Unmarshal(getBody, &getEnv); err != nil {
		t.Fatalf("invalid GET JSON: %v", err)
	}
	delete(getEnv, "requestId")

	postEnv := notFoundEnvelope(t, server.URL, "missing", `{}`)
	assertSameNotFound(t, "GET vs POST", getEnv, postEnv)
}

// TestRealtimeEndpointQuerySemaphoreReleasedOnCancel verifies that a
// disconnected slow query releases the query semaphore so subsequent
// subscriptions are not starved.
func TestRealtimeEndpointQuerySemaphoreReleasedOnCancel(t *testing.T) {
	cfg := realtime.DefaultConfig()
	cfg.MaxConcurrentQueries = 1
	cfg.PingInterval = 1 * time.Hour
	app, service, _ := newTestAppWithBroadcaster(t, cfg)

	bundle := `__pbvex.registerFunction({name:"slow",type:"query",visibility:"public",modulePath:"slow",exportName:"default"}, function(ctx,args) { var end = Date.now() + 3000; while (Date.now() < end) {} return "done"; });`
	resp, err := service.Upload(uploadRequest("realtime", bundle, functionDescriptor("slow", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	// Start a subscription whose query will block for 3s, then disconnect
	// immediately so the in-flight query is cancelled.
	args := `{}`
	req := realtimePost(t, server.URL, "slow", args)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 100 * time.Millisecond}
	res, err := client.Do(req)
	if err == nil {
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}

	// Give the server time to process the cancellation.
	time.Sleep(200 * time.Millisecond)

	// A new subscription must be able to acquire the semaphore. A fast query
	// under the same deployment would starve if the semaphore leaked.
	fastBundle := `__pbvex.registerFunction({name:"slow",type:"query",visibility:"public",modulePath:"slow",exportName:"default"}, function(ctx,args) { return "fast"; });`
	fastResp, err := service.Upload(uploadRequest("fast", fastBundle, functionDescriptor("slow", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload fast failed: %v", err)
	}
	if _, err := service.Activate(fastResp.DeploymentID, true); err != nil {
		t.Fatalf("activate fast failed: %v", err)
	}

	req2 := realtimePost(t, server.URL, "slow", args)
	req2.Header.Set("Accept", "text/event-stream")
	client2 := &http.Client{Timeout: 3 * time.Second}
	res2, err := client2.Do(req2)
	if err != nil {
		t.Fatalf("second subscription failed (semaphore leaked?): %v", err)
	}
	defer res2.Body.Close()

	reader := bufio.NewReader(res2.Body)
	_ = expectSSEMessage(t, reader) // subscribe
	line := expectSSEMessage(t, reader)
	if !strings.Contains(line, `"fast"`) {
		t.Fatalf("expected fast result, got %s", line)
	}
}

// TestCallEndpointResolutionFailuresIndistinguishable verifies that the
// public /api/pbvex/call endpoint returns byte-identical 404 responses
// (modulo requestId) for every target class: unknown function, internal
// visibility, wrong kind (httpAction), and no active deployment.
func TestCallEndpointResolutionFailuresIndistinguishable(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"intern",type:"query",visibility:"internal",modulePath:"intern",exportName:"default"}, function(ctx,args) { return "secret"; });__pbvex.registerFunction({name:"httpact",type:"httpAction",visibility:"public",modulePath:"httpact",exportName:"default",route:{method:"GET",path:"httpact"}}, function(ctx,args) { return "ok"; });__pbvex.registerFunction({name:"pub",type:"query",visibility:"public",modulePath:"pub",exportName:"default"}, function(ctx,args) { return "ok"; });`
	httpDescriptor := functionDescriptor("httpact", "httpAction", "public")
	httpDescriptor["route"] = map[string]any{"method": "GET", "path": "httpact"}
	resp, err := service.Upload(uploadRequestWithFunctions("realtime", bundle, []any{
		functionDescriptor("intern", "query", "internal"),
		httpDescriptor,
		functionDescriptor("pub", "query", "public"),
	}, nil))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	callEnvelope := func(t *testing.T, fn string) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"name": fn, "args": map[string]any{}})
		res, err := http.Post(server.URL+"/api/pbvex/call", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("call failed: %v", err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for %q, got %d: %s", fn, res.StatusCode, string(raw))
		}
		var env map[string]any
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("invalid JSON for %q: %v", fn, err)
		}
		delete(env, "requestId")
		return env
	}

	// Baseline: no active deployment.
	noActiveApp, noActiveService := newTestApp(t)
	noActiveServer := startRealtimeServer(t, noActiveApp, noActiveService)
	defer noActiveServer.Close()
	noActiveBody, _ := json.Marshal(map[string]any{"name": "anything", "args": map[string]any{}})
	noActiveRes, err := http.Post(noActiveServer.URL+"/api/pbvex/call", "application/json", strings.NewReader(string(noActiveBody)))
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	noActiveRaw, _ := io.ReadAll(noActiveRes.Body)
	noActiveRes.Body.Close()
	if noActiveRes.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for no-active, got %d", noActiveRes.StatusCode)
	}
	var baseline map[string]any
	json.Unmarshal(noActiveRaw, &baseline)
	delete(baseline, "requestId")

	// All target classes must be indistinguishable from the baseline.
	for _, fn := range []string{"missing", "intern", "httpact"} {
		got := callEnvelope(t, fn)
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(baseline)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("call %q envelope differs from baseline:\n  got=%s\n  want=%s", fn, gotJSON, wantJSON)
		}
	}

	// Realtime must also collapse all resolution failures identically.
	rtEnvelope := func(t *testing.T, fn string) map[string]any {
		t.Helper()
		req := realtimePost(t, server.URL, fn, `{}`)
		req.Header.Set("Accept", "text/event-stream")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("realtime failed: %v", err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for realtime %q, got %d", fn, res.StatusCode)
		}
		var env map[string]any
		json.Unmarshal(raw, &env)
		delete(env, "requestId")
		return env
	}

	for _, fn := range []string{"missing", "intern", "httpact"} {
		got := rtEnvelope(t, fn)
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(baseline)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("realtime %q envelope differs from baseline:\n  got=%s\n  want=%s", fn, gotJSON, wantJSON)
		}
	}
}

// TestRealtimeEndpointCustomLimitAdmission verifies that a deployment with
// maxFunctionArgsBytes above the 1 MiB default can actually admit bodies
// larger than 1 MiB (the initial admission uses the protocol ceiling, not
// the startup default), while bodies above the deployment-specific limit are
// rejected after resolution.
func TestRealtimeEndpointCustomLimitAdmission(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"echo",type:"query",visibility:"public",modulePath:"echo",exportName:"default"}, function(ctx,args) { return args; });`
	resp, err := service.Upload(uploadRequest("realtime", bundle, functionDescriptor("echo", "query", "public"), map[string]any{
		"maxFunctionArgsBytes": 2 * 1024 * 1024,
	}))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	// Build args just above 1 MiB (the old startup default) but below the
	// deployment's 2 MiB limit. This must be admitted.
	bigStr := strings.Repeat("x", 1100000)
	argsObj := map[string]any{"data": bigStr}
	id := realtime.DeriveSubscriptionID("v1", "echo", argsObj)

	body, _ := json.Marshal(map[string]any{"id": id, "path": "echo", "args": argsObj})
	req, err := http.NewRequest("POST", server.URL+"/api/pbvex/realtime", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for body above old 1MiB default but below 2MiB deployment limit, got %d", res.StatusCode)
	}

	// Build args above the deployment's 2 MiB limit — must be rejected.
	// Use 3 MiB so the total body clearly exceeds deployBodyLimit
	// (maxFunctionArgsBytes + envelope overhead) and hits the 413 check
	// before the args-level 400 check.
	tooBigStr := strings.Repeat("y", 3*1024*1024)
	tooBigArgs := map[string]any{"data": tooBigStr}
	tooBigID := realtime.DeriveSubscriptionID("v1", "echo", tooBigArgs)

	tooBigBody, _ := json.Marshal(map[string]any{"id": tooBigID, "path": "echo", "args": tooBigArgs})
	req2, err := http.NewRequest("POST", server.URL+"/api/pbvex/realtime", strings.NewReader(string(tooBigBody)))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept", "text/event-stream")
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for body above deployment limit, got %d", res2.StatusCode)
	}
}

// TestRealtimeEndpointActivationRefreshesMaxEventSize verifies that a
// low-to-high maxReturnValueBytes switch on activation causes the client to
// reconnect with the new (higher) limit. The old connection is pinned to the
// low-limit snapshot; ReconnectAll forces a new connection that negotiates
// the high limit and delivers the larger result.
func TestRealtimeEndpointActivationRefreshesMaxEventSize(t *testing.T) {
	app, service := newTestApp(t)

	// Low-limit deployment: returns 50 bytes (fits in 100-byte limit).
	lowBundle := `__pbvex.registerFunction({name:"sized",type:"query",visibility:"public",modulePath:"sized",exportName:"default"}, function(ctx,args) { return "x".repeat(50); });`
	lowResp, err := service.Upload(uploadRequest("low", lowBundle, functionDescriptor("sized", "query", "public"), map[string]any{
		"maxReturnValueBytes": 100,
	}))
	if err != nil {
		t.Fatalf("upload low failed: %v", err)
	}
	if _, err := service.Activate(lowResp.DeploymentID, true); err != nil {
		t.Fatalf("activate low failed: %v", err)
	}

	// High-limit deployment: returns 5000 bytes (exceeds old 100+4096
	// maxEventSize, fits new 1MB+4096 limit).
	highBundle := `__pbvex.registerFunction({name:"sized",type:"query",visibility:"public",modulePath:"sized",exportName:"default"}, function(ctx,args) { return "x".repeat(5000); });`
	highResp, err := service.Upload(uploadRequest("high", highBundle, functionDescriptor("sized", "query", "public"), map[string]any{
		"maxReturnValueBytes": 1 * 1024 * 1024,
	}))
	if err != nil {
		t.Fatalf("upload high failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	// Helper: connect, read subscribe + first message.
	connectAndReadMessage := func(t *testing.T) string {
		t.Helper()
		req := realtimePost(t, server.URL, "sized", `{}`)
		req.Header.Set("Accept", "text/event-stream")
		client := &http.Client{Timeout: 5 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("realtime request failed: %v", err)
		}
		defer res.Body.Close()
		reader := bufio.NewReader(res.Body)
		_ = expectSSEMessage(t, reader) // subscribe
		return expectSSEMessage(t, reader)
	}

	// Low-limit active: sees the 50-byte result.
	line1 := connectAndReadMessage(t)
	if !strings.Contains(line1, `"op":"message"`) {
		t.Fatalf("expected message under low limit, got %s", line1)
	}

	// Activate high-limit deployment: ReconnectAll drops the old connection.
	// A new connection pins the high-limit snapshot and delivers the 5000-byte
	// result that would have exceeded the old 100+4096 maxEventSize.
	if _, err := service.Activate(highResp.DeploymentID, true); err != nil {
		t.Fatalf("activate high failed: %v", err)
	}
	line2 := connectAndReadMessage(t)
	if !strings.Contains(line2, `"op":"message"`) {
		t.Fatalf("expected message under high limit, got %s", line2)
	}
	if !strings.Contains(line2, strings.Repeat("x", 100)) {
		t.Fatalf("expected large result after reconnect, got (truncated) %s", line2[:min(200, len(line2))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestCallEndpointNoActiveDeploymentIsNotFound verifies that calling any
// function when there is no active deployment returns 404 (function not
// found), not 500 or 403.
func TestCallEndpointNoActiveDeploymentIsNotFound(t *testing.T) {
	app, service := newTestApp(t)
	server := startRealtimeServer(t, app, service)
	defer server.Close()

	callBody, _ := json.Marshal(map[string]any{"name": "anything", "args": map[string]any{}})
	callRes, err := http.Post(server.URL+"/api/pbvex/call", "application/json", strings.NewReader(string(callBody)))
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	callRes.Body.Close()
	if callRes.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for call with no active deployment, got %d", callRes.StatusCode)
	}
}

// TestManifestRejectsConfigAboveProtocolLimit verifies that manifest validation
// rejects maxFunctionArgsBytes and maxReturnValueBytes above the canonical
// protocol limit, while exactly the limit is accepted.
func TestManifestRejectsConfigAboveProtocolLimit(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"noop",type:"query",visibility:"public",modulePath:"noop",exportName:"default"}, function(ctx,args) { return null; });`
	fn := functionDescriptor("noop", "query", "public")

	// Exactly at the limit → accepted.
	_, err := service.Upload(uploadRequest("at_limit", bundle, fn, map[string]any{
		"maxFunctionArgsBytes": deploy.MaxFunctionArgsLimit,
		"maxReturnValueBytes":  deploy.MaxReturnValueLimit,
	}))
	if err != nil {
		t.Fatalf("upload at limit failed: %v", err)
	}

	// maxFunctionArgsBytes above limit → rejected.
	_, err = service.Upload(uploadRequest("args_over", bundle, fn, map[string]any{
		"maxFunctionArgsBytes": deploy.MaxFunctionArgsLimit + 1,
	}))
	if err == nil {
		t.Fatal("expected error for maxFunctionArgsBytes above limit")
	}

	// maxReturnValueBytes above limit → rejected.
	_, err = service.Upload(uploadRequest("ret_over", bundle, fn, map[string]any{
		"maxReturnValueBytes": deploy.MaxReturnValueLimit + 1,
	}))
	if err == nil {
		t.Fatal("expected error for maxReturnValueBytes above limit")
	}
}

// TestRealtimeEndpointAdmitsExactMaxFunctionArgs verifies end-to-end that a
// deployment configured with maxFunctionArgsBytes == MaxFunctionArgsLimit can
// admit a request body whose args are exactly the limit, while the deployment-
// specific body check rejects args that exceed it.
func TestRealtimeEndpointAdmitsExactMaxFunctionArgs(t *testing.T) {
	app, service := newTestApp(t)

	// Use a small config to keep the test fast but prove the boundary logic.
	bundle := `__pbvex.registerFunction({name:"echo",type:"query",visibility:"public",modulePath:"echo",exportName:"default"}, function(ctx,args) { return args; });`
	limit := int64(4096)
	resp, err := service.Upload(uploadRequest("realtime", bundle, functionDescriptor("echo", "query", "public"), map[string]any{
		"maxFunctionArgsBytes": limit,
	}))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()

	// Build args that are exactly the limit (the raw JSON string length).
	// A JSON string of length (limit-2) for the quotes gives exactly limit bytes.
	exact := `"` + strings.Repeat("a", int(limit)-2) + `"`
	if int64(len(exact)) != limit {
		t.Fatalf("exact args length %d != limit %d", len(exact), limit)
	}
	var exactVal any
	json.Unmarshal([]byte(exact), &exactVal)
	id := realtime.DeriveSubscriptionID("v1", "echo", exactVal)
	body, _ := json.Marshal(map[string]any{"id": id, "path": "echo", "args": json.RawMessage(exact)})

	req, err := http.NewRequest("POST", server.URL+"/api/pbvex/realtime", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for args exactly at limit, got %d", res.StatusCode)
	}

	// Args at limit+1 → rejected (413 from deployment body check or 400 from args check).
	over := `"` + strings.Repeat("a", int(limit)-1) + `"`
	if int64(len(over)) != limit+1 {
		t.Fatalf("over args length %d != limit+1 %d", len(over), limit+1)
	}
	var overVal any
	json.Unmarshal([]byte(over), &overVal)
	overID := realtime.DeriveSubscriptionID("v1", "echo", overVal)
	overBody, _ := json.Marshal(map[string]any{"id": overID, "path": "echo", "args": json.RawMessage(over)})

	req2, err := http.NewRequest("POST", server.URL+"/api/pbvex/realtime", strings.NewReader(string(overBody)))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept", "text/event-stream")
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusBadRequest && res2.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 400 or 413 for args above limit, got %d", res2.StatusCode)
	}
}

// TestInvokeSnapshotPinsDeployment proves that InvokeSnapshot runs against
// the exact deployment captured at admission time, not the currently active
// deployment. This is the core guarantee that prevents the activation
// limit-race: a subscription admitted on v1 never invokes v2 even after
// v2 is activated.
func TestInvokeSnapshotPinsDeployment(t *testing.T) {
	app, service := newTestApp(t)
	_ = app

	// Deploy v1 returning "v1".
	bundle1 := `__pbvex.registerFunction({name:"version",type:"query",visibility:"public",modulePath:"version",exportName:"default"}, function(ctx,args) { return "v1"; });`
	resp1, err := service.Upload(uploadRequest("v1", bundle1, functionDescriptor("version", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload v1 failed: %v", err)
	}
	if _, err := service.Activate(resp1.DeploymentID, true); err != nil {
		t.Fatalf("activate v1 failed: %v", err)
	}

	// Resolve the v1 snapshot (this is what admission does).
	snap, err := service.ResolvePublicQuery(context.Background(), "version")
	if err != nil {
		t.Fatalf("resolve v1 snapshot failed: %v", err)
	}

	// Deploy and activate v2 returning "v2".
	bundle2 := `__pbvex.registerFunction({name:"version",type:"query",visibility:"public",modulePath:"version",exportName:"default"}, function(ctx,args) { return "v2"; });`
	resp2, err := service.Upload(uploadRequest("v2", bundle2, functionDescriptor("version", "query", "public"), nil))
	if err != nil {
		t.Fatalf("upload v2 failed: %v", err)
	}
	if _, err := service.Activate(resp2.DeploymentID, true); err != nil {
		t.Fatalf("activate v2 failed: %v", err)
	}

	// InvokeSnapshot against the v1 snapshot must return "v1", not "v2".
	result, err := service.InvokeSnapshot(context.Background(), snap, map[string]any{})
	if err != nil {
		t.Fatalf("InvokeSnapshot failed: %v", err)
	}
	if result != "v1" {
		t.Fatalf("expected pinned v1 result, got %v", result)
	}

	// A fresh CallQuery resolves the active v2 and returns "v2".
	result2, err := service.CallQuery(context.Background(), "version", map[string]any{})
	if err != nil {
		t.Fatalf("CallQuery failed: %v", err)
	}
	if result2 != "v2" {
		t.Fatalf("expected active v2 result, got %v", result2)
	}
}
