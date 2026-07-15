package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func testNamespace(t *testing.T, definition deploy.ComponentDefinition, mount deploy.ComponentMount) deploy.ComponentNamespace {
	t.Helper()
	graph := &deploy.ComponentGraph{Definitions: []deploy.ComponentDefinition{definition}, Mounts: []deploy.ComponentMount{mount}}
	namespaces, err := deploy.ComponentNamespaces(graph)
	if err != nil {
		t.Fatal(err)
	}
	return namespaces[mount.Name]
}

func TestMountArgsResolutionPreservesDefaultNullAndAbsence(t *testing.T) {
	defaultNull := testNamespace(t,
		deploy.ComponentDefinition{ComponentID: "defaultNull", Args: map[string]any{"type": "defaulted", "validator": map[string]any{"type": "any"}, "defaultValue": nil}},
		deploy.ComponentMount{Name: "defaultNull", ComponentID: "defaultNull"},
	)
	value, present, err := resolveComponentArgs(defaultNull, nil)
	if err != nil || !present || value != nil {
		t.Fatalf("default null must resolve as explicit null: %#v %v %v", value, present, err)
	}

	optional := testNamespace(t,
		deploy.ComponentDefinition{ComponentID: "optional", Args: map[string]any{"type": "optional", "validator": map[string]any{"type": "string"}}},
		deploy.ComponentMount{Name: "optional", ComponentID: "optional"},
	)
	if value, present, err := resolveComponentArgs(optional, nil); err != nil || present || value != nil {
		t.Fatalf("missing optional must remain JS undefined: %#v %v %v", value, present, err)
	}
}

func TestMissingDeclaredComponentEnvFailsRuntimeContext(t *testing.T) {
	const variable = "PBVEX_COMPONENT_TEST_MISSING_TOKEN"
	t.Setenv(variable, "")
	t.Setenv(variable, "present")
	// Unset is materially different from an explicitly empty string.
	t.Setenv(variable, "")
	definition := deploy.ComponentDefinition{ComponentID: "secrets", Env: map[string]deploy.EnvArgDescriptor{"TOKEN": {Type: "envVar", Name: variable}}}
	namespace := testNamespace(t, definition, deploy.ComponentMount{Name: "secrets", ComponentID: "secrets"})
	if env, err := resolveComponentEnv(namespace); err != nil || env["TOKEN"] != "" {
		t.Fatalf("explicit empty env must resolve: %#v %v", env, err)
	}

	missing := strings.ReplaceAll(variable, "TOKEN", "ABSENT")
	definition.Env["TOKEN"] = deploy.EnvArgDescriptor{Type: "envVar", Name: missing}
	namespace = testNamespace(t, definition, deploy.ComponentMount{Name: "secrets", ComponentID: "secrets"})
	if _, err := resolveComponentEnv(namespace); err == nil || !strings.Contains(err.Error(), `env "TOKEN" requires unset variable`) {
		t.Fatalf("expected precise missing env error, got %v", err)
	}
}

func TestResolveMissingObjectRequiresEveryFieldToAcceptMissing(t *testing.T) {
	optional := map[string]any{"type": "object", "shape": map[string]any{"value": map[string]any{"type": "optional", "validator": map[string]any{"type": "string"}}}}
	if value, ok := resolveMissingComponentArg(optional); !ok || value == nil {
		t.Fatalf("all-optional object should resolve: %#v %v", value, ok)
	}
	required := map[string]any{"type": "object", "shape": map[string]any{"value": map[string]any{"type": "string"}}}
	if _, ok := resolveMissingComponentArg(required); ok {
		t.Fatal("required object must not accept missing")
	}
}

func TestComponentNamespaceIsPathDerivedAndUpgradeStable(t *testing.T) {
	first, err := deploy.ComponentNamespaceID("counter")
	if err != nil {
		t.Fatal(err)
	}
	second, err := deploy.ComponentNamespaceID("counter")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, "cmp_") || len(first) != 56 {
		t.Fatalf("unexpected stable namespace %q / %q", first, second)
	}
	if renamed, _ := deploy.ComponentNamespaceID("counter2"); renamed == first {
		t.Fatal("renamed mount must get a new namespace")
	}
}

func TestSameDefinitionMountedTwiceGetsIsolatedCollections(t *testing.T) {
	definition := deploy.ComponentDefinition{ComponentID: "shared", Schema: map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{}}}}}
	graph := &deploy.ComponentGraph{Definitions: []deploy.ComponentDefinition{definition}, Mounts: []deploy.ComponentMount{
		{Name: "left", ComponentID: "shared"}, {Name: "right", ComponentID: "shared"},
	}}
	namespaces, err := deploy.ComponentNamespaces(graph)
	if err != nil {
		t.Fatal(err)
	}
	left, right := namespaces["left"], namespaces["right"]
	if left.ID == right.ID || left.PhysicalByTable["items"] == right.PhysicalByTable["items"] {
		t.Fatalf("repeated mounts shared storage: %#v / %#v", left, right)
	}
}

