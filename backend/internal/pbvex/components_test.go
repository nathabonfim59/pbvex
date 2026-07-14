package pbvex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
)

// uploadComponentDeployment uploads a deployment built from a typed manifest and
// bundle, returning the deployment id. It marshals through JSON so the upload
// path exercises the same validator as real clients. When the manifest declares
// components, matching module sources are generated (empty content hashing to
// emptyHash) so module-hash authentication passes.
func uploadComponentDeployment(t *testing.T, service *deploy.Service, manifest deploy.DeploymentManifest, bundle string) string {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var manifestMap map[string]any
	if err := json.Unmarshal(raw, &manifestMap); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	req := map[string]any{
		"manifest": manifestMap,
		"bundle":   testBundle(bundle),
		"sha256":   bundleHash(bundle),
		"size":     int64(len(bundle)),
	}
	if mods := modulesForManifest(manifest); mods != nil {
		req["modules"] = mods
	}
	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	return resp.DeploymentID
}

// modulesForManifest synthesizes uploaded module sources for every component
// mount/modulePath. Module bytes are empty (hashing to emptyHash) to match the
// fixtures' declared moduleHashes.
func modulesForManifest(manifest deploy.DeploymentManifest) []any {
	if manifest.Components == nil {
		return nil
	}
	defs := map[string]deploy.ComponentDefinition{}
	for _, d := range manifest.Components.Definitions {
		defs[d.ComponentID] = d
	}
	var out []any
	seen := map[string]bool{}
	var walk func(m deploy.ComponentMount, parent string)
	walk = func(m deploy.ComponentMount, parent string) {
		path := m.MountPath(parent)
		if def, ok := defs[m.ComponentID]; ok {
			for _, rel := range def.ModulePaths {
				full := "pbvex/components/" + path + "/" + rel
				if seen[full] {
					continue
				}
				seen[full] = true
				out = append(out, map[string]any{"path": full, "bytes": ""})
			}
		}
		for _, child := range m.Children {
			walk(child, path)
		}
	}
	for _, m := range manifest.Components.Mounts {
		walk(m, "")
	}
	return out
}

func emptyHash() string {
	return "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
}

// componentID returns the canonical content-addressed componentId for a
// definition, so test manifests satisfy integrity authentication. bundleSha
// must match the upload's sha256 field (the verified bundle hash).
func componentID(def deploy.ComponentDefinition, bundleSha string) string {
	return deploy.ComputeComponentID(def, bundleSha)
}

func toInt64(value any) int64 {
	switch number := value.(type) {
	case int64:
		return number
	case int:
		return int64(number)
	case float64:
		return int64(number)
	case float32:
		return int64(number)
	default:
		return 0
	}
}

func componentCatalogCollectionsForTest(t *testing.T, app core.App, namespace string) map[string]string {
	t.Helper()
	records, err := app.FindAllRecords(schema.CollectionComponents)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.GetString(schema.FieldName) != namespace {
			continue
		}
		var metadata struct {
			Collections map[string]string `json:"collections"`
		}
		encoded, err := json.Marshal(record.Get(schema.FieldMetadata))
		if err != nil || json.Unmarshal(encoded, &metadata) != nil {
			t.Fatal("invalid component catalog metadata")
		}
		return metadata.Collections
	}
	t.Fatal("component catalog record not found")
	return nil
}

