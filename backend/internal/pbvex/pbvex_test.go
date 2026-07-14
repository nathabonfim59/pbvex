package pbvex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/realtime"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

const testBundleJS = `__pbvex.registerFunction({name:"hello",type:"query",visibility:"public",modulePath:"hello",exportName:"default"}, function(ctx,args) { return "Hello, " + args.name + "!"; });`

func storageManifestRequest(deploymentID, bundle string, functions []deploy.FunctionDescriptor) map[string]any {
	funcs := make([]any, len(functions))
	for i, fn := range functions {
		item := map[string]any{
			"name":       fn.Name,
			"type":       string(fn.Type),
			"visibility": string(fn.Visibility),
			"modulePath": fn.ModulePath,
			"exportName": fn.ExportName,
		}
		if fn.Route != nil {
			route := map[string]any{
				"method":     fn.Route.Method,
				"path":       fn.Route.Path,
				"pathPrefix": fn.Route.PathPrefix,
			}
			if fn.Route.Path == "" {
				delete(route, "path")
			}
			if fn.Route.PathPrefix == "" {
				delete(route, "pathPrefix")
			}
			item["route"] = route
		}
		funcs[i] = item
	}
	return map[string]any{
		"manifest": map[string]any{
			"protocolVersion": "v1",
			"deploymentId":    deploymentID,
			"functions":       funcs,
		},
		"bundle": testBundle(bundle),
		"sha256": bundleHash(bundle),
		"size":   int64(len(bundle)),
	}
}

func testManifestWithExport(deploymentID, export string) deploy.DeploymentManifest {
	return deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    deploymentID,
		Functions: []deploy.FunctionDescriptor{
			{
				Name:       export,
				Type:       deploy.FunctionTypeQuery,
				Visibility: deploy.FunctionVisibilityPublic,
				ModulePath: export,
				ExportName: "default",
			},
		},
	}
}

func testManifest(deploymentID string) deploy.DeploymentManifest {
	return testManifestWithExport(deploymentID, "hello")
}

func testBundle(bundle string) string {
	return base64.StdEncoding.EncodeToString([]byte(bundle))
}

func bundleHash(bundle string) string {
	h := sha256.Sum256([]byte(bundle))
	return hex.EncodeToString(h[:])
}

func testUploadRequest(deploymentID, bundle string, export ...string) map[string]any {
	exportName := "hello"
	if len(export) > 0 {
		exportName = export[0]
	}
	manifest := testManifestWithExport(deploymentID, exportName)
	b64 := testBundle(bundle)
	h := bundleHash(bundle)
	size := int64(len(bundle))
	return map[string]any{
		"manifest": map[string]any{
			"protocolVersion": manifest.ProtocolVersion,
			"deploymentId":    manifest.DeploymentID,
			"functions": []any{
				map[string]any{
					"name":       manifest.Functions[0].Name,
					"type":       string(manifest.Functions[0].Type),
					"visibility": string(manifest.Functions[0].Visibility),
					"modulePath": manifest.Functions[0].ModulePath,
					"exportName": manifest.Functions[0].ExportName,
				},
			},
		},
		"bundle": b64,
		"sha256": h,
		"size":   size,
	}
}

func newTestApp(t *testing.T) (*tests.TestApp, *deploy.Service) {
	t.Helper()

	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Runtime.PoolSize = 2
	cfg.Runtime.Timeout = 2 * time.Second
	cfg.Deploy.HistoryLimit = 5
	cfg.Storage.MaxFileSize = 1 << 20

	service, _, err := RegisterCore(app, cfg)
	if err != nil {
		app.Cleanup()
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap: %v", err)
	}
	if err := app.RunAllMigrations(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to run migrations: %v", err)
	}

	t.Cleanup(app.Cleanup)

	return app, service
}

func newTestAppWithBroadcaster(t *testing.T, rtCfg realtime.Config) (*tests.TestApp, *deploy.Service, deploy.Invalidator) {
	t.Helper()

	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Runtime.PoolSize = 2
	cfg.Runtime.Timeout = 2 * time.Second
	cfg.Deploy.HistoryLimit = 5
	cfg.Realtime = rtCfg

	service, invalidator, err := RegisterCore(app, cfg)
	if err != nil {
		app.Cleanup()
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap: %v", err)
	}
	if err := app.RunAllMigrations(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to run migrations: %v", err)
	}

	t.Cleanup(app.Cleanup)

	return app, service, invalidator
}

func superuserToken(t *testing.T, app *tests.TestApp) string {
	t.Helper()
	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatalf("failed to find superuser: %v", err)
	}
	token, err := superuser.NewAuthToken()
	if err != nil {
		t.Fatalf("failed to generate superuser token: %v", err)
	}
	return token
}

func TestSchemaBootstrapIdempotent(t *testing.T) {
	app, _ := newTestApp(t)

	for _, name := range []string{"_pbvex_deployments", "_pbvex_functions", "_pbvex_schemaState", "_pbvex_jobs", "_pbvex_components"} {
		if _, err := app.FindCollectionByNameOrId(name); err != nil {
			t.Fatalf("missing reserved collection %q: %v", name, err)
		}
	}

	if _, _, err := RegisterCore(app, DefaultConfig()); err != nil {
		t.Fatalf("second bootstrap failed: %v", err)
	}
}

