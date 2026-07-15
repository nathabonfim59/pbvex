package pbvex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
)

const composedCapabilitiesBundle = `
__pbvex.registerFunction({name:"compose",type:"mutation",visibility:"public",modulePath:"caps",exportName:"compose"}, async function(ctx) {
  const user = await ctx.auth.getUserIdentity();
  const id = await ctx.db.insert("notes", {body:"direct"});
  const uploadUrl = await ctx.storage.generateUploadUrl();
  const jobId = await ctx.scheduler.runAfter(0, {_path:"scheduledStorage",_type:"mutation",_visibility:"internal"}, {});
  return {id, uploadUrl, jobId, tokenIdentifier:user&&user.tokenIdentifier, hasAuth:!!ctx.auth, hasDb:!!ctx.db, hasStorage:!!ctx.storage, hasScheduler:!!ctx.scheduler};
});
__pbvex.registerFunction({name:"scheduledStorage",type:"mutation",visibility:"internal",modulePath:"caps",exportName:"scheduledStorage"}, async function(ctx) {
  await ctx.db.insert("notes", {body:"scheduled"});
  const uploadUrl = await ctx.storage.generateUploadUrl();
  return {uploadUrl, hasDb:!!ctx.db, hasStorage:!!ctx.storage, hasScheduler:!!ctx.scheduler};
});
__pbvex.registerFunction({name:"realtimeCaps",type:"query",visibility:"public",modulePath:"caps",exportName:"realtimeCaps"}, async function(ctx) {
  const notes = await ctx.db.query("notes").collect();
  return {count:notes.length, hasDb:!!ctx.db, hasStorage:!!ctx.storage, hasGetUrl:typeof ctx.storage.getUrl === "function"};
});
`

func TestDatabaseSchedulerStorageComposeAcrossScheduledAndRealtimeInvocations(t *testing.T) {
	app, service := newTestApp(t)
	descriptors := []deploy.FunctionDescriptor{
		{Name: "compose", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "caps", ExportName: "compose"},
		{Name: "scheduledStorage", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "caps", ExportName: "scheduledStorage"},
		{Name: "realtimeCaps", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "caps", ExportName: "realtimeCaps"},
	}
	req := storageManifestRequest("composed_capabilities", composedCapabilitiesBundle, descriptors)
	req["manifest"].(map[string]any)["schema"] = map[string]any{"tables": []any{map[string]any{
		"tableName": "notes",
		"fields":    map[string]any{"body": map[string]any{"type": "string"}},
	}}}
	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate: %v", err)
	}

	identity := &auth.UserIdentity{Subject: "user", Issuer: "pocketbase:collection", TokenIdentifier: "pocketbase:collection:user"}
	direct, err := service.Call(context.Background(), "compose", map[string]any{}, identity, "capability-request")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	directMap, ok := direct.(map[string]any)
	if !ok || directMap["hasAuth"] != true || directMap["tokenIdentifier"] != identity.TokenIdentifier || directMap["hasDb"] != true || directMap["hasStorage"] != true || directMap["hasScheduler"] != true {
		t.Fatalf("combined mutation capabilities: %#v", direct)
	}
	if uploadURL, _ := directMap["uploadUrl"].(string); uploadURL == "" {
		t.Fatalf("combined mutation missing upload URL: %#v", direct)
	}
	jobID, _ := directMap["jobId"].(string)
	if jobID == "" {
		t.Fatalf("combined mutation missing job id: %#v", direct)
	}
	waitForDurableJobStatus(t, app, jobID, "completed")
	job, err := app.FindRecordById(schema.CollectionJobs, jobID)
	if err != nil {
		t.Fatal(err)
	}
	var scheduled map[string]any
	if err := job.UnmarshalJSONField(schema.FieldResult, &scheduled); err != nil {
		t.Fatalf("scheduled result: %v", err)
	}
	if scheduled["hasDb"] != true || scheduled["hasStorage"] != true || scheduled["hasScheduler"] != true {
		t.Fatalf("scheduled mutation capabilities: %#v", scheduled)
	}
	if uploadURL, _ := scheduled["uploadUrl"].(string); uploadURL == "" {
		t.Fatalf("scheduled mutation missing upload URL: %#v", scheduled)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()
	realtimeReq := realtimePost(t, server.URL, "realtimeCaps", `{}`)
	realtimeReq.Header.Set("Accept", "text/event-stream")
	res, err := (&http.Client{}).Do(realtimeReq)
	if err != nil {
		t.Fatalf("realtime request: %v", err)
	}
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	_ = sseReadLine(t, reader)
	_ = sseReadLine(t, reader)
	message := sseReadLine(t, reader)
	if !strings.Contains(message, `"hasDb":true`) || !strings.Contains(message, `"hasStorage":true`) || !strings.Contains(message, `"hasGetUrl":true`) || !strings.Contains(message, `"count":2`) {
		t.Fatalf("realtime query capabilities: %s", message)
	}
}