// TestComponentSchemalessIsolation verifies that a component without a schema
// declaration cannot access any table and does not inherit root tables.
func TestComponentSchemalessIsolation(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"schemaless_add",type:"mutation",visibility:"public",modulePath:"pbvex/components/plain/ops.ts",exportName:"add"}, async function(ctx,args) {
  await ctx.db.insert("messages", {value: args.value});
  return {ok: true};
});
})();`

	plainDef := deploy.ComponentDefinition{
		ComponentID:  "",
		ModulePaths:  []string{"ops.ts"},
		ModuleHashes: map[string]string{"ops.ts": emptyHash()},
	}
	plainDef.ComponentID = componentID(plainDef, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "schemaless_iso",
		Functions: []deploy.FunctionDescriptor{{
			Name: "schemaless_add", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/plain/ops.ts", ExportName: "add",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{plainDef},
			Mounts:      []deploy.ComponentMount{{Name: "plain", ComponentID: plainDef.ComponentID}},
		},
		Schema: map[string]any{"tables": []any{
			map[string]any{"tableName": "messages", "fields": map[string]any{"value": map[string]any{"type": "number"}}},
		}},
	}

	id := uploadComponentDeployment(t, service, manifest, bundle)
	storedBefore, err := service.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	manifestBefore, _ := json.Marshal(storedBefore.Manifest)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	storedAfter, err := service.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	manifestAfter, _ := json.Marshal(storedAfter.Manifest)
	if string(manifestBefore) != string(manifestAfter) || storedAfter.DeploymentID != storedBefore.DeploymentID {
		t.Fatal("activation mutated content-addressed deployment manifest or id")
	}
	_, err = service.Invoke(context.Background(), id, "schemaless_add", map[string]any{"value": 1})
	if err == nil {
		t.Fatal("expected schemaless component insert to be rejected")
	}
	if !strings.Contains(err.Error(), "not in the component schema") && !strings.Contains(err.Error(), "schema") && !strings.Contains(err.Error(), "invalid table") {
		t.Fatalf("expected schema rejection error, got %v", err)
	}
}

// TestComponentRepeatedMountAndAliasIsolation verifies that the same component
// mounted under multiple names keeps fully isolated namespaces.
func TestComponentRepeatedMountAndAliasIsolation(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"drive",type:"action",visibility:"public",modulePath:"pbvex/drive.ts",exportName:"default"}, async function(ctx,args) {
  await ctx.run("components.a.store.add", {v: args.a});
  await ctx.run("components.b.store.add", {v: args.b});
  return {a: await ctx.run("components.a.store.count", {}), b: await ctx.run("components.b.store.count", {})};
});
__pbvex.registerFunction({name:"add",type:"mutation",visibility:"public",modulePath:"pbvex/components/a/store.ts",exportName:"add"}, async function(ctx,args) { return ctx.db.insert("items",{v: args.v}); });
__pbvex.registerFunction({name:"b_add",type:"mutation",visibility:"public",modulePath:"pbvex/components/b/store.ts",exportName:"add"}, async function(ctx,args) { return ctx.db.insert("items",{v: args.v}); });
__pbvex.registerFunction({name:"a_count",type:"query",visibility:"public",modulePath:"pbvex/components/a/store.ts",exportName:"count"}, async function(ctx) { return ctx.db.query("items").collect().length; });
__pbvex.registerFunction({name:"b_count",type:"query",visibility:"public",modulePath:"pbvex/components/b/store.ts",exportName:"count"}, async function(ctx) { return ctx.db.query("items").collect().length; });
__pbvex.registerFunction({name:"b_get",type:"query",visibility:"public",modulePath:"pbvex/components/b/store.ts",exportName:"get"}, async function(ctx,args) { return ctx.db.get(args.id); });
})();`

	schema := deploy.JSONValue(map[string]any{"tables": []any{
		map[string]any{"tableName": "items", "fields": map[string]any{"v": map[string]any{"type": "number"}}},
	}})
	storeDef := deploy.ComponentDefinition{ComponentID: "", ModulePaths: []string{"store.ts"}, ModuleHashes: map[string]string{"store.ts": emptyHash()}, Schema: schema}
	storeDef.ComponentID = componentID(storeDef, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "alias_repeat",
		Functions: []deploy.FunctionDescriptor{
			{Name: "drive", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/drive.ts", ExportName: "default"},
			{Name: "add", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/a/store.ts", ExportName: "add"},
			{Name: "b_add", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/b/store.ts", ExportName: "add"},
			{Name: "a_count", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/a/store.ts", ExportName: "count"},
			{Name: "b_count", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/b/store.ts", ExportName: "count"},
			{Name: "b_get", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/b/store.ts", ExportName: "get"},
		},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{storeDef},
			Mounts: []deploy.ComponentMount{
				{Name: "a", ComponentID: storeDef.ComponentID},
				{Name: "b", ComponentID: storeDef.ComponentID},
			},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	aIDRaw, err := service.Invoke(context.Background(), id, "add", map[string]any{"v": 1})
	if err != nil {
		t.Fatalf("a add: %v", err)
	}
	aID, ok := aIDRaw.(string)
	if !ok || !strings.HasPrefix(aID, "pbv2.") {
		t.Fatalf("component insert returned non-v2 id: %#v", aIDRaw)
	}
	if _, err := service.Invoke(context.Background(), id, "add", map[string]any{"v": 2}); err != nil {
		t.Fatalf("a add2: %v", err)
	}
	r1, err := service.Invoke(context.Background(), id, "a_count", map[string]any{})
	if err != nil {
		t.Fatalf("a count: %v", err)
	}
	if toInt64(r1) != 2 {
		t.Fatalf("a_count = %v, want 2", r1)
	}
	r2, err := service.Invoke(context.Background(), id, "b_count", map[string]any{})
	if err != nil {
		t.Fatalf("b count: %v", err)
	}
	if toInt64(r2) != 0 {
		t.Fatalf("b_count = %v, want 0 (isolated)", r2)
	}
	if _, err := service.Invoke(context.Background(), id, "b_get", map[string]any{"id": aID}); err == nil {
		t.Fatal("mount b accepted an id owned by mount a")
	}
	driven, err := service.Invoke(context.Background(), id, "drive", map[string]any{"a": 3, "b": 4})
	if err != nil {
		t.Fatalf("root action drive: %v", err)
	}
	counts, ok := driven.(map[string]any)
	if !ok || toInt64(counts["a"]) != 3 || toInt64(counts["b"]) != 1 {
		t.Fatalf("root action resolved wrong mount namespaces: %#v", driven)
	}
}

// TestComponentNestedMountIsolation verifies parent/child mount namespaces.
func TestComponentNestedMountIsolation(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"put",type:"mutation",visibility:"public",modulePath:"pbvex/components/outer/bin.ts",exportName:"put"}, async function(ctx,args){ await ctx.db.insert("bin",{n: args.n}); return {ok:true}; });
__pbvex.registerFunction({name:"inner_put",type:"mutation",visibility:"public",modulePath:"pbvex/components/outer/inner/bin.ts",exportName:"put"}, async function(ctx,args){ await ctx.db.insert("bin",{n: args.n}); return {ok:true}; });
__pbvex.registerFunction({name:"count",type:"query",visibility:"public",modulePath:"pbvex/components/outer/bin.ts",exportName:"count"}, async function(ctx){ return (await ctx.db.query("bin").collect()).length; });
__pbvex.registerFunction({name:"inner_count",type:"query",visibility:"public",modulePath:"pbvex/components/outer/inner/bin.ts",exportName:"count"}, async function(ctx){ return (await ctx.db.query("bin").collect()).length; });
})();`

	schema := deploy.JSONValue(map[string]any{"tables": []any{
		map[string]any{"tableName": "bin", "fields": map[string]any{"n": map[string]any{"type": "number"}}},
	}})
	binDef := deploy.ComponentDefinition{ComponentID: "", ModulePaths: []string{"bin.ts"}, ModuleHashes: map[string]string{"bin.ts": emptyHash()}, Schema: schema}
	binDef.ComponentID = componentID(binDef, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "nested_mount",
		Functions: []deploy.FunctionDescriptor{
			{Name: "put", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/outer/bin.ts", ExportName: "put"},
			{Name: "inner_put", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/outer/inner/bin.ts", ExportName: "put"},
			{Name: "count", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/outer/bin.ts", ExportName: "count"},
			{Name: "inner_count", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/outer/inner/bin.ts", ExportName: "count"},
		},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{binDef},
			Mounts: []deploy.ComponentMount{{
				Name: "outer", ComponentID: binDef.ComponentID,
				Children: []deploy.ComponentMount{{Name: "inner", ComponentID: binDef.ComponentID}},
			}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := service.Invoke(context.Background(), id, "put", map[string]any{"n": 1}); err != nil {
		t.Fatalf("outer put: %v", err)
	}
	if _, err := service.Invoke(context.Background(), id, "inner_put", map[string]any{"n": 1}); err != nil {
		t.Fatalf("inner put: %v", err)
	}
	if _, err := service.Invoke(context.Background(), id, "inner_put", map[string]any{"n": 2}); err != nil {
		t.Fatalf("inner put2: %v", err)
	}
	outer, _ := service.Invoke(context.Background(), id, "count", map[string]any{})
	inner, _ := service.Invoke(context.Background(), id, "inner_count", map[string]any{})
	if toInt64(outer) != 1 {
		t.Fatalf("outer count = %v, want 1", outer)
	}
	if toInt64(inner) != 2 {
		t.Fatalf("inner count = %v, want 2", inner)
	}
}

// TestAsyncNestedRejectionDoesNotCorruptState verifies that a rejected nested
// call does not corrupt invocation/mount state for subsequent nested calls
// (per-call binding is safe for sibling overlap).
func TestAsyncNestedRejectionDoesNotCorruptState(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"drive",type:"action",visibility:"public",modulePath:"pbvex/drive.ts",exportName:"default"}, async function(ctx) {
  var ok = 0;
  for (var i = 0; i < 5; i++) { try { await ctx.run("bad.default", {}); } catch (e) {} }
  for (var j = 0; j < 5; j++) { await ctx.run("good.default", {}); ok++; }
  return ok;
});
__pbvex.registerFunction({name:"good",type:"query",visibility:"internal",modulePath:"pbvex/good.ts",exportName:"default"}, async function() { return 1; });
__pbvex.registerFunction({name:"bad",type:"query",visibility:"internal",modulePath:"pbvex/bad.ts",exportName:"default"}, async function() { throw new Error("boom"); });
})();`

	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "async_reject",
		Functions: []deploy.FunctionDescriptor{
			{Name: "drive", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/drive.ts", ExportName: "default"},
			{Name: "good", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/good.ts", ExportName: "default"},
			{Name: "bad", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/bad.ts", ExportName: "default"},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "drive", map[string]any{})
	if err != nil {
		t.Fatalf("drive failed: %v", err)
	}
	// Rejections must not corrupt state; all 5 good calls still succeed.
	if toInt64(res) != 5 {
		t.Fatalf("expected 5 successful nested calls, got %v", res)
	}
}

// TestCumulativeWorkBudgetSequential verifies the work budget is cumulative per
// top-level request and is never refunded: more than maxTotalWork (64) sequential
// nested calls must be rejected.
func TestCumulativeWorkBudgetSequential(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"drive",type:"action",visibility:"public",modulePath:"pbvex/drive.ts",exportName:"default"}, async function(ctx) {
  for (var i = 0; i < 100; i++) { await ctx.run("good.default", {}); }
  return i;
});
__pbvex.registerFunction({name:"good",type:"query",visibility:"internal",modulePath:"pbvex/good.ts",exportName:"default"}, async function() { return 1; });
})();`

	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "budget_seq",
		Functions: []deploy.FunctionDescriptor{
			{Name: "drive", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/drive.ts", ExportName: "default"},
			{Name: "good", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/good.ts", ExportName: "default"},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	_, err := service.Invoke(context.Background(), id, "drive", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "work budget") {
		t.Fatalf("expected cumulative work budget error, got %v", err)
	}
}

// TestCumulativeWorkBudgetConcurrent verifies the budget also bounds concurrent
// (Promise.all) nested calls: a single request issuing >64 overlapping calls
// must be rejected.
func TestCumulativeWorkBudgetConcurrent(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"drive",type:"action",visibility:"public",modulePath:"pbvex/drive.ts",exportName:"default"}, async function(ctx) {
  var refs = [];
  for (var i = 0; i < 100; i++) { refs.push(ctx.run("good.default", {})); }
  await Promise.all(refs);
  return 100;
});
__pbvex.registerFunction({name:"good",type:"query",visibility:"internal",modulePath:"pbvex/good.ts",exportName:"default"}, async function() { return 1; });
})();`

	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "budget_conc",
		Functions: []deploy.FunctionDescriptor{
			{Name: "drive", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/drive.ts", ExportName: "default"},
			{Name: "good", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/good.ts", ExportName: "default"},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	_, err := service.Invoke(context.Background(), id, "drive", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "work budget") {
		t.Fatalf("expected cumulative work budget error for concurrent calls, got %v", err)
	}
}

// TestAsyncNeverSettlingRejectsSettlement verifies the runtime pumps the event
// loop until the request timeout for a pending promise, rather than failing
// immediately with "did not settle".
func TestAsyncNeverSettlingRejectsSettlement(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"hang",type:"query",visibility:"public",modulePath:"hang",exportName:"default"}, function() { return new Promise(function(){}); });`
	// Short request timeout so the test does not wait the full default.
	shortCfg := deploy.DefaultDeploymentConfig
	shortCfg.DefaultRequestTimeoutMs = 300
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "hang_settle",
		Config:          &shortCfg,
		Functions: []deploy.FunctionDescriptor{{
			Name: "hang", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "hang", ExportName: "default",
		}},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	start := time.Now()
	_, err := service.Invoke(context.Background(), id, "hang", map[string]any{})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected unsettled promise to be rejected")
	}
	// Must wait for the timeout, not fail immediately.
	if elapsed < 250*time.Millisecond {
		t.Fatalf("expected to wait until timeout, failed after %v", elapsed)
	}
}

// TestComponentDataIdentityAcrossUpgradeAndRollback verifies that component
// data keeps its identity (stable mount id) across a component upgrade and a
// subsequent rollback.
func TestComponentDataIdentityAcrossUpgradeAndRollback(t *testing.T) {
	app, service := newTestApp(t)

	bundle := func(suffix string) string {
		return `(function(){
__pbvex.registerFunction({name:"add",type:"mutation",visibility:"public",modulePath:"pbvex/components/counter/store.ts",exportName:"add"}, async function(ctx,args){ await ctx.db.insert("nums",{v: args.v}); return {ok:true}; });
__pbvex.registerFunction({name:"count",type:"query",visibility:"public",modulePath:"pbvex/components/counter/store.ts",exportName:"count"}, async function(ctx){ return (await ctx.db.query("nums").withIndex("by_v").collect()).length; });
__pbvex.registerFunction({name:"version",type:"query",visibility:"public",modulePath:"pbvex/components/counter/store.ts",exportName:"version"}, async function(){ return "` + suffix + `"; });
})();`
	}
	mkManifest := func(id string, def deploy.ComponentDefinition) deploy.DeploymentManifest {
		return deploy.DeploymentManifest{
			ProtocolVersion: "v1",
			DeploymentID:    id,
			Functions: []deploy.FunctionDescriptor{
				{Name: "add", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/counter/store.ts", ExportName: "add"},
				{Name: "count", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/counter/store.ts", ExportName: "count"},
				{Name: "version", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/counter/store.ts", ExportName: "version"},
			},
			Components: &deploy.ComponentGraph{
				Definitions: []deploy.ComponentDefinition{def},
				Mounts:      []deploy.ComponentMount{{Name: "counter", ComponentID: def.ComponentID}},
			},
		}
	}

	// Two content-distinct components (different schemas) so their def_ ids
	// differ, exercising stable mount identity across a real component upgrade.
	v1Schema := deploy.JSONValue(map[string]any{"tables": []any{
		map[string]any{"tableName": "nums", "fields": map[string]any{"v": map[string]any{"type": "number"}}, "indexes": []any{map[string]any{"name": "by_v", "fields": []any{"v"}}}},
	}})
	v2Schema := deploy.JSONValue(map[string]any{"tables": []any{
		map[string]any{"tableName": "nums", "fields": map[string]any{"v": map[string]any{"type": "number"}, "tag": map[string]any{"type": "optional", "validator": map[string]any{"type": "string"}}}, "indexes": []any{
			map[string]any{"name": "by_v", "fields": []any{"v"}}, map[string]any{"name": "by_tag", "fields": []any{"tag"}},
		}},
	}})
	v1Def := deploy.ComponentDefinition{ModulePaths: []string{"store.ts"}, ModuleHashes: map[string]string{"store.ts": emptyHash()}, Schema: v1Schema}
	v1Def.ComponentID = componentID(v1Def, bundleHash(bundle("v1")))
	v2Def := deploy.ComponentDefinition{ModulePaths: []string{"store.ts"}, ModuleHashes: map[string]string{"store.ts": emptyHash()}, Schema: v2Schema}
	v2Def.ComponentID = componentID(v2Def, bundleHash(bundle("v2")))
	if v1Def.ComponentID == v2Def.ComponentID {
		t.Fatal("test setup: v1 and v2 component ids must differ")
	}
	v1 := uploadComponentDeployment(t, service, mkManifest("identity_v1", v1Def), bundle("v1"))
	if _, err := service.Activate(v1, true); err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	if _, err := service.Invoke(context.Background(), v1, "add", map[string]any{"v": 1}); err != nil {
		t.Fatalf("v1 add: %v", err)
	}
	if _, err := service.Invoke(context.Background(), v1, "add", map[string]any{"v": 2}); err != nil {
		t.Fatalf("v1 add2: %v", err)
	}

	// Upgrade to v2 with a different component id (content change). Data must
	// persist because the mount identity is path-derived.
	v2 := uploadComponentDeployment(t, service, mkManifest("identity_v2", v2Def), bundle("v2"))
	if _, err := service.Activate(v2, true); err != nil {
		t.Fatalf("activate v2: %v", err)
	}
	c2, _ := service.Invoke(context.Background(), v2, "count", map[string]any{})
	if toInt64(c2) != 2 {
		t.Fatalf("after upgrade count = %v, want 2 (data identity lost)", c2)
	}
	namespaces, err := deploy.ComponentNamespaces(mkManifest("catalog", v2Def).Components)
	if err != nil {
		t.Fatal(err)
	}
	physical := namespaces["counter"].PhysicalByTable["nums"]
	collection, err := app.FindCollectionByNameOrId(physical)
	if err != nil || collection.GetIndex("idx_pbvex_"+physical+"_by_tag") == "" {
		t.Fatalf("v2 component index not materialized: %v", err)
	}

	// Rollback to v1; data must still be present.
	if _, err := service.Rollback(v2); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if getActiveID(t, app, service) != v1 {
		t.Fatal("rollback did not restore v1")
	}
	c1, _ := service.Invoke(context.Background(), v1, "count", map[string]any{})
	if toInt64(c1) != 2 {
		t.Fatalf("after rollback count = %v, want 2", c1)
	}
	collection, err = app.FindCollectionByNameOrId(physical)
	if err != nil || collection.GetIndex("idx_pbvex_"+physical+"_by_tag") != "" {
		t.Fatalf("rollback did not restore v1 index visibility: %v", err)
	}

	// Removing the mount leaves its physical namespace dormant rather than
	// deleting data. Remounting the same path restores it.
	emptyBundle := `(function(){})();`
	removedManifest := deploy.DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "identity_removed", Functions: []deploy.FunctionDescriptor{}}
	removed := uploadComponentDeployment(t, service, removedManifest, emptyBundle)
	if _, err := service.Activate(removed, true); err != nil {
		t.Fatalf("activate removed mount: %v", err)
	}
	rows, err := app.FindAllRecords(physical)
	if err != nil || len(rows) != 2 {
		t.Fatalf("removed mount did not remain dormant: rows=%d err=%v", len(rows), err)
	}
	service = deploy.NewService(app, deploy.NewRepo(), runtime.NewManager(runtime.DefaultConfig()), deploy.DefaultConfig())
	remounted := uploadComponentDeployment(t, service, mkManifest("identity_remounted", v2Def), bundle("v2"))
	if _, err := service.Activate(remounted, true); err != nil {
		t.Fatalf("remount same path: %v", err)
	}
	if count, err := service.Invoke(context.Background(), remounted, "count", map[string]any{}); err != nil || toInt64(count) != 2 {
		t.Fatalf("same-path remount did not restore rows: count=%v err=%v", count, err)
	}

	// A schema that makes a new field required cannot migrate the existing
	// rows. The failed activation must leave active state, rows and indexes
	// untouched.
	badSchema := deploy.JSONValue(map[string]any{"tables": []any{map[string]any{
		"tableName": "nums", "fields": map[string]any{"v": map[string]any{"type": "number"}, "required": map[string]any{"type": "string"}},
		"indexes": []any{map[string]any{"name": "by_v", "fields": []any{"v"}}},
	}}})
	badDef := deploy.ComponentDefinition{ModulePaths: []string{"store.ts"}, ModuleHashes: map[string]string{"store.ts": emptyHash()}, Schema: badSchema}
	badDef.ComponentID = componentID(badDef, bundleHash(bundle("bad")))
	bad := uploadComponentDeployment(t, service, mkManifest("identity_bad", badDef), bundle("bad"))
	if _, err := service.Activate(bad, true); err == nil {
		t.Fatal("invalid component migration activated")
	}
	if getActiveID(t, app, service) != remounted {
		t.Fatal("failed activation changed active deployment")
	}
	rows, err = app.FindAllRecords(physical)
	if err != nil || len(rows) != 2 {
		t.Fatalf("failed activation changed component rows: rows=%d err=%v", len(rows), err)
	}
	collection, err = app.FindCollectionByNameOrId(physical)
	if err != nil || collection.GetIndex("idx_pbvex_"+physical+"_by_tag") == "" {
		t.Fatalf("failed activation changed component indexes: %v", err)
	}
}

func TestComponentTableMembershipOwnershipLifecycle(t *testing.T) {
	bundle := `(function(){
__pbvex.registerFunction({name:"writeB",type:"mutation",visibility:"public",modulePath:"pbvex/components/catalog/store.ts",exportName:"writeB"},async function(ctx,args){return await ctx.db.insert("b",{v:args.v});});
__pbvex.registerFunction({name:"countB",type:"query",visibility:"public",modulePath:"pbvex/components/catalog/store.ts",exportName:"countB"},async function(ctx){return (await ctx.db.query("b").withIndex("by_v").collect()).length;});
})();`
	table := func(name string) map[string]any {
		return map[string]any{
			"tableName": name,
			"fields":    map[string]any{"v": map[string]any{"type": "number"}},
			"indexes":   []any{map[string]any{"name": "by_v", "fields": []any{"v"}}},
		}
	}
	manifest := func(id string, tables ...map[string]any) deploy.DeploymentManifest {
		rawTables := make([]any, len(tables))
		for i := range tables {
			rawTables[i] = tables[i]
		}
		definition := deploy.ComponentDefinition{
			ModulePaths:  []string{"store.ts"},
			ModuleHashes: map[string]string{"store.ts": emptyHash()},
			Schema:       deploy.JSONValue(map[string]any{"tables": rawTables}),
		}
		definition.ComponentID = componentID(definition, bundleHash(bundle))
		return deploy.DeploymentManifest{
			ProtocolVersion: "v1",
			DeploymentID:    id,
			Functions: []deploy.FunctionDescriptor{
				{Name: "writeB", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/catalog/store.ts", ExportName: "writeB"},
				{Name: "countB", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/catalog/store.ts", ExportName: "countB"},
			},
			Components: &deploy.ComponentGraph{
				Definitions: []deploy.ComponentDefinition{definition},
				Mounts:      []deploy.ComponentMount{{Name: "catalog", ComponentID: definition.ComponentID}},
			},
		}
	}
	assertCatalogB := func(t *testing.T, app core.App) string {
		t.Helper()
		withB := manifest("catalog_probe", table("a"), table("b"))
		namespaces, err := deploy.ComponentNamespaces(withB.Components)
		if err != nil {
			t.Fatal(err)
		}
		namespace := namespaces["catalog"]
		physical := namespace.PhysicalByTable["b"]
		if got := componentCatalogCollectionsForTest(t, app, namespace.ID)["b"]; got != physical {
			t.Fatalf("catalog ownership for dormant b = %q, want %q", got, physical)
		}
		collection, err := app.FindCollectionByNameOrId(physical)
		if err != nil || collection.GetIndex("idx_pbvex_"+physical+"_by_v") == "" {
			t.Fatalf("b backing index unavailable: %v", err)
		}
		return physical
	}
	assertB := func(t *testing.T, app core.App, service *deploy.Service, deploymentID string, expected int64) {
		t.Helper()
		assertCatalogB(t, app)
		count, err := service.Invoke(context.Background(), deploymentID, "countB", map[string]any{})
		if err != nil || toInt64(count) != expected {
			t.Fatalf("b count = %v, want %d: %v", count, expected, err)
		}
	}

	t.Run("remove then rollback and re-add", func(t *testing.T) {
		app, service := newTestApp(t)
		ab := uploadComponentDeployment(t, service, manifest("membership_ab_v1", table("a"), table("b")), bundle)
		a := uploadComponentDeployment(t, service, manifest("membership_a_v2", table("a")), bundle)
		readd := uploadComponentDeployment(t, service, manifest("membership_ab_v3", table("a"), table("b")), bundle)
		if _, err := service.Activate(ab, true); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Invoke(context.Background(), ab, "writeB", map[string]any{"v": 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Activate(a, true); err != nil {
			t.Fatal(err)
		}
		physical := assertCatalogB(t, app)
		if rows, err := app.FindAllRecords(physical); err != nil || len(rows) != 1 {
			t.Fatalf("dormant b rows = %d: %v", len(rows), err)
		}
		if _, err := service.Rollback(a); err != nil {
			t.Fatal(err)
		}
		assertB(t, app, service, ab, 1)
		if _, err := service.Activate(a, true); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Activate(readd, true); err != nil {
			t.Fatal(err)
		}
		assertB(t, app, service, readd, 1)
	})

	t.Run("add then rollback and re-upgrade", func(t *testing.T) {
		app, service := newTestApp(t)
		a := uploadComponentDeployment(t, service, manifest("membership_a_v1", table("a")), bundle)
		ab := uploadComponentDeployment(t, service, manifest("membership_ab_v2", table("a"), table("b")), bundle)
		if _, err := service.Activate(a, true); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Activate(ab, true); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Invoke(context.Background(), ab, "writeB", map[string]any{"v": 2}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Rollback(ab); err != nil {
			t.Fatal(err)
		}
		physical := assertCatalogB(t, app)
		if rows, err := app.FindAllRecords(physical); err != nil || len(rows) != 1 {
			t.Fatalf("rolled-back b rows = %d: %v", len(rows), err)
		}
		if _, err := service.Activate(ab, true); err != nil {
			t.Fatal(err)
		}
		assertB(t, app, service, ab, 1)
	})
}

// TestComponentEnvAndArgsExposure verifies resolved env vars and mount args are
// exposed to the runtime context.
func TestComponentEnvAndArgsExposure(t *testing.T) {
	t.Setenv("PBVEX_TEST_TOKEN", "secret-token")
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"inspect",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/ops.ts",exportName:"inspect"}, async function(ctx) {
  return {greeting: ctx.env.GREETING, token: ctx.env.TOKEN, label: ctx.args.label};
});
})();`

	cfgDef := deploy.ComponentDefinition{
		ComponentID:  "",
		ModulePaths:  []string{"ops.ts"},
		ModuleHashes: map[string]string{"ops.ts": emptyHash()},
		Args:         map[string]any{"type": "object", "shape": map[string]any{"label": map[string]any{"type": "string"}}},
		Env: map[string]deploy.EnvArgDescriptor{
			"GREETING": {Type: "value", Value: "hi"},
			"TOKEN":    {Type: "envVar", Name: "PBVEX_TEST_TOKEN"},
		},
	}
	cfgDef.ComponentID = componentID(cfgDef, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "env_args",
		Functions: []deploy.FunctionDescriptor{{
			Name: "inspect", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/ops.ts", ExportName: "inspect",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{cfgDef},
			Mounts: []deploy.ComponentMount{{
				Name: "cfg", ComponentID: cfgDef.ComponentID, Args: map[string]any{"label": "world"},
			}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "inspect", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result %T", res)
	}
	if m["greeting"] != "hi" {
		t.Errorf("greeting = %v, want hi", m["greeting"])
	}
	if m["token"] != "secret-token" {
		t.Errorf("token = %v, want secret-token", m["token"])
	}
	if m["label"] != "world" {
		t.Errorf("label = %v, want world", m["label"])
	}
}

// TestRootNamespaceDatabaseOperations verifies that root (non-component)
// functions operate on the canonical root namespace and persist data through
// activation restarts.
func TestRootNamespaceDatabaseOperations(t *testing.T) {
	app, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"put",type:"mutation",visibility:"public",modulePath:"pbvex/root.ts",exportName:"put"}, async function(ctx,args){ return await ctx.db.insert("items",{n: args.n}); });
__pbvex.registerFunction({name:"count",type:"query",visibility:"public",modulePath:"pbvex/root.ts",exportName:"count"}, async function(ctx){ return (await ctx.db.query("items").collect()).length; });
})();`

	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "root_ns",
		Functions: []deploy.FunctionDescriptor{
			{Name: "put", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "put"},
			{Name: "count", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "count"},
		},
		Schema: map[string]any{"tables": []any{
			map[string]any{"tableName": "items", "fields": map[string]any{"n": map[string]any{"type": "number"}}},
		}},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// insert returns the canonical {id} shape.
	inserted, err := service.Invoke(context.Background(), id, "put", map[string]any{"n": 7})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	idStr, ok := inserted.(string)
	if !ok || idStr == "" {
		t.Fatalf("expected non-empty id string, got %#v", inserted)
	}
	if _, err := service.Invoke(context.Background(), id, "put", map[string]any{"n": 8}); err != nil {
		t.Fatalf("put2: %v", err)
	}
	c, _ := service.Invoke(context.Background(), id, "count", map[string]any{})
	if toInt64(c) != 2 {
		t.Fatalf("root count = %v, want 2", c)
	}

	// Data must persist across a restart using the stable root namespace.
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	c2, err := service.Invoke(context.Background(), id, "count", map[string]any{})
	if err != nil {
		t.Fatalf("count after restart: %v", err)
	}
	if toInt64(c2) != 2 {
		t.Fatalf("root count after restart = %v, want 2 (namespace not stable)", c2)
	}
}

// TestRootInsertReturnShapeAndCreationTime verifies the canonical document
// shape returned by get/query: _id string, _creationTime numeric.
func TestRootInsertReturnShapeAndCreationTime(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"put",type:"mutation",visibility:"public",modulePath:"pbvex/root.ts",exportName:"put"}, async function(ctx,args){ return await ctx.db.insert("items",{n: args.n}); });
__pbvex.registerFunction({name:"get",type:"query",visibility:"public",modulePath:"pbvex/root.ts",exportName:"get"}, async function(ctx,args){ return await ctx.db.get(args.id); });`

	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "root_shape",
		Functions: []deploy.FunctionDescriptor{
			{Name: "put", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "put"},
			{Name: "get", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "get"},
		},
		Schema: map[string]any{"tables": []any{
			map[string]any{"tableName": "items", "fields": map[string]any{"n": map[string]any{"type": "number"}}},
		}},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}

	putRes, err := service.Invoke(context.Background(), id, "put", map[string]any{"n": 1})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	docID, ok := putRes.(string)
	if !ok || docID == "" {
		t.Fatalf("expected id string, got %#v", putRes)
	}

	doc, err := service.Invoke(context.Background(), id, "get", map[string]any{"id": docID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	m, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("doc type %T", doc)
	}
	if m["_id"] != docID {
		t.Errorf("_id = %v, want %v", m["_id"], docID)
	}
	switch ct := m["_creationTime"].(type) {
	case int64, int, float64:
		// numeric, as the generated SDK declares (_creationTime: number).
	default:
		t.Errorf("_creationTime type %T, want numeric", ct)
	}
}

// TestPatchReplaceDeleteReturnUndefined verifies the runtime discards the
// stored document for patch/replace/delete and resolves to undefined (not
// null), matching the SDK DatabaseContext contract (Promise<void>).
// The assertion happens inside deployed JS (r === undefined, not r === null)
// because both JS null and undefined decode to Go nil — a Go-side nil check
// alone cannot distinguish them.
func TestPatchReplaceDeleteReturnUndefined(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"put",type:"mutation",visibility:"public",modulePath:"pbvex/root.ts",exportName:"put"}, async function(ctx,args){ return await ctx.db.insert("items",{n: args.n}); });
__pbvex.registerFunction({name:"patch",type:"mutation",visibility:"public",modulePath:"pbvex/root.ts",exportName:"patch"}, async function(ctx,args){
  var r = await ctx.db.patch(args.id, {n: args.n});
  if (r !== undefined) throw new Error("patch resolved to " + String(r) + " (" + typeof r + "), expected undefined");
  return true;
});
__pbvex.registerFunction({name:"replace",type:"mutation",visibility:"public",modulePath:"pbvex/root.ts",exportName:"replace"}, async function(ctx,args){
  var r = await ctx.db.replace(args.id, {n: args.n});
  if (r !== undefined) throw new Error("replace resolved to " + String(r) + " (" + typeof r + "), expected undefined");
  return true;
});
__pbvex.registerFunction({name:"del",type:"mutation",visibility:"public",modulePath:"pbvex/root.ts",exportName:"del"}, async function(ctx,args){
  var r = await ctx.db.delete(args.id);
  if (r !== undefined) throw new Error("delete resolved to " + String(r) + " (" + typeof r + "), expected undefined");
  return true;
});
})();`
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "undefined_returns",
		Functions: []deploy.FunctionDescriptor{
			{Name: "put", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "put"},
			{Name: "patch", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "patch"},
			{Name: "replace", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "replace"},
			{Name: "del", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "del"},
		},
		Schema: map[string]any{"tables": []any{
			map[string]any{"tableName": "items", "fields": map[string]any{"n": map[string]any{"type": "number"}}},
		}},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}

	putRes, _ := service.Invoke(context.Background(), id, "put", map[string]any{"n": float64(1)})
	docID := putRes.(string)

	for _, fn := range []string{"patch", "replace"} {
		res, err := service.Invoke(context.Background(), id, fn, map[string]any{"id": docID, "n": float64(2)})
		if err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		if b, ok := res.(bool); !ok || !b {
			t.Fatalf("%s did not assert undefined (got %#v)", fn, res)
		}
	}
	delRes, err := service.Invoke(context.Background(), id, "del", map[string]any{"id": docID})
	if err != nil {
		t.Fatalf("del: %v", err)
	}
	if b, ok := delRes.(bool); !ok || !b {
		t.Fatalf("del did not assert undefined (got %#v)", delRes)
	}
}

// TestComponentSourceTamperRejected verifies that a manifest whose declared
// moduleHashes/componentId do not match its definition is rejected (the
// componentId is tied to declared content and cannot be forged).
func TestComponentSourceTamperRejected(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"add",type:"mutation",visibility:"public",modulePath:"pbvex/components/counter/store.ts",exportName:"add"}, async function(ctx,args){ await ctx.db.insert("nums",{v: args.v}); return {ok:true}; });`
	schema := deploy.JSONValue(map[string]any{"tables": []any{
		map[string]any{"tableName": "nums", "fields": map[string]any{"v": map[string]any{"type": "number"}}},
	}})
	goodDef := deploy.ComponentDefinition{ModulePaths: []string{"store.ts"}, ModuleHashes: map[string]string{"store.ts": emptyHash()}, Schema: schema}
	goodDef.ComponentID = componentID(goodDef, bundleHash(bundle))

	mkReq := func(componentID string) map[string]any {
		manifest := deploy.DeploymentManifest{
			ProtocolVersion: "v1",
			DeploymentID:    "tamper",
			Functions: []deploy.FunctionDescriptor{
				{Name: "add", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/counter/store.ts", ExportName: "add"},
			},
			Components: &deploy.ComponentGraph{
				Definitions: []deploy.ComponentDefinition{{
					ComponentID:  componentID,
					ModulePaths:  goodDef.ModulePaths,
					ModuleHashes: goodDef.ModuleHashes,
					Schema:       schema,
				}},
				Mounts: []deploy.ComponentMount{{Name: "counter", ComponentID: componentID}},
			},
		}
		raw, _ := json.Marshal(manifest)
		var manifestMap map[string]any
		json.Unmarshal(raw, &manifestMap)
		return map[string]any{
			"manifest": manifestMap,
			"bundle":   testBundle(bundle),
			"sha256":   bundleHash(bundle),
			"size":     int64(len(bundle)),
			"modules":  []any{map[string]any{"path": "pbvex/components/counter/store.ts", "bytes": ""}},
		}
	}

	// Correct componentId is accepted.
	if _, err := service.Upload(mkReq(goodDef.ComponentID)); err != nil {
		t.Fatalf("valid componentId should upload: %v", err)
	}

	// Flip one hex character of the content-addressed id; the manifest now lies
	// about the component content and must be rejected.
	tampered := goodDef.ComponentID[:len(goodDef.ComponentID)-1] + "0"
	if _, err := service.Upload(mkReq(tampered)); err == nil {
		t.Fatal("expected tampered componentId to be rejected")
	}
}

