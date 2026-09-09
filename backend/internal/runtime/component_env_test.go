package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// Values that must never cross the resolver boundary into public errors,
// JavaScript, or observer events.
const (
	envManagedName        = "PBVEX_TEST_MANAGED_RUNTIME_TOKEN"
	envManagedValue       = "managed-secret-value-9f2"
	envProviderDiagnostic = "provider denied environment.read via socket /run/pbvex/policy.sock"
)

// envResolverStub records every consulted name so tests can prove that the
// runtime resolves each binding per operation and never caches outcomes.
type envResolverStub struct {
	mu     sync.Mutex
	calls  []string
	policy func(call int, name string) (string, bool, error)
}

func (r *envResolverStub) resolve(ctx context.Context, name string) (string, bool, error) {
	r.mu.Lock()
	r.calls = append(r.calls, name)
	call, policy := len(r.calls), r.policy
	r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if policy == nil {
		return envManagedValue, true, nil
	}
	return policy(call, name)
}

func (r *envResolverStub) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func TestComponentEnvResolverBindingMatrix(t *testing.T) {
	newNamespace := func() deploy.ComponentNamespace {
		definition := deploy.ComponentDefinition{ComponentID: "secrets", Env: map[string]deploy.EnvArgDescriptor{
			"TOKEN":   {Type: "envVar", Name: envManagedName},
			"LITERAL": {Type: "value", Value: "literal-value"},
		}}
		return testNamespace(t, definition, deploy.ComponentMount{Name: "secrets", ComponentID: "secrets"})
	}

	t.Run("allowed resolves through the resolver only for envVar bindings", func(t *testing.T) {
		stub := &envResolverStub{}
		env, err := resolveComponentEnv(context.Background(), newNamespace(), stub.resolve)
		if err != nil {
			t.Fatal(err)
		}
		if env["TOKEN"] != envManagedValue || env["LITERAL"] != "literal-value" {
			t.Fatalf("unexpected resolved env: %#v", env)
		}
		if recorded := stub.recorded(); len(recorded) != 1 || recorded[0] != envManagedName {
			t.Fatalf("resolver must see exactly the declared variable name: %#v", recorded)
		}
	})

	t.Run("provided empty string stays distinct from unset", func(t *testing.T) {
		stub := &envResolverStub{policy: func(int, string) (string, bool, error) { return "", true, nil }}
		env, err := resolveComponentEnv(context.Background(), newNamespace(), stub.resolve)
		if err != nil || env["TOKEN"] != "" {
			t.Fatalf("explicit empty env must resolve: %#v %v", env, err)
		}
	})

	t.Run("unset fails with a sanitized error", func(t *testing.T) {
		stub := &envResolverStub{policy: func(int, string) (string, bool, error) { return "", false, nil }}
		_, err := resolveComponentEnv(context.Background(), newNamespace(), stub.resolve)
		if err == nil || !strings.Contains(err.Error(), `env "TOKEN" is not provided`) {
			t.Fatalf("expected sanitized unset error, got %v", err)
		}
		assertNoManagedEnvDisclosure(t, err.Error())
	})

	t.Run("resolver errors never expose policy diagnostics", func(t *testing.T) {
		stub := &envResolverStub{policy: func(int, string) (string, bool, error) {
			return "", false, errors.New(envProviderDiagnostic + " value=" + envManagedValue)
		}}
		_, err := resolveComponentEnv(context.Background(), newNamespace(), stub.resolve)
		if err == nil || !strings.Contains(err.Error(), `env "TOKEN" is unavailable`) {
			t.Fatalf("expected sanitized denial error, got %v", err)
		}
		assertNoManagedEnvDisclosure(t, err.Error())
	})

	t.Run("context cancellation is honored before the resolver runs", func(t *testing.T) {
		stub := &envResolverStub{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := resolveComponentEnv(ctx, newNamespace(), stub.resolve)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if recorded := stub.recorded(); len(recorded) != 0 {
			t.Fatalf("canceled context must skip the resolver: %#v", recorded)
		}
	})
}

func newComponentEnvApp(t *testing.T) (core.App, *deploy.ComponentGraph, string) {
	t.Helper()
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Cleanup() })
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	componentSchema := map[string]any{"tables": []any{map[string]any{
		"tableName": "items",
		"fields":    map[string]any{"owner": map[string]any{"type": "string"}},
	}}}
	definition := deploy.ComponentDefinition{
		ComponentID: "widget", ModulePaths: []string{"store.ts"}, Schema: componentSchema,
		Env: map[string]deploy.EnvArgDescriptor{
			"TOKEN":   {Type: "envVar", Name: envManagedName},
			"LITERAL": {Type: "value", Value: "literal-value"},
		},
	}
	graph := &deploy.ComponentGraph{Definitions: []deploy.ComponentDefinition{definition}, Mounts: []deploy.ComponentMount{{Name: "widget", ComponentID: "widget"}}}
	namespaces, err := deploy.ComponentNamespaces(graph)
	if err != nil {
		t.Fatal(err)
	}
	physical := namespaces["widget"].PhysicalByTable["items"]
	collection := core.NewBaseCollection(physical)
	collection.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	collection.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	collection.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	return app, graph, physical
}

