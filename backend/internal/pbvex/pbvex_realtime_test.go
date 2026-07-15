package pbvex

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/realtime"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func TestRealtimeEndpointHeadersAndEarlyFlush(t *testing.T) {
	app, service := newTestApp(t)

	bundle := testBundleJS
	resp, err := service.Upload(testUploadRequest("realtime", bundle))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

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

	server := httptest.NewServer(mux)
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

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %s", ct)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") && !strings.Contains(cc, "no-transform") {
		t.Fatalf("expected cache-control no-cache/no-transform, got %s", cc)
	}
	if res.Header.Get("Connection") != "keep-alive" {
		t.Fatalf("expected Connection: keep-alive, got %s", res.Header.Get("Connection"))
	}

	reader := bufio.NewReader(res.Body)
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read first event line: %v", err)
	}
	first = strings.TrimSpace(first)
	if !strings.HasPrefix(first, "data: {") {
		t.Fatalf("expected SSE data event, got %s", first)
	}
	if !strings.Contains(first, `"op":"subscribe"`) {
		t.Fatalf("expected subscribe envelope first, got %s", first)
	}

	// second line should be blank separator
	blank, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read blank line: %v", err)
	}
	if strings.TrimSpace(blank) != "" {
		t.Fatalf("expected blank separator, got %s", blank)
	}

	// next event should be message with payload
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read message event: %v", err)
	}
	line = strings.TrimSpace(line)
	if !strings.Contains(line, `"op":"message"`) {
		t.Fatalf("expected message envelope, got %s", line)
	}
	if !strings.Contains(line, `"Hello, world!"`) {
		t.Fatalf("expected payload result, got %s", line)
	}
}

func TestRealtimeEndpointSubscribeEnvelopeShape(t *testing.T) {
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

	req := realtimePost(t, server.URL, "hello", `{"name":"world"}`)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()

	reader := bufio.NewReader(res.Body)
	first := sseReadLine(t, reader)

	// maxEventSize must be a top-level field of the realtime envelope data,
	// not nested under payload.
	var env struct {
		Data struct {
			ID           string           `json:"id"`
			Op           string           `json:"op"`
			MaxEventSize int64            `json:"maxEventSize"`
			Payload      *json.RawMessage `json:"payload,omitempty"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(first, "data: ")), &env); err != nil {
		t.Fatalf("failed to parse subscribe envelope: %v\nline: %s", err, first)
	}
	if env.Data.Op != "subscribe" {
		t.Fatalf("expected op=subscribe, got %q", env.Data.Op)
	}
	if env.Data.MaxEventSize <= 0 {
		t.Fatalf("expected top-level maxEventSize > 0, got %d", env.Data.MaxEventSize)
	}
	if env.Data.Payload != nil {
		t.Fatalf("subscribe envelope must not nest maxEventSize under payload; got payload=%s", string(*env.Data.Payload))
	}
}

func TestRealtimeEndpointRejectsMissingAccept(t *testing.T) {
	app, service := newTestApp(t)

	bundle := testBundleJS
	resp, err := service.Upload(testUploadRequest("realtime", bundle))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

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

	server := httptest.NewServer(mux)
	defer server.Close()

	args := `{"name":"world"}`
	req := realtimePost(t, server.URL, "hello", args)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("expected status 406, got %d", res.StatusCode)
	}
}

func TestRealtimeEndpointRejectsInternalAction(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"admin",type:"action",visibility:"internal",modulePath:"admin",exportName:"default"}, function(ctx,args) { return "secret"; });`
	req := map[string]any{
		"manifest": map[string]any{
			"protocolVersion": "v1",
			"deploymentId":    "realtime",
			"functions": []any{
				map[string]any{
					"name":       "admin",
					"type":       "action",
					"visibility": "internal",
					"modulePath": "admin",
					"exportName": "default",
				},
			},
		},
		"bundle": testBundle(bundle),
		"sha256": bundleHash(bundle),
		"size":   int64(len(bundle)),
	}
	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

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

	server := httptest.NewServer(mux)
	defer server.Close()

	args := `{}`
	req2 := realtimePost(t, server.URL, "admin", args)
	req2.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Do(req2)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", res.StatusCode)
	}
}

func TestRealtimeEndpointRerunsDoNotShareModuleState(t *testing.T) {
	app, service, invalidator := newTestAppWithBroadcaster(t, realtime.DefaultConfig())

	bundle := `let calls = 0;
__pbvex.registerFunction({name:"static",type:"query",visibility:"public",modulePath:"static",exportName:"default"}, function(ctx,args) { calls += 1; return calls; });`
	req := testUploadRequest("realtime", bundle, "static")
	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

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

	server := httptest.NewServer(mux)
	defer server.Close()

	args := `{}`
	req2 := realtimePost(t, server.URL, "static", args)
	req2.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req2)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()

	reader := bufio.NewReader(res.Body)
	// subscribe
	_, _ = reader.ReadString('\n')
	_, _ = reader.ReadString('\n')

	// first message
	line1, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}
	line1 = strings.TrimSpace(line1)

	// trigger invalidation
	invalidator.InvalidateAll()

	// A rerun executes in a fresh runtime, so calls is 1 again. The identical
	// result is deduplicated instead of exposing module state from the prior run.
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
			t.Fatalf("expected no duplicate message, got %s", line)
		}
	case <-time.After(1 * time.Second):
		// expected: no duplicate message within 1s
	}
}