// TestModuleSourceTamperRejected verifies that uploading module bytes whose
// recomputed hash does not match the manifest's declared moduleHashes is
// rejected (the componentId is tied to actual uploaded executable bytes, not
// client-declared hashes).
func TestModuleSourceTamperRejected(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"add",type:"mutation",visibility:"public",modulePath:"pbvex/components/counter/store.ts",exportName:"add"}, async function(ctx,args){ await ctx.db.insert("nums",{v: args.v}); return {ok:true}; });`
	schema := deploy.JSONValue(map[string]any{"tables": []any{
		map[string]any{"tableName": "nums", "fields": map[string]any{"v": map[string]any{"type": "number"}}},
	}})
	goodDef := deploy.ComponentDefinition{ModulePaths: []string{"store.ts"}, ModuleHashes: map[string]string{"store.ts": emptyHash()}, Schema: schema}
	goodDef.ComponentID = componentID(goodDef, bundleHash(bundle))

	raw, _ := json.Marshal(deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "module_tamper",
		Functions:       []deploy.FunctionDescriptor{{Name: "add", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/counter/store.ts", ExportName: "add"}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{{ComponentID: goodDef.ComponentID, ModulePaths: goodDef.ModulePaths, ModuleHashes: goodDef.ModuleHashes, Schema: schema}},
			Mounts:      []deploy.ComponentMount{{Name: "counter", ComponentID: goodDef.ComponentID}},
		},
	})
	var manifestMap map[string]any
	json.Unmarshal(raw, &manifestMap)
	base := map[string]any{
		"manifest": manifestMap,
		"bundle":   testBundle(bundle),
		"sha256":   bundleHash(bundle),
		"size":     int64(len(bundle)),
	}

	// Correct module bytes (empty content -> emptyHash) upload fine.
	good := copyReq(base)
	good["modules"] = []any{map[string]any{"path": "pbvex/components/counter/store.ts", "bytes": ""}}
	if _, err := service.Upload(good); err != nil {
		t.Fatalf("valid module bytes should upload: %v", err)
	}

	// Tampered module bytes: non-empty content whose hash != declared emptyHash.
	tampered := copyReq(base)
	tampered["modules"] = []any{map[string]any{"path": "pbvex/components/counter/store.ts", "bytes": testBundle("evil code")}}
	if _, err := service.Upload(tampered); err == nil {
		t.Fatal("expected module hash mismatch to be rejected")
	}

	// Missing module entirely.
	missing := copyReq(base)
	missing["modules"] = []any{}
	if _, err := service.Upload(missing); err == nil {
		t.Fatal("expected missing module to be rejected")
	}
}

func copyReq(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// TestComponentsDoNotCreateGenericDataCollection replaces the historical
// generic-store migration test: component storage is now one deterministic
// physical PocketBase collection per namespace/table, and no `_pbvex_data`
// collection is ever bootstrapped.
func TestComponentsDoNotCreateGenericDataCollection(t *testing.T) {
	app, _ := newTestApp(t)
	if _, err := app.FindCollectionByNameOrId("_pbvex_data"); err == nil {
		t.Fatal("generic component data collection must not exist")
	}
}

// TestBundleSwapRejected verifies that swapping the executable bundle with
// different code (while keeping modules and manifest componentIds intact) is
// rejected even when the attacker recomputes the self-declared bundle sha256
// and size. The componentId binds to the verified bundleSha, so a different
// bundle produces a content-hash mismatch.
func TestBundleSwapRejected(t *testing.T) {
	_, service := newTestApp(t)

	goodBundle := `__pbvex.registerFunction({name:"add",type:"mutation",visibility:"public",modulePath:"pbvex/components/counter/store.ts",exportName:"add"}, async function(ctx,args){ await ctx.db.insert("nums",{v: args.v}); return {ok:true}; });`
	evilBundle := `__pbvex.registerFunction({name:"add",type:"mutation",visibility:"public",modulePath:"pbvex/components/counter/store.ts",exportName:"add"}, async function(ctx,args){ await ctx.db.insert("nums",{v: args.v, pwned: true}); return {ok:true}; });`
	schema := deploy.JSONValue(map[string]any{"tables": []any{
		map[string]any{"tableName": "nums", "fields": map[string]any{"v": map[string]any{"type": "number"}}},
	}})
	def := deploy.ComponentDefinition{ModulePaths: []string{"store.ts"}, ModuleHashes: map[string]string{"store.ts": emptyHash()}, Schema: schema}
	def.ComponentID = componentID(def, bundleHash(goodBundle))

	mkReq := func(bundle string) map[string]any {
		raw, _ := json.Marshal(deploy.DeploymentManifest{
			ProtocolVersion: "v1",
			DeploymentID:    "bundle_swap",
			Functions:       []deploy.FunctionDescriptor{{Name: "add", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/counter/store.ts", ExportName: "add"}},
			Components: &deploy.ComponentGraph{
				Definitions: []deploy.ComponentDefinition{{ComponentID: def.ComponentID, ModulePaths: def.ModulePaths, ModuleHashes: def.ModuleHashes, Schema: schema}},
				Mounts:      []deploy.ComponentMount{{Name: "counter", ComponentID: def.ComponentID}},
			},
		})
		var manifestMap map[string]any
		json.Unmarshal(raw, &manifestMap)
		return map[string]any{
			"manifest": manifestMap,
			"bundle":   testBundle(bundle),
			"sha256":   bundleHash(bundle),
			"size":     int64(len(bundle)),
			"modules":  []any{map[string]any{"path": "pbvex/components/counter/store.ts", "bytes": ""}},
		}
	}

	// Original bundle uploads fine.
	if _, err := service.Upload(mkReq(goodBundle)); err != nil {
		t.Fatalf("valid bundle should upload: %v", err)
	}

	// Attacker swaps the executable bundle and recomputes sha256/size, but
	// keeps the same manifest (modules, componentIds). The componentId was
	// computed with the original bundleSha, so authentication must fail.
	if _, err := service.Upload(mkReq(evilBundle)); err == nil {
		t.Fatal("expected bundle swap to be rejected (componentId no longer matches)")
	}
}

// TestDefaultedArgsResolution verifies that mount args declared with a
// "defaulted" validator are resolved to their defaultValue when omitted from
// the mount definition, and the handler sees the resolved value.
func TestDefaultedArgsResolution(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"getRetries",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"getRetries"}, async function(ctx) {
  return ctx.args.retries;
});
})();`

	argsDesc := deploy.JSONValue(map[string]any{
		"type": "object",
		"shape": map[string]any{
			"retries": map[string]any{
				"type":         "defaulted",
				"validator":    map[string]any{"type": "number"},
				"defaultValue": float64(3),
			},
		},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "defaulted_args",
		Functions: []deploy.FunctionDescriptor{{
			Name: "getRetries", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "getRetries",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts: []deploy.ComponentMount{{
				Name: "cfg", ComponentID: def.ComponentID,
				// Mount omits retries — default should be applied
				Args: map[string]any{},
			}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "getRetries", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if toInt64(res) != 3 {
		t.Fatalf("expected default retries=3, got %v", res)
	}
}

// TestNestedDefaultedArgsResolution verifies that defaults nested inside
// object args are resolved recursively by bridge.resolveDefaults.
func TestNestedDefaultedArgsResolution(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"getCfg",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"getCfg"}, async function(ctx) {
  return ctx.args.config.timeout;
});
})();`

	argsDesc := deploy.JSONValue(map[string]any{
		"type": "object",
		"shape": map[string]any{
			"config": map[string]any{
				"type": "object",
				"shape": map[string]any{
					"timeout": map[string]any{
						"type":         "defaulted",
						"validator":    map[string]any{"type": "number"},
						"defaultValue": float64(30),
					},
				},
			},
		},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "nested_defaulted",
		Functions: []deploy.FunctionDescriptor{{
			Name: "getCfg", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "getCfg",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts: []deploy.ComponentMount{{
				Name: "cfg", ComponentID: def.ComponentID,
				// config provided but timeout omitted — nested default should apply
				Args: map[string]any{"config": map[string]any{}},
			}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "getCfg", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if toInt64(res) != 30 {
		t.Fatalf("expected nested default timeout=30, got %v", res)
	}
}

// TestDefaultedRejectsExplicitNull verifies that explicit JSON null does not
// satisfy a defaulted field — null is distinct from missing, so the upload is
// rejected at validation time.
func TestDefaultedRejectsExplicitNull(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"get",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"get"}, async function(ctx){ return ctx.args.retries; });`
	argsDesc := deploy.JSONValue(map[string]any{
		"type": "object",
		"shape": map[string]any{
			"retries": map[string]any{
				"type": "defaulted", "validator": map[string]any{"type": "number"}, "defaultValue": float64(3),
			},
		},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	raw, _ := json.Marshal(deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "defaulted_null",
		Functions:       []deploy.FunctionDescriptor{{Name: "get", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/components/cfg/store.ts", ExportName: "get"}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{{ComponentID: def.ComponentID, ModulePaths: def.ModulePaths, ModuleHashes: def.ModuleHashes, Args: argsDesc}},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID, Args: map[string]any{"retries": nil}}},
		},
	})
	var manifestMap map[string]any
	json.Unmarshal(raw, &manifestMap)
	req := map[string]any{
		"manifest": manifestMap,
		"bundle":   testBundle(bundle),
		"sha256":   bundleHash(bundle),
		"size":     int64(len(bundle)),
		"modules":  modulesForManifest(deploy.DeploymentManifest{Components: &deploy.ComponentGraph{Mounts: []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}}}}),
	}
	// Upload must reject explicit null for a defaulted number — null ≠ missing.
	if _, err := service.Upload(req); err == nil {
		t.Fatal("expected upload to reject explicit null for defaulted number")
	}
}

