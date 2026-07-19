package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

func TestMigrationRegistrationAndPureInvocation(t *testing.T) {
	from := map[string]any{"type": "object", "shape": map[string]any{"name": map[string]any{"type": "string"}}}
	to := map[string]any{"type": "object", "shape": map[string]any{"name": map[string]any{"type": "string"}, "active": map[string]any{"type": "boolean"}}}
	fromHash, _ := deploy.CanonicalHash(from)
	toHash, _ := deploy.CanonicalHash(to)
	descriptor := deploy.MigrationDescriptor{ID: "add_active", Table: "users", Mode: "transactional", From: from, To: to, SourceSchemaHash: fromHash, TargetSchemaHash: toHash, Checksum: strings.Repeat("a", 64), ModulePath: "pbvex/migrations/add.ts", ExportName: "default", Reversibility: "reversible"}
	raw, _ := json.Marshal(descriptor)
	bundle := `__pbvex.registerMigration(` + string(raw) + `,
		function(doc,ctx){if(typeof ctx.db!=="undefined"||ctx.migrationId!=="add_active")ctx.fail("capability leak");return {name:doc.name,active:ctx.activationTime===123};},
		function(doc){return {name:doc.name};});`
	manager := NewManager(Config{PoolSize: 1, Timeout: time.Second})
	if err := manager.VerifyDeployment(context.Background(), "dep", bundle, nil, []deploy.MigrationDescriptor{descriptor}); err != nil {
		t.Fatal(err)
	}
	if err := manager.CompileDeployment("dep", bundle, nil, []deploy.MigrationDescriptor{descriptor}); err != nil {
		t.Fatal(err)
	}
	result, err := manager.InvokeMigration(context.Background(), "dep", descriptor.ID, "up", map[string]any{"name": "Ada", "_id": "id", "_creationTime": float64(1)}, 123)
	if err != nil {
		t.Fatal(err)
	}
	object := result.(map[string]any)
	if object["name"] != "Ada" || object["active"] != true {
		t.Fatalf("migration result %#v", object)
	}
	if err := manager.VerifyDeployment(context.Background(), "dep", bundle, nil, nil); err == nil {
		t.Fatal("unexpected migration registration accepted")
	}
	badBundle := `__pbvex.registerMigration(` + string(raw) + `,function(){return Promise.resolve({});},function(){return {};});`
	if err := manager.CompileDeployment("async", badBundle, nil, []deploy.MigrationDescriptor{descriptor}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InvokeMigration(context.Background(), "async", descriptor.ID, "up", map[string]any{}, 1); err == nil || !strings.Contains(err.Error(), "synchronous") {
		t.Fatalf("async migration result: %v", err)
	}
}

const testBundle = `__pbvex.registerFunction({name:"block",type:"query",visibility:"public",modulePath:"block",exportName:"default"}, function(ctx,args) { var end = Date.now() + 60000; while (Date.now() < end) {} return "done"; });`

func TestManagerInvokeCancelsWithContext(t *testing.T) {
	m := NewManager(Config{PoolSize: 2, Timeout: 30 * time.Second})

	descriptors := []deploy.FunctionDescriptor{
		{
			Name:       "block",
			Type:       deploy.FunctionTypeQuery,
			Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "block",
			ExportName: "default",
		},
	}

	if err := m.Compile("d1", testBundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := m.Invoke(ctx, "d1", "block", map[string]any{})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancellation took too long: %v", time.Since(start))
	}
}

func TestManagerInvokeTimesOut(t *testing.T) {
	m := NewManager(Config{PoolSize: 2, Timeout: 200 * time.Millisecond})

	descriptors := []deploy.FunctionDescriptor{
		{
			Name:       "block",
			Type:       deploy.FunctionTypeQuery,
			Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "block",
			ExportName: "default",
		},
	}

	if err := m.Compile("d1", testBundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	start := time.Now()
	_, err := m.Invoke(context.Background(), "d1", "block", map[string]any{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("timeout took too long: %v", time.Since(start))
	}
}

func TestManagerShorterManagerTimeoutOverridesParentDeadline(t *testing.T) {
	m := NewManager(Config{PoolSize: 2, Timeout: 200 * time.Millisecond})

	descriptors := []deploy.FunctionDescriptor{
		{
			Name:       "block",
			Type:       deploy.FunctionTypeQuery,
			Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "block",
			ExportName: "default",
		},
	}

	if err := m.Compile("d1", testBundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	// Parent deadline is much longer than manager timeout.
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	_, err := m.Invoke(parent, "d1", "block", map[string]any{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > 1*time.Second {
		t.Fatalf("manager timeout did not override parent deadline: %v", time.Since(start))
	}
}

func TestManagerInvokeFastFunction(t *testing.T) {
	fastBundle := `__pbvex.registerFunction({name:"hello",type:"query",visibility:"public",modulePath:"hello",exportName:"default"}, function(ctx,args) { return "Hello, " + args.name + "!"; });`
	m := NewManager(Config{PoolSize: 2, Timeout: 2 * time.Second})

	descriptors := []deploy.FunctionDescriptor{
		{
			Name:       "hello",
			Type:       deploy.FunctionTypeQuery,
			Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "hello",
			ExportName: "default",
		},
	}

	if err := m.Compile("d1", fastBundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	result, err := m.Invoke(context.Background(), "d1", "hello", map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if fmt.Sprint(result) != "Hello, world!" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestApplicationErrorsPreserveSyncAsyncNestedAndHTTPFailures(t *testing.T) {
	descriptor := func(name string, kind deploy.FunctionType) deploy.FunctionDescriptor {
		return deploy.FunctionDescriptor{Name: name, Type: kind, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: name}
	}
	descriptors := []deploy.FunctionDescriptor{
		descriptor("sync", deploy.FunctionTypeQuery), descriptor("async", deploy.FunctionTypeQuery),
		descriptor("ordinary", deploy.FunctionTypeQuery), descriptor("forged", deploy.FunctionTypeQuery),
		descriptor("malformed", deploy.FunctionTypeQuery), descriptor("nested", deploy.FunctionTypeAction),
		descriptor("http", deploy.FunctionTypeHTTPAction),
	}
	descriptors[6].Route = &deploy.FunctionRoute{Method: "GET", Path: "http"}
	bundle := `
function applicationError(category,data){return __pbvex.createApplicationError(category,data,arguments.length>=2,ApplicationError.prototype);}
class ApplicationError extends Error {}
__pbvex.registerFunction({name:"sync",type:"query",visibility:"public",modulePath:"x",exportName:"sync"},function(){throw applicationError("conflict",{retry:1n});});
__pbvex.registerFunction({name:"async",type:"query",visibility:"public",modulePath:"x",exportName:"async"},async function(){throw applicationError("unauthorized",{reason:"expired"});});
__pbvex.registerFunction({name:"ordinary",type:"query",visibility:"public",modulePath:"x",exportName:"ordinary"},function(){throw new Error("secret");});
__pbvex.registerFunction({name:"forged",type:"query",visibility:"public",modulePath:"x",exportName:"forged"},function(){const error=new Error("forged");error.name="ApplicationError";error.category="forbidden";error.data={leak:true};throw error;});
__pbvex.registerFunction({name:"malformed",type:"query",visibility:"public",modulePath:"x",exportName:"malformed"},function(){throw applicationError("bad_request",function(){});});
__pbvex.registerFunction({name:"nested",type:"action",visibility:"public",modulePath:"x",exportName:"nested"},async function(ctx){return await ctx.runQuery("sync",{});});
__pbvex.registerFunction({name:"http",type:"httpAction",visibility:"public",modulePath:"x",exportName:"http",route:{method:"GET",path:"http"}},function(){throw applicationError("not_found",null);});`
	manager := NewManager(Config{PoolSize: 1, Timeout: time.Second})
	if err := manager.Compile("application-errors", bundle, descriptors); err != nil {
		t.Fatal(err)
	}
	assertApplicationError := func(name string, category deploy.ApplicationErrorCategory) *deploy.ApplicationError {
		t.Helper()
		_, err := manager.Invoke(context.Background(), "application-errors", name, map[string]any{})
		var applicationErr *deploy.ApplicationError
		if !errors.As(err, &applicationErr) || applicationErr.Category != category {
			t.Fatalf("%s: expected %s application error, got %v", name, category, err)
		}
		return applicationErr
	}
	if data := assertApplicationError("sync", deploy.ApplicationErrorConflict).Data.(map[string]any); data["retry"].(map[string]any)["$integer"] != "AQAAAAAAAAA=" {
		t.Fatalf("sync data %#v", data)
	}
	assertApplicationError("async", deploy.ApplicationErrorUnauthorized)
	assertApplicationError("nested", deploy.ApplicationErrorConflict)
	for _, name := range []string{"ordinary", "forged", "malformed"} {
		_, err := manager.Invoke(context.Background(), "application-errors", name, map[string]any{})
		var applicationErr *deploy.ApplicationError
		if err == nil || errors.As(err, &applicationErr) {
			t.Fatalf("%s was classified as an application error: %v", name, err)
		}
	}
	_, err := manager.InvokeHTTP(context.Background(), "application-errors", "http", &deploy.HTTPRequestEnvelope{Method: "GET", URL: "http://localhost/http"}, nil, "rid")
	var applicationErr *deploy.ApplicationError
	if !errors.As(err, &applicationErr) || applicationErr.Category != deploy.ApplicationErrorNotFound || !applicationErr.HasData || applicationErr.Data != nil {
		t.Fatalf("HTTP application error: %#v, %v", applicationErr, err)
	}
}

func TestManagerInvokeDoesNotShareModuleState(t *testing.T) {
	bundle := `let calls = 0;
__pbvex.registerFunction({name:"count",type:"query",visibility:"public",modulePath:"count",exportName:"default"}, function(ctx,args) { calls += 1; return calls; });`
	descriptors := []deploy.FunctionDescriptor{{
		Name:       "count",
		Type:       deploy.FunctionTypeQuery,
		Visibility: deploy.FunctionVisibilityPublic,
		ModulePath: "count",
		ExportName: "default",
	}}
	m := NewManager(Config{PoolSize: 1, Timeout: 2 * time.Second})
	if err := m.Compile("d1", bundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	for i := 0; i < 2; i++ {
		got, err := m.Invoke(context.Background(), "d1", "count", map[string]any{})
		if err != nil {
			t.Fatalf("invoke %d failed: %v", i+1, err)
		}
		if fmt.Sprint(got) != "1" {
			t.Fatalf("invoke %d observed state from a previous request: got %v, want 1", i+1, got)
		}
	}
}