func TestScheduledMutationInvalidatesRealtimeQuery(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"count",type:"query",visibility:"public",modulePath:"pbvex/notes.ts",exportName:"count"},async function(ctx){return await ctx.db.query("notes").collect()});
__pbvex.registerFunction({name:"schedule",type:"mutation",visibility:"public",modulePath:"pbvex/notes.ts",exportName:"schedule"},async function(ctx){await ctx.scheduler.runAfter(0,{_path:"write",_type:"mutation",_visibility:"internal"},{});return null});
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"internal",modulePath:"pbvex/notes.ts",exportName:"write"},async function(ctx){return await ctx.db.insert("notes",{body:"scheduled"})});`
	req := testUploadRequest("scheduled_realtime", bundle, "count")
	manifest := req["manifest"].(map[string]any)
	manifest["functions"] = []any{
		map[string]any{"name": "count", "type": "query", "visibility": "public", "modulePath": "pbvex/notes.ts", "exportName": "count"},
		map[string]any{"name": "schedule", "type": "mutation", "visibility": "public", "modulePath": "pbvex/notes.ts", "exportName": "schedule"},
		map[string]any{"name": "write", "type": "mutation", "visibility": "internal", "modulePath": "pbvex/notes.ts", "exportName": "write"},
	}
	manifest["schema"] = map[string]any{"tables": []any{map[string]any{
		"tableName": "notes",
		"fields":    map[string]any{"body": map[string]any{"type": "string"}},
	}}}
	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate failed: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()
	realtimeReq := realtimePost(t, server.URL, "count", `{}`)
	realtimeReq.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(realtimeReq)
	if err != nil {
		t.Fatalf("realtime request failed: %v", err)
	}
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	if line := expectSSEMessage(t, reader); !strings.Contains(line, `"op":"subscribe"`) {
		t.Fatalf("expected subscribe event, got %s", line)
	}
	if line := expectSSEMessage(t, reader); !strings.Contains(line, `"payload":[]`) {
		t.Fatalf("expected empty initial query result, got %s", line)
	}

	if _, err := service.Call(context.Background(), "schedule", map[string]any{}); err != nil {
		t.Fatalf("schedule mutation failed: %v", err)
	}
	for {
		line := expectSSEMessage(t, reader)
		if strings.Contains(line, `"op":"ping"`) {
			continue
		}
		if !strings.Contains(line, `"body":"scheduled"`) {
			t.Fatalf("expected scheduled write realtime update, got %s", line)
		}
		break
	}
}