// TestInvalidDefaultValueRejected verifies that a defaultValue that does not
// satisfy the inner validator is rejected at upload time.
func TestInvalidDefaultValueRejected(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `__pbvex.registerFunction({name:"get",type:"query",visibility:"public",modulePath:"pbvex/root.ts",exportName:"get"}, async function(){ return 1; });`
	argsDesc := deploy.JSONValue(map[string]any{
		"type": "object",
		"shape": map[string]any{
			"retries": map[string]any{
				"type": "defaulted", "validator": map[string]any{"type": "number"}, "defaultValue": "not-a-number",
			},
		},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	raw, _ := json.Marshal(deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "bad_default",
		Functions:       []deploy.FunctionDescriptor{{Name: "get", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "get"}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{{ComponentID: def.ComponentID, ModulePaths: def.ModulePaths, ModuleHashes: def.ModuleHashes, Args: argsDesc}},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}},
		},
	})
	var manifestMap map[string]any
	json.Unmarshal(raw, &manifestMap)
	req := map[string]any{
		"manifest": manifestMap,
		"bundle":   testBundle(bundle),
		"sha256":   bundleHash(bundle),
		"size":     int64(len(bundle)),
		"modules":  modulesForManifest(deploy.DeploymentManifest{Components: &deploy.ComponentGraph{Mounts: []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}}}}),
	}
	if _, err := service.Upload(req); err == nil {
		t.Fatal("expected upload to reject invalid defaultValue (string for number validator)")
	}
}