func TestComponentNamespaceUpgradeKeepsPhysicalCollections(t *testing.T) {
	schema := map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{}}}}
	build := func(componentID string) deploy.ComponentNamespace {
		definition := deploy.ComponentDefinition{ComponentID: componentID, Schema: schema}
		return testNamespace(t, definition, deploy.ComponentMount{Name: "counter", ComponentID: componentID})
	}
	oldNamespace, upgradedNamespace := build("oldDefinition"), build("newDefinition")
	if oldNamespace.ID != upgradedNamespace.ID || oldNamespace.PhysicalByTable["items"] != upgradedNamespace.PhysicalByTable["items"] {
		t.Fatalf("same-path upgrade changed storage: %#v / %#v", oldNamespace, upgradedNamespace)
	}
}

func TestComponentNamespaceCatalogHandlesNesting(t *testing.T) {
	definitions := []deploy.ComponentDefinition{{ComponentID: "parent"}, {ComponentID: "child"}}
	graph := &deploy.ComponentGraph{Definitions: definitions, Mounts: []deploy.ComponentMount{{Name: "parent", ComponentID: "parent", Children: []deploy.ComponentMount{{Name: "child", ComponentID: "child"}}}}}
	namespaces, err := deploy.ComponentNamespaces(graph)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := namespaces["parent"]; !ok {
		t.Fatal("missing parent namespace")
	}
	if _, ok := namespaces["parent/child"]; !ok {
		t.Fatal("missing nested namespace")
	}
	if namespaces["parent"].ID == namespaces["parent/child"].ID {
		t.Fatal("nested namespace collision")
	}
}

func TestComponentNestedInvocationPreservesAuthAndStorageNamespace(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}

	componentSchema := map[string]any{"tables": []any{map[string]any{
		"tableName": "items",
		"fields":    map[string]any{"owner": map[string]any{"type": "string"}},
	}}}
	definition := deploy.ComponentDefinition{
		ComponentID: "widget", ModulePaths: []string{"store.ts"}, Schema: componentSchema,
	}
	graph := &deploy.ComponentGraph{
		Definitions: []deploy.ComponentDefinition{definition},
		Mounts:      []deploy.ComponentMount{{Name: "widget", ComponentID: "widget"}},
	}
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

	descriptors := []deploy.FunctionDescriptor{
		{Name: "write", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/components/widget/store.ts", ExportName: "write"},
		{Name: "read", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/components/widget/store.ts", ExportName: "read"},
		{Name: "outer", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "pbvex/root.ts", ExportName: "outer"},
	}
	bundle := `(function(){
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"internal",modulePath:"pbvex/components/widget/store.ts",exportName:"write"}, async function(ctx){ const user=await ctx.auth.getUserIdentity(); return ctx.db.insert("items",{owner:user.subject}); });
__pbvex.registerFunction({name:"read",type:"query",visibility:"internal",modulePath:"pbvex/components/widget/store.ts",exportName:"read"}, async function(ctx,args){ const user=await ctx.auth.getUserIdentity(); const doc=ctx.db.get(args.id); return {owner:doc.owner,subject:user.subject,token:user.tokenIdentifier}; });
__pbvex.registerFunction({name:"outer",type:"action",visibility:"public",modulePath:"pbvex/root.ts",exportName:"outer"}, async function(ctx){ const id=await ctx.runMutation("write",{}); return ctx.runQuery("read",{id:id}); });
})();`
	manager := NewManager(DefaultConfig())
	if err := manager.Compile("components-auth", bundle, descriptors, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	manifest := deploy.DeploymentManifest{DeploymentID: "components-auth", Functions: descriptors, Components: graph}
	identity := &auth.UserIdentity{Subject: "user-1", TokenIdentifier: "pocketbase:users:user-1", Issuer: "pocketbase:users"}
	result, err := manager.InvokeWithDatabase(context.Background(), "components-auth", "outer", map[string]any{}, identity, "request-1", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(map[string]any)
	if !ok || got["owner"] != "user-1" || got["subject"] != "user-1" || got["token"] != "pocketbase:users:user-1" {
		t.Fatalf("component nested invocation lost auth or namespace: %#v", result)
	}
	if count, err := backingRecordCountForTest(app, physical); err != nil || count != 1 {
		t.Fatalf("component write did not use its physical collection: count=%d err=%v", count, err)
	}
}

func backingRecordCountForTest(app core.App, collection string) (int, error) {
	records, err := app.FindAllRecords(collection)
	return len(records), err
}