func TestDeploymentActivationMaterializesDatabaseSchema(t *testing.T) {
	app, service := newTestApp(t)
	bundle := `__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"write",exportName:"default"},function(ctx){return ctx.db.insert("notes",{["body_with'quote"]:"x"})});`
	req := testUploadRequest("database_schema", bundle, "write")
	req["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	req["manifest"].(map[string]any)["schema"] = map[string]any{"tables": []any{map[string]any{"tableName": "notes", "fields": map[string]any{"body_with'quote": map[string]any{"type": "string"}}, "indexes": []any{map[string]any{"name": "by_body", "fields": []any{"body_with'quote"}}}}}}
	resp, err := service.Upload(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.FindCollectionByNameOrId("notes"); err == nil {
		t.Fatal("upload must not create user collection")
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	notes, err := app.FindCollectionByNameOrId("notes")
	if err != nil {
		t.Fatal(err)
	}
	if notes.GetIndex("idx_pbvex_notes_by_body") == "" {
		t.Fatal("declared index was not materialized")
	}
	var plan []struct {
		Detail string `db:"detail"`
	}
	path := schema.SQLiteJSONPathLiteral("body_with'quote")
	key, err := schema.OrderKey(map[string]any{"type": "string"}, "x", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.DB().NewQuery(fmt.Sprintf("EXPLAIN QUERY PLAN SELECT * FROM notes WHERE json_extract(%s, %s) = {:body} ORDER BY json_extract(%s, %s), created, id", schema.DocumentOrderField, path, schema.DocumentOrderField, path)).Bind(map[string]any{"body": key}).All(&plan); err != nil {
		t.Fatal(err)
	}
	used := false
	for _, row := range plan {
		if strings.Contains(row.Detail, "idx_pbvex_notes_by_body") {
			used = true
		}
	}
	if !used {
		t.Fatalf("declared index was not used: %#v", plan)
	}
	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: baseRouter}); err != nil {
		t.Fatal(err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	reqCall := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(`{"name":"write","args":{}}`))
	reqCall.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, reqCall)
	if rr.Code != http.StatusOK {
		t.Fatalf("database public call failed: %d %s", rr.Code, rr.Body.String())
	}
	// User backing tables are not a PocketBase public API. Check after a
	// successful write so a public raw endpoint could not hide behind an empty
	// collection response.
	rawReq := httptest.NewRequest(http.MethodGet, "/api/collections/notes/records", nil)
	rawRR := httptest.NewRecorder()
	mux.ServeHTTP(rawRR, rawReq)
	if rawRR.Code < http.StatusBadRequest {
		t.Fatalf("raw backing collection was publicly readable: %d %s", rawRR.Code, rawRR.Body.String())
	}
	rows, err := app.FindRecordsByFilter("notes", "", "", 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("database API write %#v %v", rows, err)
	}
}

// TestRecursiveSchemaActivationAndValidation uploads and activates a deployment
// whose schema contains a genuinely recursive (named-ref) validator, then
// confirms runtime document validation accepts valid recursive data and rejects
// invalid recursive data through the activated schema's field validator.
func TestRecursiveSchemaActivationAndValidation(t *testing.T) {
	app, service := newTestApp(t)
	bundle := `__pbvex.registerFunction({name:"default",type:"query",visibility:"public",modulePath:"default",exportName:"default"},function(){return null;});`
	req := testUploadRequest("recursive_schema", bundle, "default")
	tree := map[string]any{
		"type": "recursive", "name": "Node",
		"validator": map[string]any{
			"type": "object", "shape": map[string]any{
				"name":     map[string]any{"type": "string"},
				"children": map[string]any{"type": "array", "item": map[string]any{"type": "ref", "name": "Node"}},
			},
		},
	}
	req["manifest"].(map[string]any)["schema"] = map[string]any{"tables": []any{map[string]any{
		"tableName": "trees", "fields": map[string]any{"root": tree},
	}}}

	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload with recursive schema rejected: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activation with recursive schema failed: %v", err)
	}
	if _, err := app.FindCollectionByNameOrId("trees"); err != nil {
		t.Fatalf("recursive schema collection was not materialized: %v", err)
	}

	// Runtime document validation against the activated recursive field.
	fields := req["manifest"].(map[string]any)["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)["fields"].(map[string]any)
	valid := map[string]any{"root": map[string]any{"name": "root", "children": []any{map[string]any{"name": "leaf", "children": []any{}}}}}
	if _, err := schema.NormalizeDocument(fields, valid, false, true, nil); err != nil {
		t.Fatalf("expected valid recursive document to validate: %v", err)
	}
	invalid := map[string]any{"root": map[string]any{"name": "root", "children": []any{map[string]any{"children": []any{}}}}}
	if _, err := schema.NormalizeDocument(fields, invalid, false, true, nil); err == nil {
		t.Fatal("expected invalid recursive document to be rejected")
	}
}

// TestRecursiveDefaultDeployedInsert uploads, activates, and invokes a real
// deployed mutation whose schema contains a recursive type with a defaulted
// field. ctx.db.insert validates the document against the activated recursive
// descriptor, applies the default, and rejects invalid recursive data.
func TestRecursiveDefaultDeployedInsert(t *testing.T) {
	app, service := newTestApp(t)
	bundle := `__pbvex.registerFunction({name:"default",type:"mutation",visibility:"public",modulePath:"default",exportName:"default"},function(ctx,args){return ctx.db.insert("trees",{root:args.tree});});`
	req := testUploadRequest("recursive_default", bundle, "default")
	req["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	req["manifest"].(map[string]any)["schema"] = map[string]any{"tables": []any{map[string]any{
		"tableName": "trees",
		"fields": map[string]any{"root": map[string]any{
			"type": "recursive", "name": "Node",
			"validator": map[string]any{
				"type": "object", "shape": map[string]any{
					"name":     map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "anon"},
					"kind":     map[string]any{"type": "string"},
					"children": map[string]any{"type": "array", "item": map[string]any{"type": "ref", "name": "Node"}},
				},
			},
		}},
	}}}

	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload rejected: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activation failed: %v", err)
	}

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: baseRouter}); err != nil {
		t.Fatal(err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	// Valid insert: a tree whose root omits `name` -> the defaulted field
	// applies "anon" through the recursive descriptor. `kind` is required.
	valid := `{"name":"default","args":{"tree":{"kind":"root","children":[{"kind":"leaf","children":[]}]}}}`
	validReq := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(valid))
	validReq.Header.Set("Content-Type", "application/json")
	validRR := httptest.NewRecorder()
	mux.ServeHTTP(validRR, validReq)
	if validRR.Code != http.StatusOK {
		t.Fatalf("valid recursive insert failed: %d %s", validRR.Code, validRR.Body.String())
	}
	rows, err := app.FindRecordsByFilter("trees", "", "", 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected one tree row: %#v %v", rows, err)
	}
	data, _ := rows[0].Get("_pbvex_data").(types.JSONRaw)
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	root, _ := doc["root"].(map[string]any)
	if root["name"] != "anon" {
		t.Fatalf("expected defaulted recursive name 'anon', got %#v", root)
	}
	// The nested child also omits `name` — the default must apply at every
	// level of the recursive structure, not just the root.
	children, _ := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	child, _ := children[0].(map[string]any)
	if child["name"] != "anon" {
		t.Fatalf("expected nested child defaulted name 'anon', got %#v", child["name"])
	}

	// Invalid insert: root omits the required `kind` -> rejected by the
	// deployed schema validator even though `name` is defaulted.
	invalid := `{"name":"default","args":{"tree":{"children":[{"kind":"leaf","children":[]}]}}}`
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(invalid))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidRR := httptest.NewRecorder()
	mux.ServeHTTP(invalidRR, invalidReq)
	if invalidRR.Code == http.StatusOK {
		t.Fatalf("expected invalid recursive insert to be rejected, got 200: %s", invalidRR.Body.String())
	}
}