// TestTopLevelDefaultedArgsResolution verifies that a top-level defaulted args
// descriptor (not wrapped in object) applies defaultValue when mount args are
// entirely absent. This tests the resolveDefaults "defaulted" case directly.
func TestTopLevelDefaultedArgsResolution(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"get",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"get"}, async function(ctx){ return ctx.args; });
})();`

	// Top-level descriptor is defaulted, not object
	argsDesc := deploy.JSONValue(map[string]any{
		"type":         "defaulted",
		"validator":    map[string]any{"type": "string"},
		"defaultValue": "fallback",
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))

	// Mount with NO args field at all — resolveDefaults should apply "fallback"
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "top_defaulted",
		Functions: []deploy.FunctionDescriptor{{
			Name: "get", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "get",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "get", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if s, ok := res.(string); !ok || s != "fallback" {
		t.Fatalf("expected top-level default 'fallback', got %v", res)
	}
}

// TestTopLevelUnionDefaultedResolution verifies that a top-level union args
// descriptor with a defaulted branch applies defaultValue when mount args
// are absent, and that isOptionalDescriptor admits the union.
func TestTopLevelUnionDefaultedResolution(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"get",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"get"}, async function(ctx){ return ctx.args; });
})();`

	argsDesc := deploy.JSONValue(map[string]any{
		"type": "union",
		"validators": []any{
			map[string]any{"type": "string"},
			map[string]any{
				"type":         "defaulted",
				"validator":    map[string]any{"type": "string"},
				"defaultValue": "union-default",
			},
		},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "union_defaulted",
		Functions: []deploy.FunctionDescriptor{{
			Name: "get", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "get",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "get", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if s, ok := res.(string); !ok || s != "union-default" {
		t.Fatalf("expected union default 'union-default', got %v", res)
	}
}

// TestTopLevelOptionalPreservesUndefined verifies that a component with
// top-level v.optional args, mounted without args, exposes ctx.args as
// undefined (not an empty object) to the handler.
func TestTopLevelOptionalPreservesUndefined(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"check",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"check"}, async function(ctx){
  if (ctx.args !== undefined) throw new Error("expected undefined, got " + typeof ctx.args);
  return "ok";
});
})();`

	argsDesc := deploy.JSONValue(map[string]any{
		"type":      "optional",
		"validator": map[string]any{"type": "string"},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "opt_undefined",
		Functions: []deploy.FunctionDescriptor{{
			Name: "check", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "check",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "check", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if s, ok := res.(string); !ok || s != "ok" {
		t.Fatalf("expected 'ok' (undefined preserved), got %v", res)
	}
}

// TestNestedUnionDefaultResolution verifies that an object field whose
// descriptor is a union containing a defaulted branch is resolved correctly
// when the field is absent.
func TestNestedUnionDefaultResolution(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"get",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"get"}, async function(ctx){ return ctx.args.config; });
})();`

	argsDesc := deploy.JSONValue(map[string]any{
		"type": "object",
		"shape": map[string]any{
			"config": map[string]any{
				"type": "union",
				"validators": []any{
					map[string]any{"type": "string"},
					map[string]any{
						"type":         "defaulted",
						"validator":    map[string]any{"type": "number"},
						"defaultValue": float64(42),
					},
				},
			},
		},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "nested_union_def",
		Functions: []deploy.FunctionDescriptor{{
			Name: "get", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "get",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts: []deploy.ComponentMount{{
				Name: "cfg", ComponentID: def.ComponentID,
				Args: map[string]any{}, // config omitted
			}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "get", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if toInt64(res) != 42 {
		t.Fatalf("expected nested union default 42, got %v", res)
	}
}

// TestUnionBranchOrderOptionalFirst verifies that union(optional(string),
// defaulted(number,3)) on missing yields undefined — because the optional
// branch accepts missing first in declaration order.
func TestUnionBranchOrderOptionalFirst(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"check",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"check"}, async function(ctx){
  if (ctx.args !== undefined) throw new Error("expected undefined, got " + typeof ctx.args);
  return "ok";
});
})();`

	argsDesc := deploy.JSONValue(map[string]any{
		"type": "union",
		"validators": []any{
			map[string]any{"type": "optional", "validator": map[string]any{"type": "string"}},
			map[string]any{"type": "defaulted", "validator": map[string]any{"type": "number"}, "defaultValue": float64(3)},
		},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "union_opt_first",
		Functions: []deploy.FunctionDescriptor{{
			Name: "check", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "check",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "check", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if s, ok := res.(string); !ok || s != "ok" {
		t.Fatalf("expected 'ok' (optional branch first → undefined), got %v", res)
	}
}

// TestUnionBranchOrderDefaultedFirst verifies that union(defaulted(number,3),
// optional(string)) on missing yields 3 — because the defaulted branch
// accepts missing first in declaration order.
func TestUnionBranchOrderDefaultedFirst(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"get",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"get"}, async function(ctx){ return ctx.args; });
})();`

	argsDesc := deploy.JSONValue(map[string]any{
		"type": "union",
		"validators": []any{
			map[string]any{"type": "defaulted", "validator": map[string]any{"type": "number"}, "defaultValue": float64(3)},
			map[string]any{"type": "optional", "validator": map[string]any{"type": "string"}},
		},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "union_def_first",
		Functions: []deploy.FunctionDescriptor{{
			Name: "get", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "get",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "get", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if toInt64(res) != 3 {
		t.Fatalf("expected defaulted branch first → 3, got %v", res)
	}
}

// TestExplicitNullReachesJSNull verifies that explicit JSON null for a
// nullable descriptor (any) reaches the handler as JS null, not undefined.
func TestExplicitNullReachesJSNull(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"check",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"check"}, async function(ctx){
  if (ctx.args === null) return "null";
  if (ctx.args === undefined) return "undefined";
  return "other:" + typeof ctx.args;
});
})();`

	argsDesc := deploy.JSONValue(map[string]any{"type": "any"})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "explicit_null",
		Functions: []deploy.FunctionDescriptor{{
			Name: "check", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "check",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}},
		},
	}
	// Build request with explicit "args": null in the raw map
	raw, _ := json.Marshal(manifest)
	var manifestMap map[string]any
	json.Unmarshal(raw, &manifestMap)
	mounts := manifestMap["components"].(map[string]any)["mounts"].([]any)
	mounts[0].(map[string]any)["args"] = nil // Explicit JSON null
	req := map[string]any{
		"manifest": manifestMap,
		"bundle":   testBundle(bundle),
		"sha256":   bundleHash(bundle),
		"size":     int64(len(bundle)),
		"modules":  modulesForManifest(manifest),
	}
	resp, err := service.Upload(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), resp.DeploymentID, "check", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if s, ok := res.(string); !ok || s != "null" {
		t.Fatalf("expected 'null' (explicit null preserved), got %v", res)
	}
}

// TestNestedOptionalMissingIsUndefined verifies that a missing optional
// field inside an object is not present on ctx.args (JS undefined, not null).
func TestNestedOptionalMissingIsUndefined(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"check",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"check"}, async function(ctx){
  if ("opt" in ctx.args) return "present:" + ctx.args.opt;
  return "absent";
});
})();`

	argsDesc := deploy.JSONValue(map[string]any{
		"type": "object",
		"shape": map[string]any{
			"opt": map[string]any{"type": "optional", "validator": map[string]any{"type": "string"}},
		},
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "nested_opt_missing",
		Functions: []deploy.FunctionDescriptor{{
			Name: "check", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "check",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts: []deploy.ComponentMount{{
				Name: "cfg", ComponentID: def.ComponentID,
				Args: map[string]any{}, // opt omitted
			}},
		},
	}
	id := uploadComponentDeployment(t, service, manifest, bundle)
	if _, err := service.Activate(id, true); err != nil {
		t.Fatalf("activate: %v", err)
	}
	res, err := service.Invoke(context.Background(), id, "check", map[string]any{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if s, ok := res.(string); !ok || s != "absent" {
		t.Fatalf("expected 'absent' (optional field omitted, not null), got %v", res)
	}
}

// TestDefaultedAnyReplacesExplicitNull verifies that defaulted(any, "fallback")
// applies the default for absent args but NOT for explicit null — explicit
// null reaches the handler as null.
func TestDefaultedAnyReplacesExplicitNull(t *testing.T) {
	_, service := newTestApp(t)

	bundle := `(function(){
__pbvex.registerFunction({name:"check",type:"query",visibility:"public",modulePath:"pbvex/components/cfg/store.ts",exportName:"check"}, async function(ctx){
  if (ctx.args === null) return "null";
  if (ctx.args === undefined) return "undefined";
  return String(ctx.args);
});
})();`

	argsDesc := deploy.JSONValue(map[string]any{
		"type":         "defaulted",
		"validator":    map[string]any{"type": "any"},
		"defaultValue": "fallback",
	})
	def := deploy.ComponentDefinition{
		ModulePaths:  []string{"store.ts"},
		ModuleHashes: map[string]string{"store.ts": emptyHash()},
		Args:         argsDesc,
	}
	def.ComponentID = componentID(def, bundleHash(bundle))

	// Absent → default "fallback"
	manifestAbsent := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "def_any_absent",
		Functions: []deploy.FunctionDescriptor{{
			Name: "check", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "check",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}},
		},
	}
	id1 := uploadComponentDeployment(t, service, manifestAbsent, bundle)
	if _, err := service.Activate(id1, true); err != nil {
		t.Fatalf("activate absent: %v", err)
	}
	res1, _ := service.Invoke(context.Background(), id1, "check", map[string]any{})
	if s, ok := res1.(string); !ok || s != "fallback" {
		t.Fatalf("absent: expected 'fallback', got %v", res1)
	}

	// Explicit null → handler sees null (not the default)
	manifestNull := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "def_any_null",
		Functions: []deploy.FunctionDescriptor{{
			Name: "check", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "check",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{def},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: def.ComponentID}},
		},
	}
	// Build request with explicit "args": null
	rawNull, _ := json.Marshal(manifestNull)
	var manifestMapNull map[string]any
	json.Unmarshal(rawNull, &manifestMapNull)
	mountsNull := manifestMapNull["components"].(map[string]any)["mounts"].([]any)
	mountsNull[0].(map[string]any)["args"] = nil
	reqNull := map[string]any{
		"manifest": manifestMapNull,
		"bundle":   testBundle(bundle),
		"sha256":   bundleHash(bundle),
		"size":     int64(len(bundle)),
		"modules":  modulesForManifest(manifestNull),
	}
	id2, err := service.Upload(reqNull)
	if err != nil {
		t.Fatalf("upload null: %v", err)
	}
	if _, err := service.Activate(id2.DeploymentID, true); err != nil {
		t.Fatalf("activate null: %v", err)
	}
	res2, _ := service.Invoke(context.Background(), id2.DeploymentID, "check", map[string]any{})
	if s, ok := res2.(string); !ok || s != "null" {
		t.Fatalf("explicit null: expected 'null', got %v", res2)
	}

	// An absent arg whose declared default is null is also resolved, rather
	// than being mistaken for an absent optional value.
	defNullDefault := def
	defNullDefault.Args = deploy.JSONValue(map[string]any{
		"type":         "defaulted",
		"validator":    map[string]any{"type": "any"},
		"defaultValue": nil,
	})
	defNullDefault.ComponentID = componentID(defNullDefault, bundleHash(bundle))
	manifestNullDefault := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    "def_any_null_default",
		Functions: []deploy.FunctionDescriptor{{
			Name: "check", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "pbvex/components/cfg/store.ts", ExportName: "check",
		}},
		Components: &deploy.ComponentGraph{
			Definitions: []deploy.ComponentDefinition{defNullDefault},
			Mounts:      []deploy.ComponentMount{{Name: "cfg", ComponentID: defNullDefault.ComponentID}},
		},
	}
	id3 := uploadComponentDeployment(t, service, manifestNullDefault, bundle)
	if _, err := service.Activate(id3, true); err != nil {
		t.Fatalf("activate null default: %v", err)
	}
	res3, err := service.Invoke(context.Background(), id3, "check", map[string]any{})
	if err != nil {
		t.Fatalf("invoke null default: %v", err)
	}
	if s, ok := res3.(string); !ok || s != "null" {
		t.Fatalf("absent null default: expected 'null', got %v", res3)
	}
}