const httpActionCapabilitiesBundle = `
__pbvex.registerFunction({name:"httpCapabilities",type:"httpAction",visibility:"public",modulePath:"httpCaps",exportName:"httpCapabilities",route:{method:"POST",path:"http-capabilities"}}, async function(ctx, request) {
  const input = await request.json();
  if (input.op === "prepare") {
    const uploadUrl = await ctx.storage.generateUploadUrl();
    const jobId = await ctx.scheduler.runAfter(3600000, {_path:"scheduledNoop",_type:"mutation",_visibility:"internal"}, {});
    await ctx.scheduler.cancel(jobId);
    return new Response(JSON.stringify({uploadUrl, jobId, hasAuth:!!ctx.auth, hasDb:!!ctx.db, hasStorage:!!ctx.storage, hasScheduler:!!ctx.scheduler}), {headers:{"content-type":"application/json"}});
  }
  if (input.op === "get") {
    return new Response(JSON.stringify({url:await ctx.storage.getUrl(input.id)}), {headers:{"content-type":"application/json"}});
  }
  if (input.op === "delete") {
    await ctx.storage.delete(input.id);
    return new Response(JSON.stringify({deleted:true}), {headers:{"content-type":"application/json"}});
  }
  return new Response("bad operation", {status:400});
});
__pbvex.registerFunction({name:"scheduledNoop",type:"mutation",visibility:"internal",modulePath:"httpCaps",exportName:"scheduledNoop"}, async function() { return null; });
`

func TestHTTPActionComposesSchedulerAndStorageWithoutDatabase(t *testing.T) {
	app, service := newTestApp(t)
	descriptors := []deploy.FunctionDescriptor{
		{Name: "httpCapabilities", Type: deploy.FunctionTypeHTTPAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "httpCaps", ExportName: "httpCapabilities", Route: &deploy.FunctionRoute{Method: http.MethodPost, Path: "http-capabilities"}},
		{Name: "scheduledNoop", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "httpCaps", ExportName: "scheduledNoop"},
	}
	resp, err := service.Upload(storageManifestRequest("http_action_capabilities", httpActionCapabilitiesBundle, descriptors))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate: %v", err)
	}

	server := startRealtimeServer(t, app, service)
	defer server.Close()
	call := func(body string) map[string]any {
		t.Helper()
		res, err := server.Client().Post(server.URL+"/api/pbvex/http-capabilities", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("http action: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("http action status=%d", res.StatusCode)
		}
		var result map[string]any
		if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
			t.Fatalf("decode http action: %v", err)
		}
		return result
	}

	prepared := call(`{"op":"prepare"}`)
	if prepared["hasAuth"] != true || prepared["hasDb"] != false || prepared["hasStorage"] != true || prepared["hasScheduler"] != true {
		t.Fatalf("http action capabilities: %#v", prepared)
	}
	uploadURL, _ := prepared["uploadUrl"].(string)
	jobID, _ := prepared["jobId"].(string)
	if uploadURL == "" || jobID == "" {
		t.Fatalf("http action results: %#v", prepared)
	}
	waitForDurableJobStatus(t, app, jobID, "canceled")

	uploadPath := uploadURL
	if parsed, parseErr := url.Parse(uploadURL); parseErr == nil && parsed.IsAbs() {
		uploadPath = parsed.RequestURI()
	}
	uploadReq, err := http.NewRequest(http.MethodPost, server.URL+uploadPath, bytes.NewReader([]byte("from http action")))
	if err != nil {
		t.Fatal(err)
	}
	uploadReq.Header.Set("Content-Type", "text/plain")
	uploadRes, err := server.Client().Do(uploadReq)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer uploadRes.Body.Close()
	if uploadRes.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d", uploadRes.StatusCode)
	}
	var uploaded struct {
		StorageID string `json:"storageId"`
	}
	if err := json.NewDecoder(uploadRes.Body).Decode(&uploaded); err != nil || uploaded.StorageID == "" {
		t.Fatalf("upload response id=%q err=%v", uploaded.StorageID, err)
	}

	got := call(`{"op":"get","id":"` + uploaded.StorageID + `"}`)
	if url, _ := got["url"].(string); url == "" {
		t.Fatalf("getUrl result: %#v", got)
	}
	deleted := call(`{"op":"delete","id":"` + uploaded.StorageID + `"}`)
	if deleted["deleted"] != true {
		t.Fatalf("delete result: %#v", deleted)
	}
	if got = call(`{"op":"get","id":"` + uploaded.StorageID + `"}`); got["url"] != nil {
		t.Fatalf("deleted storage still has URL: %#v", got)
	}
}