// TestRecursiveDefaultDeployedPatch proves a deployed ctx.db.patch applies
// recursive defaults: patching root with a tree that omits the non-optional
// defaulted `name` field results in the default "anon" being applied at every
// nesting level through the recursive descriptor.
func TestRecursiveDefaultDeployedPatch(t *testing.T) {
	app, service := newTestApp(t)
	bundle := `__pbvex.registerFunction({name:"create",type:"mutation",visibility:"public",modulePath:"create",exportName:"default"},function(ctx,args){return ctx.db.insert("trees",{root:args.tree});});
__pbvex.registerFunction({name:"patch",type:"mutation",visibility:"public",modulePath:"patch",exportName:"default"},function(ctx,args){ctx.db.patch(args.id,{root:args.tree});return null;});`
	req := testUploadRequest("recursive_patch", bundle, "create")
	manifest := req["manifest"].(map[string]any)
	manifest["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	manifest["functions"] = append(manifest["functions"].([]any),
		map[string]any{"name": "patch", "type": "mutation", "visibility": "public", "modulePath": "patch", "exportName": "default"},
	)
	manifest["schema"] = map[string]any{"tables": []any{map[string]any{
		"tableName": "trees",
		"fields": map[string]any{"root": map[string]any{
			"type": "recursive", "name": "Node",
			"validator": map[string]any{
				"type": "object", "shape": map[string]any{
					"name":     map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "anon"},
					"kind":     map[string]any{"type": "string"},
					"children": map[string]any{"type": "array", "item": map[string]any{"type": "ref", "name": "Node"}},
				},
			},
		}},
	}}}

	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload rejected: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activation failed: %v", err)
	}

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: baseRouter}); err != nil {
		t.Fatal(err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: insert a full tree (with name) to get a document ID.
	createBody := `{"name":"create","args":{"tree":{"name":"original","kind":"root","children":[]}}}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	mux.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create failed: %d %s", createRR.Code, createRR.Body.String())
	}
	var createResult map[string]any
	if err := json.Unmarshal(createRR.Body.Bytes(), &createResult); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	docID, ok := createResult["result"].(string)
	if !ok || docID == "" {
		t.Fatalf("expected string result (document id), got %#v", createResult["result"])
	}

	// Step 2: patch root with a new tree that omits `name` at both root and
	// child level. The defaulted field must be applied through the recursive
	// descriptor during patch normalization.
	patchBody := `{"name":"patch","args":{"id":"` + docID + `","tree":{"kind":"patched","children":[{"kind":"leaf","children":[]}]}}}`
	patchReq := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchRR := httptest.NewRecorder()
	mux.ServeHTTP(patchRR, patchReq)
	if patchRR.Code != http.StatusOK {
		t.Fatalf("patch failed: %d %s", patchRR.Code, patchRR.Body.String())
	}

	// Step 3: verify the patched document has the default applied at every level.
	rows, err := app.FindRecordsByFilter("trees", "", "", 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected one tree row: %#v %v", rows, err)
	}
	data, _ := rows[0].Get("_pbvex_data").(types.JSONRaw)
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	root, _ := doc["root"].(map[string]any)
	if root["name"] != "anon" {
		t.Fatalf("expected patched root defaulted name 'anon', got %#v", root["name"])
	}
	if root["kind"] != "patched" {
		t.Fatalf("expected patched root kind 'patched', got %#v", root["kind"])
	}
	children, _ := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	child, _ := children[0].(map[string]any)
	if child["name"] != "anon" {
		t.Fatalf("expected patched child defaulted name 'anon', got %#v", child["name"])
	}
	if child["kind"] != "leaf" {
		t.Fatalf("expected patched child kind 'leaf', got %#v", child["kind"])
	}
}

func TestDeployedDatabaseServiceAndPublicAPICapabilities(t *testing.T) {
	app, service := newTestApp(t)
	bundle := `
__pbvex.registerFunction({name:"create",type:"mutation",visibility:"public",modulePath:"create",exportName:"default"},function(ctx){return ctx.db.insert("entries",{title:"first"})});
__pbvex.registerFunction({name:"read",type:"query",visibility:"public",modulePath:"read",exportName:"default"},function(ctx){return ctx.db.query("entries").withIndex("by_title").collect()});
__pbvex.registerFunction({name:"readOnly",type:"query",visibility:"public",modulePath:"readOnly",exportName:"default"},function(ctx){return typeof ctx.db.insert});
__pbvex.registerFunction({name:"act",type:"action",visibility:"public",modulePath:"act",exportName:"default"},function(ctx){return typeof ctx.db});`
	req := testUploadRequest("deployed_database", bundle, "create")
	manifest := req["manifest"].(map[string]any)
	manifest["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	manifest["functions"] = append(manifest["functions"].([]any),
		map[string]any{"name": "read", "type": "query", "visibility": "public", "modulePath": "read", "exportName": "default"},
		map[string]any{"name": "readOnly", "type": "query", "visibility": "public", "modulePath": "readOnly", "exportName": "default"},
		map[string]any{"name": "act", "type": "action", "visibility": "public", "modulePath": "act", "exportName": "default"},
	)
	manifest["schema"] = map[string]any{"tables": []any{map[string]any{
		"tableName": "entries", "fields": map[string]any{"title": map[string]any{"type": "string"}},
		"indexes": []any{map[string]any{"name": "by_title", "fields": []any{"title"}}},
	}}}
	uploaded, err := service.Upload(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(uploaded.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	id, err := service.Call(context.Background(), "create", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := id.(string); !ok {
		t.Fatalf("mutation id %#v", id)
	}
	read, err := service.Call(context.Background(), "read", nil, nil, "")
	if err != nil || len(read.([]any)) != 1 {
		t.Fatalf("deployed read %#v %v", read, err)
	}
	if got, err := service.Call(context.Background(), "readOnly", nil, nil, ""); err != nil || got != "undefined" {
		t.Fatalf("query database capability %#v %v", got, err)
	}
	if got, err := service.Call(context.Background(), "act", nil, nil, ""); err != nil || got != "undefined" {
		t.Fatalf("action database capability %#v %v", got, err)
	}
	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: baseRouter}); err != nil {
		t.Fatal(err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	httpRequest := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(`{"name":"read","args":null}`))
	httpRequest.Header.Set("Content-Type", "application/json")
	httpResponse := httptest.NewRecorder()
	mux.ServeHTTP(httpResponse, httpRequest)
	if httpResponse.Code != http.StatusOK || !strings.Contains(httpResponse.Body.String(), "first") {
		t.Fatalf("deployed API read %d %s", httpResponse.Code, httpResponse.Body.String())
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Call(canceled, "create", nil, nil, ""); err == nil {
		t.Fatal("canceled call entered mutation")
	}
	rows, err := app.FindRecordsByFilter("entries", "", "", 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("canceled call wrote records %#v %v", rows, err)
	}
}

func TestHostileSchemaFieldNamesStayBoundInActivationAndQueries(t *testing.T) {
	app, service := newTestApp(t)
	// Dots are reserved for canonical q.field traversal. Other punctuation is
	// safe because every JSON path is independently quoted and bound.
	fields := []string{"dot_key", `quote"key`, "apost'rophe", "br[acket]", `x'); DROP TABLE _collections; --`}
	bundle := `
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"write",exportName:"default"},function(ctx){return ctx.db.insert("unsafe",{["dot_key"]:"v0",["quote\"key"]:"v1",["apost'rophe"]:"v2",["br[acket]"]:"v3",["x'); DROP TABLE _collections; --"]:"v4"})});
__pbvex.registerFunction({name:"read",type:"query",visibility:"public",modulePath:"read",exportName:"default"},function(ctx){return ctx.db.query("unsafe").withIndex("by4",r=>r.eq("x'); DROP TABLE _collections; --","v4")).filter(r=>r.eq(r.field("apost'rophe"),r.literal("v2"))).collect()});`
	req := testUploadRequest("hostile_fields", bundle, "write")
	manifest := req["manifest"].(map[string]any)
	manifest["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	manifest["functions"] = append(manifest["functions"].([]any), map[string]any{"name": "read", "type": "query", "visibility": "public", "modulePath": "read", "exportName": "default"})
	schemaFields := map[string]any{}
	indexes := make([]any, 0, len(fields))
	for i, field := range fields {
		schemaFields[field] = map[string]any{"type": "string"}
		indexes = append(indexes, map[string]any{"name": fmt.Sprintf("by%d", i), "fields": []any{field}})
	}
	manifest["schema"] = map[string]any{"tables": []any{map[string]any{"tableName": "unsafe", "fields": schemaFields, "indexes": indexes}}}
	uploaded, err := service.Upload(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(uploaded.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Call(context.Background(), "write", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	result, err := service.Call(context.Background(), "read", nil, nil, "")
	if err != nil || len(result.([]any)) != 1 {
		t.Fatalf("bound hostile query %#v %v", result, err)
	}
	for i, field := range fields {
		path := schema.SQLiteJSONPathLiteral(field)
		key, err := schema.OrderKey(map[string]any{"type": "string"}, fmt.Sprintf("v%d", i), true)
		if err != nil {
			t.Fatal(err)
		}
		var plan []struct {
			Detail string `db:"detail"`
		}
		query := fmt.Sprintf("EXPLAIN QUERY PLAN SELECT * FROM unsafe WHERE json_extract(%s, %s) = {:value} ORDER BY json_extract(%s, %s), created, id", schema.DocumentOrderField, path, schema.DocumentOrderField, path)
		if err := app.DB().NewQuery(query).Bind(map[string]any{"value": key}).All(&plan); err != nil {
			t.Fatal(err)
		}
		used := false
		for _, row := range plan {
			used = used || strings.Contains(row.Detail, fmt.Sprintf("idx_pbvex_unsafe_by%d", i))
		}
		if !used {
			t.Fatalf("index for hostile field %q not used: %#v", field, plan)
		}
	}
}

func TestSchemaRejectsAmbiguousDottedDocumentField(t *testing.T) {
	_, service := newTestApp(t)
	req := testUploadRequest("dotted_field", `__pbvex.registerFunction({name:"read",type:"query",visibility:"public",modulePath:"read",exportName:"default"},function(){return null})`, "read")
	req["manifest"].(map[string]any)["schema"] = map[string]any{"tables": []any{map[string]any{
		"tableName": "unsafe", "fields": map[string]any{"profile.name": map[string]any{"type": "string"}},
	}}}
	if _, err := service.Upload(req); err == nil {
		t.Fatal("ambiguous dotted top-level field was accepted")
	}
}

func TestFailedSchemaActivationLeavesActiveDeploymentAndDataUntouched(t *testing.T) {
	app, service := newTestApp(t)
	goodBundle := `__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"write",exportName:"default"},function(ctx){return ctx.db.insert("stable",{name:"kept"})});`
	goodRequest := testUploadRequest("schema_good", goodBundle, "write")
	goodManifest := goodRequest["manifest"].(map[string]any)
	goodManifest["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	goodManifest["schema"] = map[string]any{"tables": []any{map[string]any{"tableName": "stable", "fields": map[string]any{"name": map[string]any{"type": "string"}}}}}
	good, err := service.Upload(goodRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(good.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Call(context.Background(), "write", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	badBundle := `__pbvex.registerFunction({name:"noop",type:"query",visibility:"public",modulePath:"noop",exportName:"default"},function(){return null});`
	badRequest := testUploadRequest("schema_bad", badBundle, "noop")
	badRequest["manifest"].(map[string]any)["schema"] = map[string]any{"tables": []any{map[string]any{
		// Valid at upload time, but incompatible with the existing document.
		"tableName": "stable", "fields": map[string]any{
			"name":     map[string]any{"type": "string"},
			"required": map[string]any{"type": "string"},
		},
	}}}
	bad, err := service.Upload(badRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(bad.DeploymentID, true); err == nil {
		t.Fatal("incompatible schema activated")
	}
	active, err := service.Active()
	if err != nil || active.DeploymentID != good.DeploymentID {
		t.Fatalf("failed activation replaced active deployment %#v %v", active, err)
	}
	rows, err := app.FindRecordsByFilter("stable", "", "", 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("failed activation changed data %#v %v", rows, err)
	}
	stable, err := app.FindCollectionByNameOrId("stable")
	if err != nil || stable == nil {
		t.Fatalf("failed activation corrupted schema %#v %v", stable, err)
	}
}

func TestSchemaBootstrapIncompatibleCollection(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	col := core.NewBaseCollection(schema.CollectionDeployments)
	col.Fields.Add(&core.TextField{Name: "wrong"})
	if err := app.Save(col); err != nil {
		t.Fatalf("failed to create test collection: %v", err)
	}

	if _, _, err := RegisterCore(app, DefaultConfig()); err != nil {
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err == nil {
		t.Fatal("expected bootstrap error for incompatible collection")
	} else if !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("expected incompatible collection error, got: %v", err)
	}
}

func TestDeploymentActivationMaterializesNestedIndexForRuntimePagination(t *testing.T) {
	app, service := newTestApp(t)
	bundle := `
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"write",exportName:"default"},ctx=>{ctx.db.insert("profiles",{profile:{name:"Ada"}});return ctx.db.insert("profiles",{profile:{name:"Bea"}})});
__pbvex.registerFunction({name:"page",type:"query",visibility:"public",modulePath:"page",exportName:"default"},(ctx,args)=>ctx.db.query("profiles").withIndex("by_name").paginate({numItems:1,cursor:args.cursor}));`
	req := testUploadRequest("nested_index", bundle, "write")
	manifest := req["manifest"].(map[string]any)
	manifest["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	manifest["functions"] = append(manifest["functions"].([]any), map[string]any{"name": "page", "type": "query", "visibility": "public", "modulePath": "page", "exportName": "default"})
	manifest["schema"] = map[string]any{"tables": []any{map[string]any{
		"tableName": "profiles",
		"fields":    map[string]any{"profile": map[string]any{"type": "object", "shape": map[string]any{"name": map[string]any{"type": "string"}}}},
		"indexes":   []any{map[string]any{"name": "by_name", "fields": []any{"profile.name"}}},
	}}}
	deployment, err := service.Upload(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(deployment.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	profiles, err := app.FindCollectionByNameOrId("profiles")
	if err != nil || profiles.GetIndex("idx_pbvex_profiles_by_name") == "" {
		t.Fatalf("nested index was not materialized: %#v %v", profiles, err)
	}
	if _, err := service.Call(context.Background(), "write", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	path := schema.SQLiteJSONPathLiteral("profile.name")
	key, err := schema.OrderKey(map[string]any{"type": "string"}, "Ada", true)
	if err != nil {
		t.Fatal(err)
	}
	var plan []struct {
		Detail string `db:"detail"`
	}
	query := fmt.Sprintf("EXPLAIN QUERY PLAN SELECT * FROM profiles WHERE json_extract(%s, %s) = {:name} ORDER BY json_extract(%s, %s), created, id", schema.DocumentOrderField, path, schema.DocumentOrderField, path)
	if err := app.DB().NewQuery(query).Bind(map[string]any{"name": key}).All(&plan); err != nil {
		t.Fatal(err)
	}
	used := false
	for _, row := range plan {
		used = used || strings.Contains(row.Detail, "idx_pbvex_profiles_by_name")
	}
	if !used {
		t.Fatalf("nested index was not used: %#v", plan)
	}
	first, err := service.Call(context.Background(), "page", map[string]any{"cursor": nil}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	firstPage := first.(map[string]any)
	if got := firstPage["page"].([]any)[0].(map[string]any)["profile"].(map[string]any)["name"]; got != "Ada" {
		t.Fatalf("nested page order %#v", firstPage)
	}
	second, err := service.Call(context.Background(), "page", map[string]any{"cursor": firstPage["continueCursor"]}, nil, "")
	if err != nil || second.(map[string]any)["page"].([]any)[0].(map[string]any)["profile"].(map[string]any)["name"] != "Bea" {
		t.Fatalf("nested page continuation %#v, %v", second, err)
	}
}

func TestSchemaPublicAccessProtection(t *testing.T) {
	app, _ := newTestApp(t)

	scenario := tests.ApiScenario{
		Name:                  "public list pbvex deployments is forbidden",
		Method:                http.MethodGet,
		URL:                   "/api/collections/_pbvex_deployments/records",
		ExpectedStatus:        http.StatusForbidden,
		ExpectedContent:       []string{"Only superusers can perform this action."},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
	}
	scenario.Test(t)
}

func TestDeploymentUploadValidation(t *testing.T) {
	_, service := newTestApp(t)

	bundle := testBundleJS
	body := testUploadRequest("upload_test", bundle)
	body["size"] = int64(999)
	if _, err := service.Upload(body); err == nil {
		t.Fatal("expected upload to fail with size mismatch")
	}

	body2 := testUploadRequest("upload_test2", bundle)
	body2["manifest"].(map[string]any)["protocolVersion"] = "v2"
	if _, err := service.Upload(body2); err == nil {
		t.Fatal("expected upload to fail with invalid protocol version")
	}
}

func TestDeploymentUploadActivateRollback(t *testing.T) {
	app, service := newTestApp(t)

	bundle1 := testBundleJS
	resp1, err := service.Upload(testUploadRequest("test1", bundle1))
	if err != nil {
		t.Fatalf("failed to upload first deployment: %v", err)
	}

	bundle2 := strings.ReplaceAll(testBundleJS, "Hello", "Howdy")
	resp2, err := service.Upload(testUploadRequest("test2", bundle2))
	if err != nil {
		t.Fatalf("failed to upload second deployment: %v", err)
	}

	if _, err := service.Activate(resp1.DeploymentID, true); err != nil {
		t.Fatalf("failed to activate first: %v", err)
	}
	if activeID := getActiveID(t, app, service); activeID != resp1.DeploymentID {
		t.Fatalf("expected active %s, got %s", resp1.DeploymentID, activeID)
	}

	if _, err := service.Activate(resp2.DeploymentID, true); err != nil {
		t.Fatalf("failed to activate second: %v", err)
	}
	if activeID := getActiveID(t, app, service); activeID != resp2.DeploymentID {
		t.Fatalf("expected active %s, got %s", resp2.DeploymentID, activeID)
	}

	if _, err := service.Rollback(resp2.DeploymentID); err != nil {
		t.Fatalf("failed to rollback: %v", err)
	}
	if activeID := getActiveID(t, app, service); activeID != resp1.DeploymentID {
		t.Fatalf("expected rollback to %s, got %s", resp1.DeploymentID, activeID)
	}
}

func TestDeploymentRuntimeInvocation(t *testing.T) {
	_, service := newTestApp(t)

	bundle := testBundleJS
	resp, err := service.Upload(testUploadRequest("runtime", bundle))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	result, err := service.Invoke(context.Background(), resp.DeploymentID, "hello", map[string]any{"name": "world"}, nil, "")
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if result != "Hello, world!" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestDeploymentRuntimeTimeout(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"loop",type:"query",visibility:"public",modulePath:"loop",exportName:"default"}, function() { while(true) {} });`
	resp, err := service.Upload(testUploadRequest("timeout", bundle, "loop"))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = service.Invoke(ctx, resp.DeploymentID, "loop", nil, nil, "")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRestartPersistence(t *testing.T) {
	app, service := newTestApp(t)

	bundle := testBundleJS
	resp, err := service.Upload(testUploadRequest("persist", bundle))
	if err != nil {
		t.Fatalf("failed to upload: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("failed to activate: %v", err)
	}

	if err := app.ResetBootstrapState(); err != nil {
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("failed to re-bootstrap: %v", err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	if activeID := getActiveID(t, app, service); activeID != resp.DeploymentID {
		t.Fatalf("expected active %s after restart, got %s", resp.DeploymentID, activeID)
	}

	result, err := service.Invoke(context.Background(), resp.DeploymentID, "hello", map[string]any{"name": "restart"}, nil, "")
	if err != nil {
		t.Fatalf("invoke after restart failed: %v", err)
	}
	if result != "Hello, restart!" {
		t.Fatalf("unexpected result after restart: %v", result)
	}
}

func TestDeploymentAPIIntegration(t *testing.T) {
	app, _ := newTestApp(t)
	token := superuserToken(t, app)

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	if err := app.OnServe().Trigger(serveEvent); err != nil {
		t.Fatalf("failed to trigger serve: %v", err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatalf("failed to build mux: %v", err)
	}

	upload := func(deploymentID, bundle string) string {
		body, _ := json.Marshal(testUploadRequest(deploymentID, bundle))
		req := httptest.NewRequest(http.MethodPost, "/api/pbvex/deployments", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("upload failed: %d %s", rr.Code, rr.Body.String())
		}
		var resp deploy.DeploymentUploadResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal upload response: %v", err)
		}
		return resp.DeploymentID
	}

	activate := func(id string) {
		body := `{"atomic":true}`
		req := httptest.NewRequest(http.MethodPost, "/api/pbvex/deployments/"+id+"/activate", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("activate failed: %d %s", rr.Code, rr.Body.String())
		}
	}

	rollback := func(id string) string {
		req := httptest.NewRequest(http.MethodPost, "/api/pbvex/deployments/"+id+"/rollback", nil)
		req.Header.Set("Authorization", token)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("rollback failed: %d %s", rr.Code, rr.Body.String())
		}
		var rbResp deploy.DeploymentRollbackResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &rbResp); err != nil {
			t.Fatalf("failed to unmarshal rollback response: %v", err)
		}
		if rbResp.RestoredDeploymentID == nil {
			t.Fatal("rollback response missing restoredDeploymentId")
		}
		return *rbResp.RestoredDeploymentID
	}

	id1 := upload("api1", testBundleJS)
	activate(id1)

	bundle2 := strings.ReplaceAll(testBundleJS, "Hello", "Howdy")
	id2 := upload("api2", bundle2)
	activate(id2)

	restored := rollback(id2)
	if restored != id1 {
		t.Fatalf("expected rollback to restore %s, got %s", id1, restored)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/pbvex/deployments", nil)
	req.Header.Set("Authorization", token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list failed: %d %s", rr.Code, rr.Body.String())
	}
	var list deploy.DeploymentListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to unmarshal list response: %v", err)
	}
	if len(list.Deployments) != 2 {
		t.Fatalf("expected 2 deployments, got %d", len(list.Deployments))
	}
}

func TestDeploymentUploadEndpointChecksEnvelopeLimitBeforeBinding(t *testing.T) {
	// The deploy route must override PocketBase's 32 MiB global body limit so a
	// full v1 upload (64 MiB decoded bundle, ~85 MiB base64 + envelope) can be
	// admitted. Anything below the global default would silently narrow ADR 001.
	if deploy.MaxUploadEnvelopeBytes <= 32<<20 {
		t.Fatalf("deploy envelope limit %d does not override PocketBase's 32 MiB global default", deploy.MaxUploadEnvelopeBytes)
	}
	app, _ := newTestApp(t)
	base, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: base}); err != nil {
		t.Fatal(err)
	}
	mux, err := base.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	token := superuserToken(t, app)
	call := func(length int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/pbvex/deployments", strings.NewReader(`{}`))
		r.ContentLength = length // exercise the endpoint gate without allocating a giant client body
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if got := call(deploy.MaxUploadEnvelopeBytes); got.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("exact envelope limit was rejected before binding: %s", got.Body.String())
	}
	if got := call(deploy.MaxUploadEnvelopeBytes + 1); got.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-limit envelope was bound instead of rejected: %d %s", got.Code, got.Body.String())
	}
}