const envComponentDeploymentID = "env-component"

var envComponentDescriptors = []deploy.FunctionDescriptor{
	{Name: "token", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/components/widget/store.ts", ExportName: "token"},
	{Name: "write", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/components/widget/store.ts", ExportName: "write"},
	{Name: "outerWrite", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "outerWrite"},
	{Name: "outerToken", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "outerToken"},
}

const envComponentBundle = `
__pbvex.registerFunction({name:"token",type:"query",visibility:"internal",modulePath:"pbvex/components/widget/store.ts",exportName:"token"}, function(ctx){ return {token: ctx.env.TOKEN, literal: ctx.env.LITERAL}; });
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"internal",modulePath:"pbvex/components/widget/store.ts",exportName:"write"}, function(ctx){ ctx.db.insert("items",{owner:"handler-ran"}); return "written"; });
__pbvex.registerFunction({name:"outerWrite",type:"action",visibility:"public",modulePath:"pbvex/root.ts",exportName:"outerWrite"}, async function(ctx){ return await ctx.runMutation("write",{}); });
__pbvex.registerFunction({name:"outerToken",type:"action",visibility:"public",modulePath:"pbvex/root.ts",exportName:"outerToken"}, async function(ctx){ return await ctx.runQuery("token",{}); });
`

func newComponentEnvManager(t *testing.T, resolver EnvironmentResolver) (*Manager, *executionRecorder) {
	t.Helper()
	observer := &executionRecorder{}
	config := DefaultConfig()
	config.ExecutionObserver = observer
	config.EnvironmentResolver = resolver
	manager := NewManager(config)
	if err := manager.Compile(envComponentDeploymentID, envComponentBundle, envComponentDescriptors, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	return manager, observer
}

func invokeComponentEnv(t *testing.T, manager *Manager, app core.App, graph *deploy.ComponentGraph, functionName string) (any, error) {
	t.Helper()
	manifest := deploy.DeploymentManifest{DeploymentID: envComponentDeploymentID, Functions: envComponentDescriptors, Components: graph}
	identity := &auth.UserIdentity{Subject: "user-1", TokenIdentifier: "pocketbase:users:user-1", Issuer: "pocketbase:users"}
	return manager.InvokeWithDatabase(context.Background(), envComponentDeploymentID, functionName, map[string]any{}, identity, "request-1", app, manifest)
}

func assertNoManagedEnvDisclosure(t *testing.T, candidates ...string) {
	t.Helper()
	for _, candidate := range candidates {
		for _, forbidden := range []string{envManagedName, envManagedValue, envProviderDiagnostic} {
			if strings.Contains(candidate, forbidden) {
				t.Fatalf("managed env detail %q leaked: %q", forbidden, candidate)
			}
		}
	}
}

func observerReportedStrings(recorder *executionRecorder) []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	out := make([]string, 0, len(recorder.starts)+len(recorder.ends))
	for _, info := range recorder.starts {
		out = append(out, info.ID, info.RootID, info.ParentID, info.DeploymentID, info.FunctionName, info.Namespace, string(info.FunctionType), info.Origin)
	}
	for _, results := range recorder.ends {
		for _, result := range results {
			if result.Err != nil {
				out = append(out, result.Err.Error())
			}
		}
	}
	return out
}

func TestManagerComponentEnvResolverAllowsDirectAndNestedPaths(t *testing.T) {
	stub := &envResolverStub{}
	manager, _ := newComponentEnvManager(t, stub.resolve)
	app, graph, physical := newComponentEnvApp(t)

	result, err := invokeComponentEnv(t, manager, app, graph, "token")
	if err != nil {
		t.Fatal(err)
	}
	env := result.(map[string]any)
	if env["token"] != envManagedValue || env["literal"] != "literal-value" {
		t.Fatalf("component env lost resolver or literal values: %#v", env)
	}

	nested, err := invokeComponentEnv(t, manager, app, graph, "outerToken")
	if err != nil {
		t.Fatal(err)
	}
	if got := nested.(map[string]any); got["token"] != envManagedValue {
		t.Fatalf("nested component invocation lost resolver env: %#v", got)
	}

	if _, err := invokeComponentEnv(t, manager, app, graph, "outerWrite"); err != nil {
		t.Fatal(err)
	}
	if count, err := backingRecordCountForTest(app, physical); err != nil || count != 1 {
		t.Fatalf("nested component mutation side effect missing: count=%d err=%v", count, err)
	}

	for _, name := range stub.recorded() {
		if name != envManagedName {
			t.Fatalf("resolver consulted with unexpected name %q", name)
		}
	}
	if calls := len(stub.recorded()); calls != 3 {
		t.Fatalf("expected one resolver call per envVar binding resolution, got %d", calls)
	}
}

func TestManagerComponentEnvResolverDenialFailsBeforeHandler(t *testing.T) {
	stub := &envResolverStub{policy: func(int, string) (string, bool, error) {
		return "", false, errors.New(envProviderDiagnostic)
	}}
	manager, observer := newComponentEnvManager(t, stub.resolve)
	app, graph, physical := newComponentEnvApp(t)

	for _, functionName := range []string{"token", "write", "outerToken", "outerWrite"} {
		_, err := invokeComponentEnv(t, manager, app, graph, functionName)
		if err == nil {
			t.Fatalf("denied %s must fail", functionName)
		}
		assertNoManagedEnvDisclosure(t, err.Error())
	}
	if count, err := backingRecordCountForTest(app, physical); err != nil || count != 0 {
		t.Fatalf("denied env must prevent handler side effects: count=%d err=%v", count, err)
	}
	assertNoManagedEnvDisclosure(t, observerReportedStrings(observer)...)
}

func TestManagerComponentEnvResolverGatesPerOperation(t *testing.T) {
	stub := &envResolverStub{policy: func(call int, _ string) (string, bool, error) {
		if call == 1 {
			return envManagedValue, true, nil
		}
		return "", false, errors.New(envProviderDiagnostic)
	}}
	manager, _ := newComponentEnvManager(t, stub.resolve)
	app, graph, _ := newComponentEnvApp(t)

	result, err := invokeComponentEnv(t, manager, app, graph, "token")
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["token"] != envManagedValue {
		t.Fatalf("allowed operation lost resolver value: %#v", result)
	}

	if _, err := invokeComponentEnv(t, manager, app, graph, "token"); err == nil {
		t.Fatal("policy denial between operations must apply to the next invocation")
	}
	if calls := len(stub.recorded()); calls != 2 {
		t.Fatalf("runtime must consult the resolver per operation, got %d calls", calls)
	}
}

func TestManagerNilEnvironmentResolverReadsProcessEnvironment(t *testing.T) {
	t.Setenv(envManagedName, envManagedValue)
	manager, _ := newComponentEnvManager(t, nil)
	app, graph, _ := newComponentEnvApp(t)

	result, err := invokeComponentEnv(t, manager, app, graph, "token")
	if err != nil {
		t.Fatal(err)
	}
	env := result.(map[string]any)
	if env["token"] != envManagedValue {
		t.Fatalf("nil resolver must preserve standalone os.LookupEnv behavior: %#v", env)
	}
}

func TestManagerEnvironmentResolverDoesNotDisturbCompileReuse(t *testing.T) {
	stub := &envResolverStub{}
	manager, _ := newComponentEnvManager(t, stub.resolve)

	bundle := `__pbvex.registerFunction({name:"hello",type:"query",visibility:"public",modulePath:"hello",exportName:"default"}, function(){ return "ok"; });`
	descriptors := []deploy.FunctionDescriptor{{Name: "hello", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "hello", ExportName: "default"}}
	if err := manager.Compile("env-reuse", bundle, descriptors, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	first := manager.pools["env-reuse"]
	if err := manager.Compile("env-reuse", bundle, descriptors, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	if manager.pools["env-reuse"] != first {
		t.Fatal("resolver presence must not invalidate compiled runtime reuse")
	}
	result, err := manager.Invoke(context.Background(), "env-reuse", "hello", map[string]any{})
	if err != nil || result != "ok" {
		t.Fatalf("invocation after reuse failed: %v %v", result, err)
	}
	if recorded := stub.recorded(); len(recorded) != 0 {
		t.Fatalf("root functions must not consult the environment resolver: %#v", recorded)
	}
}