func TestDeployRouteDynamicAdmissionCeiling(t *testing.T) {
	app, service := newTestApp(t)

	// Upload and activate a deployment with a tight maxUploadBytes so the
	// dynamic pre-admission ceiling is well below the global route limit.
	raw := testUploadRequest("tight_cap", testBundleJS)
	raw["manifest"].(map[string]any)["config"] = map[string]any{
		"maxUploadBytes": int64(len(testBundleJS)),
	}
	if _, err := service.Upload(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("tight_cap", true); err != nil {
		t.Fatal(err)
	}

	dynamicLimit := deploy.UploadEnvelopeBytes(service.MaxUploadBytes())
	if dynamicLimit >= deploy.MaxUploadEnvelopeBytes {
		t.Fatalf("dynamic limit %d must be tighter than global %d", dynamicLimit, deploy.MaxUploadEnvelopeBytes)
	}

	base, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: base}); err != nil {
		t.Fatal(err)
	}
	mux, err := base.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	token := superuserToken(t, app)

	// Content-Length path: exact dynamic limit is admitted (not 413).
	callCL := func(contentLength int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/pbvex/deployments", strings.NewReader(`{}`))
		r.ContentLength = contentLength
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if got := callCL(dynamicLimit); got.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("body at exact dynamic limit rejected: %s", got.Body.String())
	}
	// Content-Length path: limit+1 is rejected at pre-admission (413).
	if got := callCL(dynamicLimit + 1); got.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body over dynamic limit not rejected: %d %s", got.Code, got.Body.String())
	}

	// MaxBytesReader path: unknown Content-Length forces actual body read.
	// A valid JSON prefix keeps the decoder reading past the limit so
	// MaxBytesReader truncates the body before it is fully consumed.
	body := `{"p":"` + strings.Repeat("A", int(dynamicLimit)) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/api/pbvex/deployments",
		strings.NewReader(body))
	r.ContentLength = -1
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, r)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body with unknown content-length not rejected: %d %s", rr.Code, rr.Body.String())
	}
}

func TestReservedCollectionProtection(t *testing.T) {
	app, service := newTestApp(t)
	token := superuserToken(t, app)

	bundle := testBundleJS
	resp, err := service.Upload(testUploadRequest("protected", bundle))
	if err != nil {
		t.Fatalf("failed to upload: %v", err)
	}

	record, err := deploy.NewRepo().GetDeployment(context.Background(), app, resp.DeploymentID)
	if err != nil {
		t.Fatalf("failed to find record: %v", err)
	}

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	if err := app.OnServe().Trigger(serveEvent); err != nil {
		t.Fatalf("failed to trigger serve: %v", err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatalf("failed to build mux: %v", err)
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/collections/_pbvex_deployments/records/"+record.Id, strings.NewReader(`{"active":true}`))
	patch.Header.Set("Authorization", token)
	patch.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, patch)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for update, got %d %s", rr.Code, rr.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/collections/_pbvex_deployments/records/"+record.Id, nil)
	del.Header.Set("Authorization", token)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, del)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for delete, got %d %s", rr.Code, rr.Body.String())
	}

	body := `{
		"manifest": {"protocolVersion":"v1","deploymentId":"x","functions":[]},
		"bundle": "",
		"sha256": "0000000000000000000000000000000000000000000000000000000000000000",
		"size": 0
	}`
	create := httptest.NewRequest(http.MethodPost, "/api/collections/_pbvex_deployments/records", strings.NewReader(body))
	create.Header.Set("Authorization", token)
	create.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, create)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for create, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestReservedCollectionInternalContext(t *testing.T) {
	app, service := newTestApp(t)

	bundle := testBundleJS
	resp, err := service.Upload(testUploadRequest("internal", bundle))
	if err != nil {
		t.Fatalf("failed to upload: %v", err)
	}

	record, err := deploy.NewRepo().GetDeployment(context.Background(), app, resp.DeploymentID)
	if err != nil {
		t.Fatalf("failed to find record: %v", err)
	}
	record.Set(schema.FieldActive, true)
	if err := app.Save(record); err == nil {
		t.Fatal("expected direct update without internal context to be blocked")
	}

	ctx := context.WithValue(context.Background(), schema.InternalContextKey, true)
	if err := app.SaveWithContext(ctx, record); err != nil {
		t.Fatalf("expected internal update to succeed: %v", err)
	}

	if err := app.Delete(record); err == nil {
		t.Fatal("expected direct delete without internal context to be blocked")
	}
	if err := app.DeleteWithContext(ctx, record); err != nil {
		t.Fatalf("expected internal delete to succeed: %v", err)
	}
}

func TestBackingCollectionsRejectRawSuperuserCRUD(t *testing.T) {
	app, service := newTestApp(t)
	bundle := `__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"write",exportName:"default"},ctx=>ctx.db.insert("notes",{body:"safe"}));`
	req := testUploadRequest("backing_api", bundle, "write")
	req["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	req["manifest"].(map[string]any)["schema"] = map[string]any{"tables": []any{map[string]any{
		"tableName": "notes", "fields": map[string]any{"body": map[string]any{"type": "string"}},
	}}}
	deployment, err := service.Upload(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(deployment.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Invoke(context.Background(), deployment.DeploymentID, "write", nil, nil, ""); err != nil {
		t.Fatalf("internal ctx.db write failed: %v", err)
	}
	rows, err := app.FindRecordsByFilter("notes", "", "", 2, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("internal record lookup %#v, %v", rows, err)
	}

	base, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: base}); err != nil {
		t.Fatal(err)
	}
	mux, err := base.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	token := superuserToken(t, app)
	call := func(method, path, body string) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("raw %s %s unexpectedly returned %d: %s", method, path, w.Code, w.Body.String())
		}
	}
	rawID := rows[0].Id
	call(http.MethodGet, "/api/collections/notes/records", "")
	call(http.MethodGet, "/api/collections/notes/records/"+rawID, "")
	call(http.MethodPost, "/api/collections/notes/records", `{"_pbvex_data":{"body":"evil"},"_pbvex_order":{"body":"05"}}`)
	call(http.MethodPatch, "/api/collections/notes/records/"+rawID, `{"_pbvex_data":{"body":"evil"}}`)
	call(http.MethodDelete, "/api/collections/notes/records/"+rawID, "")

	rows, err = app.FindRecordsByFilter("notes", "", "", 2, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("raw API mutated backing storage %#v, %v", rows, err)
	}
	stored, _ := json.Marshal(rows[0].Get("_pbvex_data"))
	if !strings.Contains(string(stored), "safe") {
		t.Fatalf("raw API changed backing document %s", stored)
	}
	if _, err := service.Invoke(context.Background(), deployment.DeploymentID, "write", nil, nil, ""); err != nil {
		t.Fatalf("backing protection blocked internal runtime write: %v", err)
	}
}

func TestPublicCallEndpoint(t *testing.T) {
	app, service := newTestApp(t)

	bundle := testBundleJS
	resp, err := service.Upload(testUploadRequest("call", bundle))
	if err != nil {
		t.Fatalf("failed to upload: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("failed to activate: %v", err)
	}

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	if err := app.OnServe().Trigger(serveEvent); err != nil {
		t.Fatalf("failed to trigger serve: %v", err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatalf("failed to build mux: %v", err)
	}

	body := `{"name":"hello","args":{"name":"world"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("call failed: %d %s", rr.Code, rr.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal call response: %v", err)
	}
	if result["result"] != "Hello, world!" {
		t.Fatalf("unexpected result: %v", result["result"])
	}
}

func TestPublicCallBodyAdmissionAllowsExactArgsAndRejectsEnvelopeOverflow(t *testing.T) {
	app, service := newTestApp(t)
	limit := int64(4096)
	bundle := `__pbvex.registerFunction({name:"echo",type:"query",visibility:"public",modulePath:"echo",exportName:"default"},function(ctx,args){return args});`
	resp, err := service.Upload(uploadRequest("call_limit", bundle, functionDescriptor("echo", "query", "public"), map[string]any{"maxFunctionArgsBytes": limit}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}); err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	exact := `"` + strings.Repeat("a", int(limit)-2) + `"`
	body := []byte(`{"name":"echo","args":` + exact + `}`)
	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("exact-limit args rejected: %d %s", rr.Code, rr.Body.String())
	}

	overBody := []byte(`{"name":"echo","args":"` + strings.Repeat("x", int(limit+deploy.MaxEventEnvelopeOverhead)) + `"}`)
	for _, contentLength := range []bool{true, false} {
		req = httptest.NewRequest(http.MethodPost, "/api/pbvex/call", bytes.NewReader(overBody))
		req.Header.Set("Content-Type", "application/json")
		if !contentLength {
			req.ContentLength = -1
		}
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("overflow body contentLength=%v: got %d %s", contentLength, rr.Code, rr.Body.String())
		}
	}
}

func TestActivationAtomicity(t *testing.T) {
	_, service := newTestApp(t)

	goodBundle := testBundleJS
	badBundle := `__pbvex.registerFunction({name:"missing",type:"query",visibility:"public",modulePath:"missing",exportName:"default"}, function() {});`

	goodResp, err := service.Upload(testUploadRequest("atomic_good", goodBundle))
	if err != nil {
		t.Fatalf("failed to upload good: %v", err)
	}
	if _, err := service.Activate(goodResp.DeploymentID, true); err != nil {
		t.Fatalf("failed to activate good: %v", err)
	}

	badManifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "atomic_bad",
		Functions: []deploy.FunctionDescriptor{
			{
				Name:       "notRegistered",
				Type:       deploy.FunctionTypeQuery,
				Visibility: deploy.FunctionVisibilityPublic,
				ModulePath: "notRegistered",
				ExportName: "default",
			},
		},
	}
	badRaw := map[string]any{
		"manifest": map[string]any{
			"protocolVersion": badManifest.ProtocolVersion,
			"deploymentId":    badManifest.DeploymentID,
			"functions": []any{
				map[string]any{
					"name":       badManifest.Functions[0].Name,
					"type":       string(badManifest.Functions[0].Type),
					"visibility": string(badManifest.Functions[0].Visibility),
					"modulePath": badManifest.Functions[0].ModulePath,
					"exportName": badManifest.Functions[0].ExportName,
				},
			},
		},
		"bundle": testBundle(badBundle),
		"sha256": bundleHash(badBundle),
		"size":   int64(len(badBundle)),
	}
	if _, err := service.Upload(badRaw); err == nil {
		t.Fatalf("expected upload to fail with missing function")
	}

	if active, err := service.Active(); err != nil {
		t.Fatalf("failed to get active: %v", err)
	} else if active.DeploymentID != goodResp.DeploymentID {
		t.Fatalf("expected active to remain %s, got %s", goodResp.DeploymentID, active.DeploymentID)
	}
}

func TestFixtureValidation(t *testing.T) {
	validManifests := []string{
		"../../../fixtures/manifests/valid/full.json",
		"../../../fixtures/manifests/valid/minimal.json",
	}
	invalidManifests := []string{
		"../../../fixtures/manifests/invalid/bad-export-name.json",
		"../../../fixtures/manifests/invalid/bad-function-name.json",
		"../../../fixtures/manifests/invalid/bad-function-type.json",
		"../../../fixtures/manifests/invalid/bad-module-path.json",
		"../../../fixtures/manifests/invalid/bad-version.json",
		"../../../fixtures/manifests/invalid/bad-visibility.json",
	}

	for _, path := range validManifests {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read fixture %s: %v", path, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to parse fixture %s: %v", path, err)
		}
		if _, err := deploy.ValidateManifest(raw); err != nil {
			t.Fatalf("expected valid manifest %s: %v", path, err)
		}
	}

	for _, path := range invalidManifests {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read fixture %s: %v", path, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to parse fixture %s: %v", path, err)
		}
		if _, err := deploy.ValidateManifest(raw); err == nil {
			t.Fatalf("expected invalid manifest %s to fail", path)
		}
	}
}

func TestUploadFixtureValidation(t *testing.T) {
	valid, err := os.ReadFile("../../../fixtures/uploads/valid/upload.json")
	if err != nil {
		t.Fatalf("failed to read valid upload fixture: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(valid, &raw); err != nil {
		t.Fatalf("failed to parse valid upload fixture: %v", err)
	}
	if _, _, err := deploy.ValidateUploadRequest(raw); err != nil {
		t.Fatalf("expected valid upload fixture: %v", err)
	}

	invalidFiles := []string{
		"../../../fixtures/uploads/invalid/bad-base64.json",
		"../../../fixtures/uploads/invalid/bad-hash.json",
		"../../../fixtures/uploads/invalid/bad-manifest.json",
		"../../../fixtures/uploads/invalid/bad-size.json",
	}
	for _, path := range invalidFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read fixture %s: %v", path, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to parse fixture %s: %v", path, err)
		}
		if _, _, err := deploy.ValidateUploadRequest(raw); err == nil {
			t.Fatalf("expected invalid upload fixture %s to fail", path)
		}
	}
}

func TestUnicodeBundleSizeAndHash(t *testing.T) {
	bundle := "console.log('🚀');" // 18 bytes in UTF-8, 16 chars
	b64 := base64.StdEncoding.EncodeToString([]byte(bundle))
	h := bundleHash(bundle)
	expectedSize := int64(20) // emoji is 4 bytes + 16 ASCII chars = 20 bytes
	if len(bundle) != int(expectedSize) {
		t.Fatalf("unexpected bundle size: %d", len(bundle))
	}
	size := int64(len(bundle))

	req := map[string]any{
		"manifest": map[string]any{
			"protocolVersion": "v1",
			"deploymentId":    "unicode",
			"functions":       []any{},
		},
		"bundle": b64,
		"sha256": h,
		"size":   size,
	}
	validated, decoded, err := deploy.ValidateUploadRequest(req)
	if err != nil {
		t.Fatalf("expected unicode upload to validate: %v", err)
	}
	if int64(len(decoded)) != size {
		t.Fatalf("decoded size mismatch: %d vs %d", len(decoded), size)
	}
	if validated.Sha256 != h {
		t.Fatalf("hash mismatch")
	}
}

func TestDescriptorMismatch(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"foo",type:"query",visibility:"public",modulePath:"foo",exportName:"default"}, function() {});`
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "mismatch",
		Functions: []deploy.FunctionDescriptor{
			{
				Name:       "bar",
				Type:       deploy.FunctionTypeQuery,
				Visibility: deploy.FunctionVisibilityPublic,
				ModulePath: "bar",
				ExportName: "default",
			},
		},
	}
	raw := map[string]any{
		"manifest": map[string]any{
			"protocolVersion": manifest.ProtocolVersion,
			"deploymentId":    manifest.DeploymentID,
			"functions": []any{
				map[string]any{
					"name":       manifest.Functions[0].Name,
					"type":       string(manifest.Functions[0].Type),
					"visibility": string(manifest.Functions[0].Visibility),
					"modulePath": manifest.Functions[0].ModulePath,
					"exportName": manifest.Functions[0].ExportName,
				},
			},
		},
		"bundle": testBundle(bundle),
		"sha256": bundleHash(bundle),
		"size":   int64(len(bundle)),
	}
	if _, err := service.Upload(raw); err == nil {
		t.Fatal("expected upload to fail with descriptor mismatch")
	}
}

func TestDriftValidation(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()

	col := core.NewBaseCollection(schema.CollectionDeployments)
	col.System = true
	col.Fields.Add(&core.TextField{Name: schema.FieldManifest, Required: true, Max: 1024})
	if err := app.Save(col); err != nil {
		t.Fatalf("failed to create test collection: %v", err)
	}

	if _, _, err := RegisterCore(app, DefaultConfig()); err != nil {
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatalf("failed to reset state: %v", err)
	}
	if err := app.Bootstrap(); err == nil {
		t.Fatal("expected bootstrap error for drifted collection")
	} else if !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("expected incompatible collection error, got: %v", err)
	}
}

func TestCallOnlyPublic(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"internalFn",type:"query",visibility:"internal",modulePath:"internalFn",exportName:"default"}, function() { return "secret"; });`
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "internal_only",
		Functions: []deploy.FunctionDescriptor{
			{
				Name:       "internalFn",
				Type:       deploy.FunctionTypeQuery,
				Visibility: deploy.FunctionVisibilityInternal,
				ModulePath: "internalFn",
				ExportName: "default",
			},
		},
	}
	raw := map[string]any{
		"manifest": map[string]any{
			"protocolVersion": manifest.ProtocolVersion,
			"deploymentId":    manifest.DeploymentID,
			"functions": []any{
				map[string]any{
					"name":       manifest.Functions[0].Name,
					"type":       string(manifest.Functions[0].Type),
					"visibility": string(manifest.Functions[0].Visibility),
					"modulePath": manifest.Functions[0].ModulePath,
					"exportName": manifest.Functions[0].ExportName,
				},
			},
		},
		"bundle": testBundle(bundle),
		"sha256": bundleHash(bundle),
		"size":   int64(len(bundle)),
	}
	resp, err := service.Upload(raw)
	if err != nil {
		t.Fatalf("failed to upload: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("failed to activate: %v", err)
	}

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	if err := app.OnServe().Trigger(serveEvent); err != nil {
		t.Fatalf("failed to trigger serve: %v", err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatalf("failed to build mux: %v", err)
	}

	body := `{"name":"internalFn","args":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for internal call, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestGenericCallRejectsHTTPAction(t *testing.T) {
	_, service := newTestApp(t)
	bundle := `__pbvex.registerFunction({name:"httpOnly",type:"httpAction",visibility:"public",modulePath:"httpOnly",exportName:"default",route:{method:"POST",path:"hook"}}, function(ctx, req) { return new Response(null, {status:200}); });`
	raw := testUploadRequest("http_only", bundle, "httpOnly")
	raw["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["type"] = "httpAction"
	raw["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["route"] = map[string]any{"method": "POST", "path": "hook"}
	resp, err := service.Upload(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Call(context.Background(), "httpOnly", nil, nil, ""); !errors.Is(err, deploy.ErrDeploymentNotFound) {
		t.Fatalf("generic call exposed httpAction: %v", err)
	}
}

func getActiveID(t *testing.T, app *tests.TestApp, service *deploy.Service) string {
	t.Helper()
	active, err := service.Active()
	if err != nil {
		t.Fatalf("failed to get active deployment: %v", err)
	}
	return active.DeploymentID
}

func TestStorageE2E(t *testing.T) {
	app, service := newTestApp(t)

	storageBundle := `__pbvex.registerFunction({name:"generateUploadUrl",type:"mutation",visibility:"public",modulePath:"generateUploadUrl",exportName:"default"}, async function(ctx,args) { return await ctx.storage.generateUploadUrl(); });` +
		`__pbvex.registerFunction({name:"getUrl",type:"query",visibility:"public",modulePath:"getUrl",exportName:"default"}, async function(ctx,args) { return await ctx.storage.getUrl(args.id); });` +
		`__pbvex.registerFunction({name:"deleteFile",type:"mutation",visibility:"public",modulePath:"deleteFile",exportName:"default"}, async function(ctx,args) { return await ctx.storage.delete(args.id); });`

	resp, err := service.Upload(storageManifestRequest("storage_e2e", storageBundle, []deploy.FunctionDescriptor{
		{Name: "generateUploadUrl", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "generateUploadUrl", ExportName: "default"},
		{Name: "getUrl", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "getUrl", ExportName: "default"},
		{Name: "deleteFile", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "deleteFile", ExportName: "default"},
	}))
	if err != nil {
		t.Fatalf("failed to upload storage bundle: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("failed to activate storage deployment: %v", err)
	}

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	if err := app.OnServe().Trigger(serveEvent); err != nil {
		t.Fatalf("failed to trigger serve: %v", err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatalf("failed to build mux: %v", err)
	}

	call := func(name string, args any) map[string]any {
		body := map[string]any{"name": name, "args": args}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("call %s failed: %d %s", name, rr.Code, rr.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to unmarshal call result: %v", err)
		}
		return result
	}

	generateResult := call("generateUploadUrl", map[string]any{})
	uploadURL, ok := generateResult["result"].(string)
	if !ok || uploadURL == "" {
		t.Fatalf("expected upload url from generateUploadUrl, got %v", generateResult["result"])
	}

	fileContent := []byte("hello from pbvex storage")
	uploadReq := httptest.NewRequest(http.MethodPost, uploadURL, bytes.NewReader(fileContent))
	uploadReq.Header.Set("Content-Type", "text/plain")
	uploadReq.Header.Set("X-Upload-Filename", "hello.txt")
	uploadRR := httptest.NewRecorder()
	mux.ServeHTTP(uploadRR, uploadReq)
	if uploadRR.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", uploadRR.Code, uploadRR.Body.String())
	}
	var uploadResp struct {
		StorageID string `json:"storageId"`
	}
	if err := json.Unmarshal(uploadRR.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to unmarshal upload response: %v", err)
	}
	if uploadResp.StorageID == "" {
		t.Fatal("expected storage id in upload response")
	}

	getUrlResult := call("getUrl", map[string]any{"id": uploadResp.StorageID})
	downloadURL, ok := getUrlResult["result"].(string)
	if !ok || downloadURL == "" {
		t.Fatalf("expected download url from getUrl, got %v", getUrlResult["result"])
	}

	downloadReq := httptest.NewRequest(http.MethodGet, downloadURL, nil)
	downloadRR := httptest.NewRecorder()
	mux.ServeHTTP(downloadRR, downloadReq)
	if downloadRR.Code != http.StatusOK {
		t.Fatalf("download failed: %d %s", downloadRR.Code, downloadRR.Body.String())
	}
	if !bytes.Equal(downloadRR.Body.Bytes(), fileContent) {
		t.Fatalf("download body mismatch: %s", downloadRR.Body.String())
	}
	if ct := downloadRR.Header().Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("expected text/plain content type, got %s", ct)
	}
	if downloadRR.Header().Get("Digest") == "" {
		t.Fatal("expected Digest header")
	}

	// HEAD request
	headReq := httptest.NewRequest(http.MethodHead, downloadURL, nil)
	headRR := httptest.NewRecorder()
	mux.ServeHTTP(headRR, headReq)
	if headRR.Code != http.StatusOK {
		t.Fatalf("head failed: %d %s", headRR.Code, headRR.Body.String())
	}
	if headRR.Body.Len() != 0 {
		t.Fatal("expected empty body for HEAD")
	}

	// Range request
	rangeReq := httptest.NewRequest(http.MethodGet, downloadURL, nil)
	rangeReq.Header.Set("Range", "bytes=0-4")
	rangeRR := httptest.NewRecorder()
	mux.ServeHTTP(rangeRR, rangeReq)
	if rangeRR.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 for range, got %d", rangeRR.Code)
	}
	if rangeRR.Body.String() != "hello" {
		t.Fatalf("unexpected range body: %s", rangeRR.Body.String())
	}

	// Delete
	call("deleteFile", map[string]any{"id": uploadResp.StorageID})

	// Missing after delete
	getUrlResult = call("getUrl", map[string]any{"id": uploadResp.StorageID})
	if getUrlResult["result"] != nil {
		t.Fatalf("expected null url for deleted file, got %v", getUrlResult["result"])
	}

	// Missing download
	missingReq := httptest.NewRequest(http.MethodGet, downloadURL, nil)
	missingRR := httptest.NewRecorder()
	mux.ServeHTTP(missingRR, missingReq)
	if missingRR.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for deleted file, got %d", missingRR.Code)
	}

	// Replay upload
	replayReq := httptest.NewRequest(http.MethodPost, uploadURL, bytes.NewReader(fileContent))
	replayReq.Header.Set("Content-Type", "text/plain")
	replayRR := httptest.NewRecorder()
	mux.ServeHTTP(replayRR, replayReq)
	if replayRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for replay, got %d", replayRR.Code)
	}
	var replayErr deploy.StructuredError
	if err := json.Unmarshal(replayRR.Body.Bytes(), &replayErr); err != nil || replayErr.Code != deploy.ErrorCodeUploadConsumed {
		t.Fatalf("expected upload_consumed wire error, got body=%s err=%v", replayRR.Body.String(), err)
	}

	// Tampered token
	tamperedReq := httptest.NewRequest(http.MethodPost, "/api/pbvex/storage/upload/tampered-token", bytes.NewReader(fileContent))
	tamperedReq.Header.Set("Content-Type", "text/plain")
	tamperedRR := httptest.NewRecorder()
	mux.ServeHTTP(tamperedRR, tamperedReq)
	if tamperedRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for tampered token, got %d", tamperedRR.Code)
	}

	// Oversized upload
	oversizedResult := call("generateUploadUrl", map[string]any{})
	oversizedURL, ok := oversizedResult["result"].(string)
	if !ok || oversizedURL == "" {
		t.Fatalf("expected upload url for oversized test, got %v", oversizedResult["result"])
	}
	bigContent := bytes.Repeat([]byte("x"), 2<<20)
	oversizedReq := httptest.NewRequest(http.MethodPost, oversizedURL, bytes.NewReader(bigContent))
	oversizedReq.Header.Set("Content-Type", "text/plain")
	oversizedRR := httptest.NewRecorder()
	mux.ServeHTTP(oversizedRR, oversizedReq)
	if oversizedRR.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized upload, got %d", oversizedRR.Code)
	}
	var oversizedErr deploy.StructuredError
	if err := json.Unmarshal(oversizedRR.Body.Bytes(), &oversizedErr); err != nil || oversizedErr.Code != deploy.ErrorCodeUploadTooLarge {
		t.Fatalf("expected upload_too_large wire error, got body=%s err=%v", oversizedRR.Body.String(), err)
	}

	// Content validation retains its storage-specific wire code.
	invalidContentResult := call("generateUploadUrl", map[string]any{})
	invalidContentURL, ok := invalidContentResult["result"].(string)
	if !ok || invalidContentURL == "" {
		t.Fatalf("expected upload url for invalid content test, got %v", invalidContentResult["result"])
	}
	invalidContentReq := httptest.NewRequest(http.MethodPost, invalidContentURL, bytes.NewReader([]byte("data")))
	invalidContentRR := httptest.NewRecorder()
	mux.ServeHTTP(invalidContentRR, invalidContentReq)
	if invalidContentRR.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for missing content type, got %d", invalidContentRR.Code)
	}
	var invalidContentErr deploy.StructuredError
	if err := json.Unmarshal(invalidContentRR.Body.Bytes(), &invalidContentErr); err != nil || invalidContentErr.Code != deploy.ErrorCodeInvalidContent {
		t.Fatalf("expected invalid_content wire error, got body=%s err=%v", invalidContentRR.Body.String(), err)
	}

	// Missing getUrl returns null
	missingGetResult := call("getUrl", map[string]any{"id": "missing-id"})
	if missingGetResult["result"] != nil {
		t.Fatalf("expected null for missing id, got %v", missingGetResult["result"])
	}
}

// TestStorageE2ENonDefaultBasePath verifies that when a non-default storage
// BasePath is configured, the API registers upload/download routes there and the
// signed URLs the service emits point at those routes (full HTTP round trip).
func TestStorageE2ENonDefaultBasePath(t *testing.T) {
	const basePath = "/pbvex-files"

	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)

	cfg := DefaultConfig()
	cfg.Runtime.PoolSize = 2
	cfg.Runtime.Timeout = 2 * time.Second
	cfg.Deploy.HistoryLimit = 5
	cfg.Storage.MaxFileSize = 1 << 20
	cfg.Storage.BasePath = basePath

	service, _, err := RegisterCore(app, cfg)
	if err != nil {
		t.Fatalf("failed to register core: %v", err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}

	storageBundle := `__pbvex.registerFunction({name:"generateUploadUrl",type:"mutation",visibility:"public",modulePath:"generateUploadUrl",exportName:"default"}, async function(ctx,args) { return await ctx.storage.generateUploadUrl(); });` +
		`__pbvex.registerFunction({name:"getUrl",type:"query",visibility:"public",modulePath:"getUrl",exportName:"default"}, async function(ctx,args) { return await ctx.storage.getUrl(args.id); });`
	resp, err := service.Upload(storageManifestRequest("storage_e2e_basepath", storageBundle, []deploy.FunctionDescriptor{
		{Name: "generateUploadUrl", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "generateUploadUrl", ExportName: "default"},
		{Name: "getUrl", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "getUrl", ExportName: "default"},
	}))
	if err != nil {
		t.Fatalf("failed to upload bundle: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("failed to activate: %v", err)
	}

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	if err := app.OnServe().Trigger(serveEvent); err != nil {
		t.Fatal(err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	call := func(name string, args any) map[string]any {
		body, _ := json.Marshal(map[string]any{"name": name, "args": args})
		req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("call %s failed: %d %s", name, rr.Code, rr.Body.String())
		}
		var result map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &result)
		return result
	}

	uploadURL, _ := call("generateUploadUrl", map[string]any{})["result"].(string)
	if uploadURL == "" {
		t.Fatal("expected upload url")
	}
	if !strings.Contains(uploadURL, basePath+"/upload/") {
		t.Fatalf("upload URL not signed at configured base path %q: %s", basePath, uploadURL)
	}

	fileContent := []byte("non-default base path content")
	upReq := httptest.NewRequest(http.MethodPost, uploadURL, bytes.NewReader(fileContent))
	upReq.Header.Set("Content-Type", "text/plain")
	upReq.Header.Set("X-Upload-Filename", "x.txt")
	upRR := httptest.NewRecorder()
	mux.ServeHTTP(upRR, upReq)
	if upRR.Code != http.StatusOK {
		t.Fatalf("upload at custom base path failed: %d %s", upRR.Code, upRR.Body.String())
	}
	var uploadResp struct {
		StorageID string `json:"storageId"`
	}
	_ = json.Unmarshal(upRR.Body.Bytes(), &uploadResp)
	if uploadResp.StorageID == "" {
		t.Fatal("expected storage id")
	}

	downloadURL, _ := call("getUrl", map[string]any{"id": uploadResp.StorageID})["result"].(string)
	if downloadURL == "" {
		t.Fatal("expected download url")
	}
	if !strings.Contains(downloadURL, basePath+"/"+uploadResp.StorageID) {
		t.Fatalf("download URL not signed at configured base path: %s", downloadURL)
	}

	dlReq := httptest.NewRequest(http.MethodGet, downloadURL, nil)
	dlRR := httptest.NewRecorder()
	mux.ServeHTTP(dlRR, dlReq)
	if dlRR.Code != http.StatusOK {
		t.Fatalf("download at custom base path failed: %d %s", dlRR.Code, dlRR.Body.String())
	}
	if !bytes.Equal(dlRR.Body.Bytes(), fileContent) {
		t.Fatalf("download body mismatch: %q", dlRR.Body.String())
	}

	// The default path must NOT have the route anymore (config drives registration).
	oldPathReq := httptest.NewRequest(http.MethodGet, strings.Replace(downloadURL, basePath, "/api/pbvex/storage", 1), nil)
	oldPathRR := httptest.NewRecorder()
	mux.ServeHTTP(oldPathRR, oldPathReq)
	if oldPathRR.Code != http.StatusNotFound {
		t.Fatalf("expected 404 at the old default base path, got %d", oldPathRR.Code)
	}
}

func TestStorageContextCapabilitiesByFunctionType(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"queryStorage",type:"query",visibility:"public",modulePath:"queryStorage",exportName:"default"}, function(ctx,args) { return typeof ctx.storage; });` +
		`__pbvex.registerFunction({name:"mutationStorage",type:"mutation",visibility:"public",modulePath:"mutationStorage",exportName:"default"}, function(ctx,args) { return typeof ctx.storage; });` +
		`__pbvex.registerFunction({name:"actionPrepare",type:"action",visibility:"public",modulePath:"actionPrepare",exportName:"default"}, async function(ctx,args) {
			return {uploadUrl:await ctx.storage.generateUploadUrl(),db:typeof ctx.db,scheduler:typeof ctx.scheduler,getUrl:typeof ctx.storage.getUrl,generateUploadUrl:typeof ctx.storage.generateUploadUrl,delete:typeof ctx.storage.delete};
		});` +
		`__pbvex.registerFunction({name:"actionDelete",type:"action",visibility:"public",modulePath:"actionDelete",exportName:"default"}, async function(ctx,args) {
			const before=await ctx.storage.getUrl(args.id); await ctx.storage.delete(args.id); const after=await ctx.storage.getUrl(args.id); return {before,after};
		});`

	resp, err := service.Upload(storageManifestRequest("storage_context", bundle, []deploy.FunctionDescriptor{
		{Name: "queryStorage", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "queryStorage", ExportName: "default"},
		{Name: "mutationStorage", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "mutationStorage", ExportName: "default"},
		{Name: "actionPrepare", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "actionPrepare", ExportName: "default"},
		{Name: "actionDelete", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "actionDelete", ExportName: "default"},
	}))
	if err != nil {
		t.Fatalf("failed to upload: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("failed to activate: %v", err)
	}

	baseRouter, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	if err := app.OnServe().Trigger(serveEvent); err != nil {
		t.Fatal(err)
	}
	mux, err := baseRouter.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	call := func(name string, args map[string]any) any {
		body, _ := json.Marshal(map[string]any{"name": name, "args": args})
		req := httptest.NewRequest(http.MethodPost, "/api/pbvex/call", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("call %s failed: %d %s", name, rr.Code, rr.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		return result["result"]
	}

	if s := call("queryStorage", map[string]any{}); s != "object" {
		t.Fatalf("expected storage object in query, got %s", s)
	}
	if s := call("mutationStorage", map[string]any{}); s != "object" {
		t.Fatalf("expected storage object in mutation, got %s", s)
	}

	prepared, ok := call("actionPrepare", map[string]any{}).(map[string]any)
	if !ok {
		t.Fatalf("expected action capability result object")
	}
	for _, method := range []string{"getUrl", "generateUploadUrl", "delete"} {
		if prepared[method] != "function" {
			t.Fatalf("action storage method %s: %#v", method, prepared)
		}
	}
	if prepared["db"] != "undefined" || prepared["scheduler"] != "object" {
		t.Fatalf("action capability boundaries: %#v", prepared)
	}
	uploadURL, _ := prepared["uploadUrl"].(string)
	if uploadURL == "" {
		t.Fatalf("action did not generate upload URL: %#v", prepared)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, uploadURL, strings.NewReader("action storage"))
	uploadReq.Header.Set("Content-Type", "text/plain")
	uploadReq.Header.Set("X-Upload-Filename", "action.txt")
	uploadRR := httptest.NewRecorder()
	mux.ServeHTTP(uploadRR, uploadReq)
	if uploadRR.Code != http.StatusOK {
		t.Fatalf("action-generated upload failed: %d %s", uploadRR.Code, uploadRR.Body.String())
	}
	var uploadResult struct {
		StorageID string `json:"storageId"`
	}
	if err := json.Unmarshal(uploadRR.Body.Bytes(), &uploadResult); err != nil || uploadResult.StorageID == "" {
		t.Fatalf("invalid upload response: %v %s", err, uploadRR.Body.String())
	}

	deleted, ok := call("actionDelete", map[string]any{"id": uploadResult.StorageID}).(map[string]any)
	if !ok {
		t.Fatalf("expected action delete result object")
	}
	if before, _ := deleted["before"].(string); before == "" || deleted["after"] != nil {
		t.Fatalf("action storage get/delete lifecycle: %#v", deleted)
	}
}
