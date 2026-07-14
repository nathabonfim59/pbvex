package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestDatabaseMutationAndQueryAreRequestScoped(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("messages")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	col.AddIndex("idx_pbvex_messages_by_body", false, "json_extract(_pbvex_order, '$.body'), created, id", "")
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	other := core.NewBaseCollection("other")
	other.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	other.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	other.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	if err := app.Save(other); err != nil {
		t.Fatal(err)
	}
	manifest := deploy.DeploymentManifest{Schema: map[string]any{"tables": []any{map[string]any{"tableName": "messages", "fields": map[string]any{"body": map[string]any{"type": "string"}, "n": map[string]any{"type": "number"}}, "indexes": []any{map[string]any{"name": "by_body", "fields": []any{"body"}}}}, map[string]any{"tableName": "other", "fields": map[string]any{}, "indexes": []any{}}}}}
	desc := []deploy.FunctionDescriptor{
		{Name: "write", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "write"},
		{Name: "read", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "read"},
		{Name: "terminals", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "terminals"},
		{Name: "indexed", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "indexed"},
		{Name: "fullIndexed", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "fullIndexed"},
		{Name: "ranged", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "ranged"},
		{Name: "normalize", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "normalize"},
		{Name: "systemID", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "systemID"},
		{Name: "systemIDRange", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "systemIDRange"},
		{Name: "systemTimeEq", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "systemTimeEq"},
		{Name: "systemTimeRange", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "systemTimeRange"},
		{Name: "badIndex", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "badIndex"},
		{Name: "badRange", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "badRange"},
		{Name: "duplicatePlan", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "duplicatePlan"},
		{Name: "badMath", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "badMath"},
		{Name: "modMath", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "modMath"},
		{Name: "literalAnd", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "literalAnd"},
		{Name: "badAnd", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "badAnd"},
		{Name: "patchDoc", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "patchDoc"},
		{Name: "replaceDoc", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "replaceDoc"},
		{Name: "deleteDoc", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "deleteDoc"},
		{Name: "bad", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "bad"},
		{Name: "reject", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "reject"},
		{Name: "badDocument", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "badDocument"},
		{Name: "readOnly", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "readOnly"},
		{Name: "action", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "action"},
	}
	bundle := `
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"x",exportName:"write"}, function(ctx) { ctx.db.insert("messages", {body:"hello",n:1}); ctx.db.insert("messages", {body:"other",n:2}); return ctx.db.insert("messages", {body:"third",n:3}); });
__pbvex.registerFunction({name:"read",type:"query",visibility:"public",modulePath:"x",exportName:"read"}, function(ctx) { return ctx.db.query("messages").filter(q=>q.eq(q.field("body"),q.literal("hello"))).collect(); });
__pbvex.registerFunction({name:"terminals",type:"query",visibility:"public",modulePath:"x",exportName:"terminals"}, function(ctx) { const q=ctx.db.query("messages"); const p=q.paginate({numItems:2}); return {take:q.take(2),first:q.first(),unique:q.filter(e=>e.eq(e.field("body"),e.literal("hello"))).unique(),page:p, rest:q.paginate({numItems:2,cursor:p.continueCursor})}; });
__pbvex.registerFunction({name:"indexed",type:"query",visibility:"public",modulePath:"x",exportName:"indexed"}, function(ctx) { return ctx.db.query("messages").withIndex("by_body", r=>r.eq("body","hello")).collect(); });
__pbvex.registerFunction({name:"fullIndexed",type:"query",visibility:"public",modulePath:"x",exportName:"fullIndexed"}, function(ctx) { return ctx.db.query("messages").filter(q=>q.eq(q.field("body"),q.literal("hello"))).withIndex("by_body").collect(); });
__pbvex.registerFunction({name:"ranged",type:"query",visibility:"public",modulePath:"x",exportName:"ranged"}, function(ctx) { return ctx.db.query("messages").withIndex("by_body", r=>r.gte("body","h")).collect(); });
__pbvex.registerFunction({name:"normalize",type:"query",visibility:"public",modulePath:"x",exportName:"normalize"}, function(ctx,args) { return ctx.db.normalizeId("messages",args.id); });
__pbvex.registerFunction({name:"systemID",type:"query",visibility:"public",modulePath:"x",exportName:"systemID"}, function(ctx,args) { return ctx.db.query("messages").filter(q=>q.eq(q.field("_id"),q.literal(args.id))).unique(); });
__pbvex.registerFunction({name:"systemIDRange",type:"query",visibility:"public",modulePath:"x",exportName:"systemIDRange"}, function(ctx,args) { return ctx.db.query("messages").filter(q=>q.gte(q.field("_id"),q.literal(args.id))).collect(); });
__pbvex.registerFunction({name:"systemTimeEq",type:"query",visibility:"public",modulePath:"x",exportName:"systemTimeEq"}, function(ctx,args) { return ctx.db.query("messages").filter(q=>q.eq(q.field("_creationTime"),q.literal(args.ms))).collect(); });
__pbvex.registerFunction({name:"systemTimeRange",type:"query",visibility:"public",modulePath:"x",exportName:"systemTimeRange"}, function(ctx,args) { return ctx.db.query("messages").filter(q=>q.gte(q.field("_creationTime"),q.literal(args.ms))).collect(); });
__pbvex.registerFunction({name:"badIndex",type:"query",visibility:"public",modulePath:"x",exportName:"badIndex"}, function(ctx) { return ctx.db.query("messages").withIndex("by_body", r=>r.eq("n",1)).collect(); });
__pbvex.registerFunction({name:"badRange",type:"query",visibility:"public",modulePath:"x",exportName:"badRange"}, function(ctx) { return ctx.db.query("messages").withIndex("by_body", r=>r.gte("body","a").gt("body","b")).collect(); });
__pbvex.registerFunction({name:"duplicatePlan",type:"query",visibility:"public",modulePath:"x",exportName:"duplicatePlan"}, function(ctx) { return ctx.db.query("messages").withIndex("by_body").withIndex("by_body").order("asc").order("desc").collect(); });
__pbvex.registerFunction({name:"badMath",type:"query",visibility:"public",modulePath:"x",exportName:"badMath"}, function(ctx) { return ctx.db.query("messages").filter(q=>q.eq(q.div(q.literal(1),q.literal(0)),q.literal(1))).collect(); });
__pbvex.registerFunction({name:"modMath",type:"query",visibility:"public",modulePath:"x",exportName:"modMath"}, function(ctx) { return ctx.db.query("messages").filter(q=>q.and(q.eq(q.mod(q.literal(5.5),q.literal(2)),q.literal(1.5)),q.eq(q.literal(1),q.literal(1)))).collect(); });
__pbvex.registerFunction({name:"literalAnd",type:"query",visibility:"public",modulePath:"x",exportName:"literalAnd"}, function(ctx) { return ctx.db.query("messages").filter(q=>q.and(q.literal(true),q.literal(true))).take(1); });
__pbvex.registerFunction({name:"badAnd",type:"query",visibility:"public",modulePath:"x",exportName:"badAnd"}, function(ctx) { return ctx.db.query("messages").filter(q=>q.and(q.literal(1),q.literal(true))).collect(); });
__pbvex.registerFunction({name:"patchDoc",type:"mutation",visibility:"public",modulePath:"x",exportName:"patchDoc"}, function(ctx,args) { ctx.db.patch(args.id,{body:"patched"}); return ctx.db.get(args.id); });
__pbvex.registerFunction({name:"replaceDoc",type:"mutation",visibility:"public",modulePath:"x",exportName:"replaceDoc"}, function(ctx,args) { ctx.db.replace(args.id,{body:"replaced",n:9}); return ctx.db.get(args.id); });
__pbvex.registerFunction({name:"deleteDoc",type:"mutation",visibility:"public",modulePath:"x",exportName:"deleteDoc"}, function(ctx,args) { ctx.db.delete(args.id); return ctx.db.get(args.id); });
__pbvex.registerFunction({name:"bad",type:"mutation",visibility:"public",modulePath:"x",exportName:"bad"}, function(ctx) { ctx.db.insert("messages", {body:"rollback",n:2}); throw new Error("no"); });
__pbvex.registerFunction({name:"reject",type:"mutation",visibility:"public",modulePath:"x",exportName:"reject"}, function(ctx) { ctx.db.insert("messages", {body:"rejected",n:2}); return Promise.reject(new Error("no")); });
__pbvex.registerFunction({name:"badDocument",type:"mutation",visibility:"public",modulePath:"x",exportName:"badDocument"}, function(ctx) { ctx.db.insert("messages", {body:"host rollback",n:2}); return ctx.db.insert("messages", {body:1,n:2}); });
__pbvex.registerFunction({name:"readOnly",type:"query",visibility:"public",modulePath:"x",exportName:"readOnly"}, function(ctx) { return typeof ctx.db.insert; });
__pbvex.registerFunction({name:"action",type:"action",visibility:"public",modulePath:"x",exportName:"action"}, function(ctx) { return typeof ctx.db; });`
	m := NewManager(DefaultConfig())
	if err := m.Compile("d", bundle, desc, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	written, err := m.InvokeWithDatabase(context.Background(), "d", "write", nil, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := m.InvokeWithDatabase(context.Background(), "d", "normalize", map[string]any{"id": written}, nil, "", app, manifest); err != nil || got != written {
		t.Fatalf("normalizeId got %#v, err %v", got, err)
	}
	if got, err := m.InvokeWithDatabase(context.Background(), "d", "systemID", map[string]any{"id": written}, nil, "", app, manifest); err != nil || got.(map[string]any)["_id"] != written {
		t.Fatalf("system id equality %#v, %v", got, err)
	}
	if got, err := m.InvokeWithDatabase(context.Background(), "d", "systemIDRange", map[string]any{"id": written}, nil, "", app, manifest); err != nil || len(got.([]any)) == 0 {
		t.Fatalf("system id range %#v, %v", got, err)
	}
	// A table-aware opaque id cannot be normalized to another declared table.
	vm := goja.New()
	db := &database{vm: vm, ctx: context.Background(), app: app, manifest: manifest}
	if value := db.normalizeID(goja.FunctionCall{Arguments: []goja.Value{vm.ToValue("other"), vm.ToValue(written)}}); !goja.IsNull(value) {
		t.Fatalf("wrong-table id normalized: %v", value)
	}
	if value := db.normalizeID(goja.FunctionCall{Arguments: []goja.Value{vm.ToValue("messages"), vm.ToValue("raw")}}); !goja.IsNull(value) {
		t.Fatalf("malformed id normalized: %v", value)
	}
	got, err := m.InvokeWithDatabase(context.Background(), "d", "read", nil, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := got.([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected query result %#v", got)
	}
	creation, ok := finiteNumber(items[0].(map[string]any)["_creationTime"])
	if !ok {
		t.Fatalf("unexpected creation time %#v", items[0])
	}
	if got, err := m.InvokeWithDatabase(context.Background(), "d", "systemTimeEq", map[string]any{"ms": creation}, nil, "", app, manifest); err != nil || len(got.([]any)) < 1 {
		t.Fatalf("system creationTime equality %#v, %v", got, err)
	}
	if got, err := m.InvokeWithDatabase(context.Background(), "d", "systemTimeRange", map[string]any{"ms": creation}, nil, "", app, manifest); err != nil || len(got.([]any)) != 3 {
		t.Fatalf("system creationTime range %#v, %v", got, err)
	}
	got, err = m.InvokeWithDatabase(context.Background(), "d", "terminals", nil, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	terminals := got.(map[string]any)
	if len(terminals["take"].([]any)) != 2 || terminals["unique"] == nil {
		t.Fatalf("terminal query result %#v", terminals)
	}
	if terminals["page"].(map[string]any)["isDone"] != false || terminals["rest"].(map[string]any)["isDone"] != true {
		t.Fatalf("pagination result %#v", terminals)
	}
	pageRows := terminals["page"].(map[string]any)["page"].([]any)
	restRows := terminals["rest"].(map[string]any)["page"].([]any)
	if pageRows[0].(map[string]any)["_id"] == restRows[0].(map[string]any)["_id"] {
		t.Fatalf("keyset repeated a row %#v", terminals)
	}
	got, err = m.InvokeWithDatabase(context.Background(), "d", "indexed", nil, nil, "", app, manifest)
	if err != nil || len(got.([]any)) != 1 {
		t.Fatalf("index query %#v %v", got, err)
	}
	got, err = m.InvokeWithDatabase(context.Background(), "d", "fullIndexed", nil, nil, "", app, manifest)
	if err != nil || len(got.([]any)) != 1 {
		t.Fatalf("full index query %#v %v", got, err)
	}
	got, err = m.InvokeWithDatabase(context.Background(), "d", "ranged", nil, nil, "", app, manifest)
	if err != nil || len(got.([]any)) != 3 {
		t.Fatalf("index range %#v %v", got, err)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "d", "badIndex", nil, nil, "", app, manifest); err == nil {
		t.Fatal("bad index field accepted")
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "d", "badRange", nil, nil, "", app, manifest); err == nil {
		t.Fatal("duplicate range bound accepted")
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "d", "duplicatePlan", nil, nil, "", app, manifest); err == nil {
		t.Fatal("multiple index/order selections accepted")
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "d", "badMath", nil, nil, "", app, manifest); err == nil {
		t.Fatal("division by zero accepted")
	}
	if got, err := m.InvokeWithDatabase(context.Background(), "d", "modMath", nil, nil, "", app, manifest); err != nil || len(got.([]any)) != 3 {
		t.Fatalf("floating modulo %#v %v", got, err)
	}
	if got, err := m.InvokeWithDatabase(context.Background(), "d", "literalAnd", nil, nil, "", app, manifest); err != nil || len(got.([]any)) != 1 {
		t.Fatalf("boolean literal expressions %#v %v", got, err)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "d", "badAnd", nil, nil, "", app, manifest); err == nil {
		t.Fatal("non-expression boolean operands accepted")
	}
	patched, err := m.InvokeWithDatabase(context.Background(), "d", "patchDoc", map[string]any{"id": written}, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if patched.(map[string]any)["n"] == nil || patched.(map[string]any)["body"] != "patched" {
		t.Fatalf("patch semantics %#v", patched)
	}
	replaced, err := m.InvokeWithDatabase(context.Background(), "d", "replaceDoc", map[string]any{"id": written}, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.(map[string]any)["body"] != "replaced" || replaced.(map[string]any)["n"] != int64(9) {
		t.Fatalf("replace semantics %#v", replaced)
	}
	deleted, err := m.InvokeWithDatabase(context.Background(), "d", "deleteDoc", map[string]any{"id": written}, nil, "", app, manifest)
	if err != nil || deleted != nil {
		t.Fatalf("delete semantics %#v %v", deleted, err)
	}
	if value := db.normalizeID(goja.FunctionCall{Arguments: []goja.Value{vm.ToValue("messages"), vm.ToValue(written)}}); value.String() != written {
		t.Fatalf("deleted id lost its canonical form: %v", value)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "d", "bad", nil, nil, "", app, manifest); err == nil {
		t.Fatal("expected throw")
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "d", "reject", nil, nil, "", app, manifest); err == nil {
		t.Fatal("expected rejected promise")
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "d", "badDocument", nil, nil, "", app, manifest); err == nil {
		t.Fatal("expected host validation failure")
	}
	got, err = m.InvokeWithDatabase(context.Background(), "d", "read", nil, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	items = got.([]any)
	if len(items) != 1 {
		t.Fatalf("rollback failed: %#v", got)
	}
	got, err = m.InvokeWithDatabase(context.Background(), "d", "readOnly", nil, nil, "", app, manifest)
	if err != nil || got != "undefined" {
		t.Fatalf("query write capability %#v %v", got, err)
	}
	got, err = m.InvokeWithDatabase(context.Background(), "d", "action", nil, nil, "", app, manifest)
	if err != nil || got != "undefined" {
		t.Fatalf("action ctx: %#v %v", got, err)
	}
}

func TestProtocolValidatorValues(t *testing.T) {
	id := structuralID("messages", "abcdefghijklmnop"[:15])
	cases := []struct {
		name             string
		validator, value any
		want             bool
	}{
		{"id target", map[string]any{"type": "id", "tableName": "messages"}, id, true},
		{"id wrong target", map[string]any{"type": "id", "tableName": "other"}, id, false},
		{"id malformed", map[string]any{"type": "id", "tableName": "messages"}, "raw", false},
		{"optional does not mean null", map[string]any{"type": "optional", "validator": map[string]any{"type": "string"}}, nil, false},
		{"null", map[string]any{"type": "null"}, nil, true},
		{"finite", map[string]any{"type": "number"}, float64(1), true},
		{"nan", map[string]any{"type": "number"}, math.NaN(), false},
		{"canonical integer", map[string]any{"type": "int64"}, map[string]any{"$integer": "AAAAAAAAAAA="}, true},
		{"noncanonical integer", map[string]any{"type": "int64"}, map[string]any{"$integer": "AAAAAAAAAAA"}, false},
		{"record key", map[string]any{"type": "record", "key": map[string]any{"type": "string"}, "value": map[string]any{"type": "boolean"}}, map[string]any{"x": true}, true},
		{"record bad key", map[string]any{"type": "record", "key": map[string]any{"type": "literal", "value": "x"}, "value": map[string]any{"type": "boolean"}}, map[string]any{"y": true}, false},
		{"malformed node", map[string]any{"type": "string", "extra": true}, "x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateValue(tc.validator, tc.value); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestDefaultedValidatorsTransformFunctionArgumentsAndDocuments(t *testing.T) {
	argsValidator := map[string]any{"type": "object", "shape": map[string]any{
		"name":    map[string]any{"type": "string"},
		"channel": map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "general"},
		"note":    map[string]any{"type": "optional", "validator": map[string]any{"type": "string"}},
	}}
	normalized, err := normalizeValueWithID(argsValidator, map[string]any{"name": "Ada"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := normalized.(map[string]any)
	if got["name"] != "Ada" || got["channel"] != "general" {
		t.Fatalf("default was not transformed: %#v", got)
	}
	if _, exists := got["note"]; exists {
		t.Fatalf("optional omission became a value: %#v", got)
	}
	if _, err := normalizeValueWithID(map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": float64(1)}, nil, nil); err == nil {
		t.Fatal("invalid default value was accepted")
	}

	table := map[string]any{"fields": map[string]any{
		"name":    map[string]any{"type": "string"},
		"channel": map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "general"},
	}}
	doc, err := normalizeDocumentWithID(table, map[string]any{"name": "Ada"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if doc["channel"] != "general" {
		t.Fatalf("document default was not transformed before storage: %#v", doc)
	}

	desc := []deploy.FunctionDescriptor{{
		Name: "defaults", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "defaults", Args: argsValidator, Returns: argsValidator,
	}}
	bundle := `__pbvex.registerFunction({name:"defaults",type:"query",visibility:"public",modulePath:"x",exportName:"defaults",args:{type:"object",shape:{name:{type:"string"},channel:{type:"defaulted",validator:{type:"string"},defaultValue:"general"},note:{type:"optional",validator:{type:"string"}}}},returns:{type:"object",shape:{name:{type:"string"},channel:{type:"defaulted",validator:{type:"string"},defaultValue:"general"},note:{type:"optional",validator:{type:"string"}}}}},function(_ctx,args){return {name:args.name,channel:args.channel}});`
	m := NewManager(DefaultConfig())
	if err := m.Compile("defaults", bundle, desc, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	result, err := m.Invoke(context.Background(), "defaults", "defaults", map[string]any{"name": "Ada"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["channel"] != "general" {
		t.Fatalf("handler did not receive transformed arguments: %#v", result)
	}
}

func TestValidatorEvaluatorRejectsMalformedAndCyclicNodesWithoutPanicking(t *testing.T) {
	validator := map[string]any{"type": "object", "shape": map[string]any{
		"nested": map[string]any{"type": "array", "item": map[string]any{"type": "record", "key": map[string]any{"type": "string"}, "value": map[string]any{"type": "union", "validators": []any{
			map[string]any{"type": "number"}, map[string]any{"type": "bytes"},
		}}}},
		"optional": map[string]any{"type": "optional", "validator": map[string]any{"type": "string"}},
	}}
	valid := map[string]any{"nested": []any{map[string]any{"a": float64(1), "b": map[string]any{"$bytes": "AP8="}}}}
	if !validateValue(validator, valid) {
		t.Fatal("nested validator rejected canonical wire values")
	}
	if validateValue(validator, map[string]any{"nested": []any{}, "optional": nil}) {
		t.Fatal("optional accepted explicit null")
	}
	if validateValue(validator, map[string]any{"nested": []any{map[string]any{"a": math.Inf(1)}}}) {
		t.Fatal("non-finite nested value accepted")
	}
	cyclicValidator := map[string]any{"type": "optional"}
	cyclicValidator["validator"] = cyclicValidator
	if validateValue(cyclicValidator, "x") {
		t.Fatal("cyclic validator accepted")
	}
	cyclicValue := map[string]any{}
	cyclicValue["self"] = cyclicValue
	if validateValue(map[string]any{"type": "any"}, cyclicValue) {
		t.Fatal("cyclic value accepted")
	}
	deep := any("x")
	for i := 0; i < maxWireDepth+2; i++ {
		deep = []any{deep}
	}
	deepValidator := any(map[string]any{"type": "string"})
	for i := 0; i < maxWireDepth+2; i++ {
		deepValidator = map[string]any{"type": "array", "item": deepValidator}
	}
	if validateValue(deepValidator, deep) {
		t.Fatal("excessive validator/value depth accepted")
	}
}

func structuralID(table, raw string) string {
	payload, _ := json.Marshal(opaqueIDPayload{V: 1, K: 1, N: "test", T: table, R: raw})
	return "pbv1.1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
}

func TestMutationRollsBackInvalidReturn(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("items")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	manifest := deploy.DeploymentManifest{DeploymentID: "rollback", Config: &deploy.DeploymentConfig{MaxReturnValueBytes: 5}, Schema: map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{"name": map[string]any{"type": "string"}}}}}}
	desc := []deploy.FunctionDescriptor{
		{Name: "badReturn", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "badReturn", Returns: map[string]any{"type": "number"}},
		{Name: "badID", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "badID", Returns: map[string]any{"type": "id", "tableName": "other"}},
		{Name: "oversize", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "oversize"},
		{Name: "cyclic", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "cyclic"},
		{Name: "args", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "args", Args: map[string]any{"type": "string"}},
	}
	bundle := `
__pbvex.registerFunction({name:"badReturn",type:"mutation",visibility:"public",modulePath:"x",exportName:"badReturn",returns:{type:"number"}},function(ctx){ctx.db.insert("items",{name:"rolled back"});return "wrong"});
__pbvex.registerFunction({name:"badID",type:"mutation",visibility:"public",modulePath:"x",exportName:"badID",returns:{type:"id",tableName:"other"}},function(ctx){return ctx.db.insert("items",{name:"wrong table"})._id});
__pbvex.registerFunction({name:"oversize",type:"mutation",visibility:"public",modulePath:"x",exportName:"oversize"},function(ctx){ctx.db.insert("items",{name:"also rolled back"});return "too long"});
__pbvex.registerFunction({name:"cyclic",type:"mutation",visibility:"public",modulePath:"x",exportName:"cyclic"},function(ctx){ctx.db.insert("items",{name:"cyclic rolled back"});const out={};out.self=out;return out});
__pbvex.registerFunction({name:"args",type:"mutation",visibility:"public",modulePath:"x",exportName:"args",args:{type:"string"}},function(ctx){ctx.db.insert("items",{name:"args rolled back"});return null});`
	m := NewManager(DefaultConfig())
	if err := m.Compile("rollback", bundle, desc, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "rollback", "badReturn", nil, nil, "", app, manifest); err == nil {
		t.Fatal("invalid return committed")
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "rollback", "badID", nil, nil, "", app, manifest); err == nil {
		t.Fatal("wrong-table id return committed")
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "rollback", "oversize", nil, nil, "", app, manifest); err == nil {
		t.Fatal("oversized return committed")
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "rollback", "cyclic", nil, nil, "", app, manifest); err == nil {
		t.Fatal("unencodable return committed")
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "rollback", "args", float64(1), nil, "", app, manifest); err == nil {
		t.Fatal("invalid arguments entered mutation")
	}
	rows, err := app.FindRecordsByFilter("items", "", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rollback failed: %#v", rows)
	}
}

func TestActionNestedMutationTransactionBoundary(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("items")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	manifest := deploy.DeploymentManifest{DeploymentID: "nested-tx", Schema: map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{"name": map[string]any{"type": "string"}}}}}}
	number := map[string]any{"type": "number"}
	descriptors := []deploy.FunctionDescriptor{
		{Name: "throws", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "throws"},
		{Name: "invalid", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "invalid", Returns: number},
		{Name: "valid", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "valid", Returns: number},
		{Name: "runThrows", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "runThrows"},
		{Name: "runInvalid", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "runInvalid"},
		{Name: "runValid", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "runValid"},
	}
	bundle := `
__pbvex.registerFunction({name:"throws",type:"mutation",visibility:"public",modulePath:"x",exportName:"throws"},function(ctx){ctx.db.insert("items",{name:"throw"});throw new Error("boom")});
__pbvex.registerFunction({name:"invalid",type:"mutation",visibility:"public",modulePath:"x",exportName:"invalid",returns:{type:"number"}},function(ctx){ctx.db.insert("items",{name:"invalid"});return "wrong"});
__pbvex.registerFunction({name:"valid",type:"mutation",visibility:"public",modulePath:"x",exportName:"valid",returns:{type:"number"}},function(ctx){ctx.db.insert("items",{name:"valid"});return 1});
__pbvex.registerFunction({name:"runThrows",type:"action",visibility:"public",modulePath:"x",exportName:"runThrows"},function(ctx){return ctx.runMutation("throws",{})});
__pbvex.registerFunction({name:"runInvalid",type:"action",visibility:"public",modulePath:"x",exportName:"runInvalid"},function(ctx){return ctx.runMutation("invalid",{})});
__pbvex.registerFunction({name:"runValid",type:"action",visibility:"public",modulePath:"x",exportName:"runValid"},function(ctx){return ctx.runMutation("valid",{})});`
	m := NewManager(DefaultConfig())
	if err := m.Compile("nested-tx", bundle, descriptors, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runThrows", "runInvalid"} {
		if _, err := m.InvokeWithDatabase(context.Background(), "nested-tx", name, nil, nil, "", app, manifest); err == nil {
			t.Fatalf("%s unexpectedly succeeded", name)
		}
	}
	rows, err := app.FindRecordsByFilter("items", "", "", 10, 0)
	if err != nil || len(rows) != 0 {
		t.Fatalf("failed nested mutation committed: rows=%d err=%v", len(rows), err)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "nested-tx", "runValid", nil, nil, "", app, manifest); err != nil {
		t.Fatal(err)
	}
	rows, err = app.FindRecordsByFilter("items", "", "", 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("valid nested mutation did not commit: rows=%d err=%v", len(rows), err)
	}
}

func TestCursorSurvivesOneKeyRotation(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	db := &database{vm: goja.New(), ctx: context.Background(), app: app}
	binding := strings.Repeat("a", base64.RawURLEncoding.EncodedLen(32))
	cursor := db.encodeCursor(binding, &cursorState{Tuple: []any{"x"}, ID: "abcdefghijklmnop"[:15]})
	id, err := db.encodeID("items", "abcdefghijklmnop"[:15])
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a capability issued by the pre-durable-ID implementation, when
	// the cursor root signed ids. Bootstrap persists that root separately as a
	// migration anchor, so it must not fall out of current/previous retention.
	ring, err := db.loadKeyRing()
	if err != nil {
		t.Fatal(err)
	}
	legacyPayload, err := json.Marshal(opaqueIDPayload{V: 1, K: ring.currentID, N: ring.legacyNamespace, T: "items", R: "abcdefghijklmno"})
	if err != nil {
		t.Fatal(err)
	}
	legacyID := "pbv1." + fmt.Sprint(ring.currentID) + "." + base64.RawURLEncoding.EncodeToString(legacyPayload) + "." + base64.RawURLEncoding.EncodeToString(idMAC(ring.current, legacyPayload))
	state, err := app.FindFirstRecordByFilter(schema.CollectionSchemaState, "key = {:key}", map[string]any{"key": schema.StateKeyActive})
	if err != nil {
		t.Fatal(err)
	}
	old := state.GetString(schema.FieldCursorSecret)
	state.Set(schema.FieldCursorPreviousSecret, old)
	state.Set(schema.FieldCursorSecret, base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	state.Set(schema.FieldCursorKeyID, 2)
	if err := app.Save(state); err != nil {
		t.Fatal(err)
	}
	fresh := &database{vm: goja.New(), ctx: context.Background(), app: app}
	if got := fresh.decodeCursor(cursor, binding); got.ID != "abcdefghijklmnop"[:15] {
		t.Fatalf("rotation cursor %#v", got)
	}
	if payload, err := fresh.verifyID(id); err != nil || payload.T != "items" {
		t.Fatalf("rotation id %#v, %v", payload, err)
	}
	if payload, err := fresh.verifyID(legacyID); err != nil || payload.T != "items" {
		t.Fatalf("first rotation legacy id %#v, %v", payload, err)
	}
	// Cursor keys deliberately retain only current+previous tokens, while the
	// independent persisted ID root keeps durable capabilities valid across any
	// number of future cursor rotations and a fresh database instance.
	state, err = app.FindFirstRecordByFilter(schema.CollectionSchemaState, "key = {:key}", map[string]any{"key": schema.StateKeyActive})
	if err != nil {
		t.Fatal(err)
	}
	for keyID := 3; keyID <= 7; keyID++ {
		state.Set(schema.FieldCursorPreviousSecret, state.GetString(schema.FieldCursorSecret))
		state.Set(schema.FieldCursorSecret, base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(keyID)}, 32)))
		state.Set(schema.FieldCursorKeyID, keyID)
		state.Set(schema.FieldIDKeyID, keyID)
		if err := app.Save(state); err != nil {
			t.Fatal(err)
		}
	}
	reopened := &database{vm: goja.New(), ctx: context.Background(), app: app}
	if payload, err := reopened.verifyID(id); err != nil || payload.T != "items" {
		t.Fatalf("rotated durable id %#v, %v", payload, err)
	}
	if payload, err := reopened.verifyID(legacyID); err != nil || payload.T != "items" {
		t.Fatalf("rotated legacy id %#v, %v", payload, err)
	}
	rotatedID, err := reopened.encodeID("items", "ponmlkjihgfedcb")
	if err != nil {
		t.Fatal(err)
	}
	rotatedPayload, _, _, ok := parseOpaqueID(rotatedID)
	if !ok || rotatedPayload.K != 7 {
		t.Fatalf("active id version was not persisted: %#v", rotatedPayload)
	}
	if payload, err := reopened.verifyID(rotatedID); err != nil || payload.R != "ponmlkjihgfedcb" {
		t.Fatalf("derived rotated id %#v, %v", payload, err)
	}
	mustPanic(t, func() { reopened.decodeCursor(cursor, binding) })
}

func TestQueryHashBindsInvocationPlan(t *testing.T) {
	db := &database{manifest: deploy.DeploymentManifest{DeploymentID: "d1"}, function: "one", args: `{"x":1}`}
	q := &queryBuilder{db: db, table: "messages", predicate: &expression{op: "eq", args: []*expression{{op: "field", field: "body"}, {op: "literal", value: "x"}}}}
	base := queryHash(q, 10)
	variants := []*queryBuilder{
		{db: &database{manifest: deploy.DeploymentManifest{DeploymentID: "d2"}, function: "one", args: `{"x":1}`}, table: "messages", predicate: q.predicate},
		{db: &database{manifest: deploy.DeploymentManifest{DeploymentID: "d1"}, function: "two", args: `{"x":1}`}, table: "messages", predicate: q.predicate},
		{db: &database{manifest: deploy.DeploymentManifest{DeploymentID: "d1"}, function: "one", args: `{"x":2}`}, table: "messages", predicate: q.predicate},
		{db: db, table: "messages", predicate: q.predicate, descending: true},
	}
	for _, other := range variants {
		if queryHash(other, 10) == base {
			t.Fatalf("query binding collision %#v", other)
		}
	}
	if queryHash(q, 11) == base {
		t.Fatal("page size was not bound")
	}
}

func TestCursorArgumentBindingRedactsOnlyConsumedOptions(t *testing.T) {
	vm := goja.New()
	token := "signed-token"
	raw := map[string]any{
		"page":   map[string]any{"numItems": float64(2), "cursor": token},
		"nested": map[string]any{"cursor": "application-value", "keep": true},
	}
	canonical, err := deploy.CanonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	options := vm.NewObject()
	db := &database{vm: vm, args: canonical, rawArgs: raw, argPaths: map[*goja.Object][]string{options: {"page"}}}
	bindings := db.cursorArgumentBindings(options, token)
	expected, err := deploy.CanonicalJSON(map[string]any{
		"page":   map[string]any{"numItems": float64(2), "cursor": nil},
		"nested": map[string]any{"cursor": "application-value", "keep": true},
	})
	if err != nil || !containsString(bindings, expected) {
		t.Fatalf("cursor binding erased unrelated application state: %#v", bindings)
	}
	if _, ok := uniqueArgumentCursorPath(map[string]any{"a": map[string]any{"cursor": token}, "b": map[string]any{"cursor": token}}, token, nil, 0); ok {
		t.Fatal("ambiguous cursor source was accepted")
	}
}

func TestCursorRejectsTamperAndBindingMismatch(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	db := &database{vm: goja.New(), ctx: context.Background(), app: app}
	cursor := db.encodeCursor("one", &cursorState{Tuple: []any{"x"}, ID: "abcdefghijklmnop"[:15]})
	mustPanic(t, func() { db.decodeCursor(cursor, "two") })
	mustPanic(t, func() { db.decodeCursor(cursor+"x", "one") })
}
func mustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected error")
		}
	}()
	fn()
}

func TestMutationTimeoutRollsBack(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("timeouts")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	manifest := deploy.DeploymentManifest{DeploymentID: "timeout", Schema: map[string]any{"tables": []any{map[string]any{"tableName": "timeouts", "fields": map[string]any{"name": map[string]any{"type": "string"}}}}}}
	desc := []deploy.FunctionDescriptor{{Name: "hang", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "hang"}}
	bundle := `__pbvex.registerFunction({name:"hang",type:"mutation",visibility:"public",modulePath:"x",exportName:"hang"},function(ctx){ctx.db.insert("timeouts",{name:"no"});for(;;){}})`
	m := NewManager(Config{PoolSize: 1, Timeout: 5 * time.Millisecond})
	if err := m.Compile("timeout", bundle, desc, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "timeout", "hang", nil, nil, "", app, manifest); err == nil {
		t.Fatal("expected timeout")
	}
	rows, err := app.FindRecordsByFilter("timeouts", "", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("timeout committed %#v", rows)
	}
}

func TestConcurrentDatabaseInvocationsAreIsolated(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("concurrent")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	manifest := deploy.DeploymentManifest{DeploymentID: "concurrent", Schema: map[string]any{"tables": []any{map[string]any{"tableName": "concurrent", "fields": map[string]any{"name": map[string]any{"type": "string"}}}}}}
	desc := []deploy.FunctionDescriptor{{Name: "write", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "write"}}
	bundle := `__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"x",exportName:"write"},function(ctx,args){return ctx.db.insert("concurrent",{name:args.name})})`
	m := NewManager(Config{PoolSize: 4, Timeout: time.Second})
	if err := m.Compile("concurrent", bundle, desc, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := m.InvokeWithDatabase(context.Background(), "concurrent", "write", map[string]any{"name": fmt.Sprintf("n%d", i)}, nil, "", app, manifest)
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, err := app.FindRecordsByFilter("concurrent", "", "", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 12 {
		t.Fatalf("got %d writes", len(rows))
	}
}

func TestDatabaseCollectUsesBoundedSQLAndStrictPaginationOptions(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("bounded")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{"value": map[string]any{"type": "string"}}
	for i := 0; i < maxQueryItems+2; i++ {
		r := core.NewRecord(col)
		data := map[string]any{"value": fmt.Sprintf("%04d", i)}
		projection, err := schema.OrderData(fields, data)
		if err != nil {
			t.Fatal(err)
		}
		r.Set("created", types.NowDateTime())
		r.Set(documentDataField, data)
		r.Set(schema.DocumentOrderField, projection)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	manifest := deploy.DeploymentManifest{Schema: map[string]any{"tables": []any{map[string]any{"tableName": "bounded", "fields": fields}}}}
	db := &database{vm: goja.New(), ctx: context.Background(), app: app, manifest: manifest}
	q := &queryBuilder{db: db, table: "bounded"}
	if rows, _, extra, err := db.boundedDocuments(q, maxQueryItems+1, maxQueryItems+1, nil); err != nil || extra || len(rows) != maxQueryItems+1 {
		t.Fatalf("streaming executor ignored SQL limit: %d rows, extra=%v, err=%v", len(rows), extra, err)
	}
	mustPanic(t, func() { db.docs(q) })
	mustPanic(t, func() { db.docsLimit(q, 0) })
	for _, options := range []map[string]any{
		{"numItems": "1"},
		{"numItems": float64(0)},
		{"numItems": float64(maxQueryItems + 1)},
		{"numItems": float64(1), "cursor": ""},
		{"numItems": float64(1), "extra": true},
	} {
		options := options
		mustPanic(t, func() {
			db.paginate(q, goja.FunctionCall{Arguments: []goja.Value{db.vm.ToValue(options)}})
		})
	}
	undefinedCursor := db.vm.NewObject()
	undefinedCursor.Set("numItems", 1)
	undefinedCursor.Set("cursor", goja.Undefined())
	mustPanic(t, func() {
		db.paginate(q, goja.FunctionCall{Arguments: []goja.Value{undefinedCursor}})
	})
	hiddenOption, err := db.vm.RunString(`(()=>{const value={numItems:1};Object.defineProperty(value,"extra",{value:true});return value})()`)
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, func() {
		db.paginate(q, goja.FunctionCall{Arguments: []goja.Value{hiddenOption}})
	})
	large := core.NewBaseCollection("large")
	large.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	large.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	large.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	if err := app.Save(large); err != nil {
		t.Fatal(err)
	}
	largeData := map[string]any{"value": strings.Repeat("x", 128)}
	largeProjection, err := schema.OrderData(fields, largeData)
	if err != nil {
		t.Fatal(err)
	}
	largeRecord := core.NewRecord(large)
	largeRecord.Set("created", types.NowDateTime())
	largeRecord.Set(documentDataField, largeData)
	largeRecord.Set(schema.DocumentOrderField, largeProjection)
	if err := app.Save(largeRecord); err != nil {
		t.Fatal(err)
	}
	db.manifest.Schema = map[string]any{"tables": []any{
		map[string]any{"tableName": "bounded", "fields": fields},
		map[string]any{"tableName": "large", "fields": fields},
	}}
	db.manifest.Config = &deploy.DeploymentConfig{MaxReturnValueBytes: 32}
	largeQuery := &queryBuilder{db: db, table: "large"}
	// collect, take/first/unique (docsLimit), and paginate all charge bytes as
	// each SQL row is decoded, before Goja materializes a document.
	mustPanic(t, func() { db.docs(largeQuery) })
	mustPanic(t, func() { db.docsLimit(largeQuery, 1) })
	mustPanic(t, func() {
		db.paginate(largeQuery, goja.FunctionCall{Arguments: []goja.Value{db.vm.ToValue(map[string]any{"numItems": float64(1)})}})
	})
}

func TestOpaqueIDsAreSignedTableBoundAndRemainValidAfterDelete(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{"name": map[string]any{"type": "string"}}
	collections := map[string]*core.Collection{}
	for _, name := range []string{"alpha", "beta"} {
		col := core.NewBaseCollection(name)
		col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
		col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
		col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
		if err := app.Save(col); err != nil {
			t.Fatal(err)
		}
		collections[name] = col
	}
	raw := "abcdefghijklmnop"[:15]
	for name, col := range collections {
		data := map[string]any{"name": name}
		projection, _ := schema.OrderData(fields, data)
		r := core.NewRecord(col)
		r.Set("id", raw)
		r.Set("created", types.NowDateTime())
		r.Set(documentDataField, data)
		r.Set(schema.DocumentOrderField, projection)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	manifest := deploy.DeploymentManifest{Schema: map[string]any{"tables": []any{
		map[string]any{"tableName": "alpha", "fields": fields},
		map[string]any{"tableName": "beta", "fields": fields},
	}}}
	db := &database{vm: goja.New(), ctx: context.Background(), app: app, manifest: manifest}
	alpha, err := db.encodeID("alpha", raw)
	if err != nil {
		t.Fatal(err)
	}
	beta, err := db.encodeID("beta", raw)
	if err != nil {
		t.Fatal(err)
	}
	if alpha == beta || !db.validIDForTable(alpha, "alpha") || db.validIDForTable(alpha, "beta") {
		t.Fatalf("ids are not table bound: %q %q", alpha, beta)
	}
	tampered := alpha[:len(alpha)-1] + "A"
	if tampered == alpha {
		tampered = alpha[:len(alpha)-1] + "B"
	}
	if db.validIDForTable(tampered, "alpha") {
		t.Fatal("tampered id accepted")
	}
	_, record, err := db.record(alpha)
	if err != nil || record == nil || record.Collection().Name != "alpha" {
		t.Fatalf("alpha record lookup %#v, %v", record, err)
	}
	if err := app.Delete(record); err != nil {
		t.Fatal(err)
	}
	if !db.validIDForTable(alpha, "alpha") {
		t.Fatal("deleted id lost syntax validity")
	}
	value := db.normalizeID(goja.FunctionCall{Arguments: []goja.Value{db.vm.ToValue("alpha"), db.vm.ToValue(alpha)}})
	if value.String() != alpha {
		t.Fatalf("deleted id normalization %#v", value)
	}
}

func TestOpaqueIDRootComponentAndLegacyNamespaceMatrix(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	root := &database{vm: goja.New(), ctx: context.Background(), app: app, namespace: deploy.RootNamespace}
	componentNamespace := "cmp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	component := &database{vm: goja.New(), ctx: context.Background(), app: app, namespace: componentNamespace}
	rootID, err := root.encodeID("items", "abcdefghijklmno")
	if err != nil {
		t.Fatal(err)
	}
	componentID, err := component.encodeID("items", "ponmlkjihgfedcb")
	if err != nil {
		t.Fatal(err)
	}
	if payload, err := root.verifyID(rootID); err != nil || payload.V != 2 || payload.N != deploy.RootNamespace {
		t.Fatalf("root pbv2 rejected: %#v %v", payload, err)
	}
	if payload, err := component.verifyID(componentID); err != nil || payload.V != 2 || payload.N != componentNamespace {
		t.Fatalf("component pbv2 rejected: %#v %v", payload, err)
	}
	if _, err := root.verifyID(componentID); err == nil {
		t.Fatal("root accepted component id")
	}
	if _, err := component.verifyID(rootID); err == nil {
		t.Fatal("component accepted root id")
	}
	ring, err := root.loadKeyRing()
	if err != nil {
		t.Fatal(err)
	}
	legacyPayload, _ := json.Marshal(opaqueIDPayload{V: 1, K: ring.currentID, N: ring.legacyNamespace, T: "items", R: "abcdefghijklmno"})
	legacy := "pbv1." + strconv.Itoa(ring.currentID) + "." + base64.RawURLEncoding.EncodeToString(legacyPayload) + "." + base64.RawURLEncoding.EncodeToString(idMAC(ring.current, legacyPayload))
	if _, err := root.verifyID(legacy); err != nil {
		t.Fatalf("root rejected authenticated legacy id: %v", err)
	}
	if _, err := component.verifyID(legacy); err == nil {
		t.Fatal("component accepted legacy root id")
	}
}

func TestDatabaseNestedFieldsAndCompoundEquality(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("profiles")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, Hidden: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	col.AddIndex("idx_pbvex_profiles_by_name", false, "json_extract(_pbvex_order, "+schema.SQLiteJSONPathLiteral("profile.name")+"), created, id", "")
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{
		"profile": map[string]any{"type": "object", "shape": map[string]any{
			"name":  map[string]any{"type": "string"},
			"stats": map[string]any{"type": "object", "shape": map[string]any{"score": map[string]any{"type": "number"}}},
		}},
		"anything": map[string]any{"type": "any"},
		"tags":     map[string]any{"type": "array", "item": map[string]any{"type": "string"}},
		"labels":   map[string]any{"type": "record", "key": map[string]any{"type": "string"}, "value": map[string]any{"type": "number"}},
	}
	manifest := deploy.DeploymentManifest{Schema: map[string]any{"tables": []any{map[string]any{"tableName": "profiles", "fields": fields, "indexes": []any{map[string]any{"name": "by_name", "fields": []any{"profile.name"}}}}}}}
	desc := []deploy.FunctionDescriptor{
		{Name: "write", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "write"},
		{Name: "nested", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "nested"},
		{Name: "anyEq", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "anyEq"},
		{Name: "objectEq", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "objectEq"},
		{Name: "arrayEq", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "arrayEq"},
		{Name: "recordNeq", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "recordNeq"},
		{Name: "literalEq", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "literalEq"},
		{Name: "nestedPage", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "nestedPage"},
		{Name: "badPath", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "badPath"},
	}
	bundle := `
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"x",exportName:"write"},(ctx,args)=>ctx.db.insert("profiles",args.doc));
__pbvex.registerFunction({name:"nested",type:"query",visibility:"public",modulePath:"x",exportName:"nested"},(ctx,args)=>ctx.db.query("profiles").filter(q=>q.eq(q.field("profile.name"),q.literal(args.value))).collect());
__pbvex.registerFunction({name:"anyEq",type:"query",visibility:"public",modulePath:"x",exportName:"anyEq"},(ctx,args)=>ctx.db.query("profiles").filter(q=>q.eq(q.field("anything"),q.literal(args.value))).collect());
__pbvex.registerFunction({name:"objectEq",type:"query",visibility:"public",modulePath:"x",exportName:"objectEq"},(ctx,args)=>ctx.db.query("profiles").filter(q=>q.eq(q.field("profile"),q.literal(args.value))).collect());
__pbvex.registerFunction({name:"arrayEq",type:"query",visibility:"public",modulePath:"x",exportName:"arrayEq"},(ctx,args)=>ctx.db.query("profiles").filter(q=>q.eq(q.field("tags"),q.literal(args.value))).collect());
__pbvex.registerFunction({name:"recordNeq",type:"query",visibility:"public",modulePath:"x",exportName:"recordNeq"},(ctx,args)=>ctx.db.query("profiles").filter(q=>q.neq(q.field("labels"),q.literal(args.value))).collect());
__pbvex.registerFunction({name:"literalEq",type:"query",visibility:"public",modulePath:"x",exportName:"literalEq"},ctx=>ctx.db.query("profiles").filter(q=>q.eq(q.literal({a:1,b:[true]}),q.literal({b:[true],a:1}))).collect());
__pbvex.registerFunction({name:"nestedPage",type:"query",visibility:"public",modulePath:"x",exportName:"nestedPage"},(ctx,args)=>ctx.db.query("profiles").withIndex("by_name").paginate({numItems:1,cursor:args.cursor}));
__pbvex.registerFunction({name:"badPath",type:"query",visibility:"public",modulePath:"x",exportName:"badPath"},ctx=>ctx.db.query("profiles").filter(q=>q.eq(q.field("profile.missing"),q.literal("x"))).collect());`
	m := NewManager(DefaultConfig())
	if err := m.Compile("nested", bundle, desc, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	docs := []map[string]any{
		{"profile": map[string]any{"name": "Ada", "stats": map[string]any{"score": float64(1)}}, "anything": map[string]any{"b": []any{true}, "a": float64(1)}, "tags": []any{"go", "db"}, "labels": map[string]any{"x": float64(1)}},
		{"profile": map[string]any{"name": "Bea", "stats": map[string]any{"score": float64(2)}}, "anything": []any{"other"}, "tags": []any{"js"}, "labels": map[string]any{"x": float64(2)}},
	}
	for _, doc := range docs {
		if _, err := m.InvokeWithDatabase(context.Background(), "nested", "write", map[string]any{"doc": doc}, nil, "", app, manifest); err != nil {
			t.Fatal(err)
		}
	}
	assertCount := func(name string, args any, want int) {
		t.Helper()
		got, err := m.InvokeWithDatabase(context.Background(), "nested", name, args, nil, "", app, manifest)
		if err != nil || len(got.([]any)) != want {
			t.Fatalf("%s = %#v, %v", name, got, err)
		}
	}
	assertCount("nested", map[string]any{"value": "Ada"}, 1)
	assertCount("anyEq", map[string]any{"value": map[string]any{"a": float64(1), "b": []any{true}}}, 1)
	assertCount("objectEq", map[string]any{"value": docs[0]["profile"]}, 1)
	assertCount("arrayEq", map[string]any{"value": []any{"go", "db"}}, 1)
	assertCount("recordNeq", map[string]any{"value": map[string]any{"x": float64(1)}}, 1)
	assertCount("literalEq", nil, 2)
	first, err := m.InvokeWithDatabase(context.Background(), "nested", "nestedPage", map[string]any{"cursor": nil}, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	page := first.(map[string]any)
	if got := page["page"].([]any)[0].(map[string]any)["profile"].(map[string]any)["name"]; got != "Ada" {
		t.Fatalf("nested index ordering = %#v", page)
	}
	second, err := m.InvokeWithDatabase(context.Background(), "nested", "nestedPage", map[string]any{"cursor": page["continueCursor"]}, nil, "", app, manifest)
	if err != nil || second.(map[string]any)["page"].([]any)[0].(map[string]any)["profile"].(map[string]any)["name"] != "Bea" {
		t.Fatalf("nested index cursor continuation %#v, %v", second, err)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "nested", "badPath", nil, nil, "", app, manifest); err == nil {
		t.Fatal("undeclared nested q.field path was accepted")
	}
}

func TestDatabaseFieldEqualityUsesCanonicalWireValueAcrossValidators(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	backing := func(name string) *core.Collection {
		col := core.NewBaseCollection(name)
		col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
		col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, Hidden: true, MaxSize: 1 << 20})
		col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
		return col
	}
	if err := app.Save(backing("targets")); err != nil {
		t.Fatal(err)
	}
	if err := app.Save(backing("values")); err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{
		"asID":     map[string]any{"type": "id", "tableName": "targets"},
		"asString": map[string]any{"type": "string"},
		"asAny":    map[string]any{"type": "any"},
		"asUnion": map[string]any{"type": "union", "validators": []any{
			map[string]any{"type": "string"}, map[string]any{"type": "id", "tableName": "targets"},
		}},
	}
	manifest := deploy.DeploymentManifest{DeploymentID: "wire_eq", Schema: map[string]any{"tables": []any{
		map[string]any{"tableName": "targets", "fields": map[string]any{"kind": map[string]any{"type": "string"}}},
		map[string]any{"tableName": "values", "fields": fields},
	}}}
	descriptors := []deploy.FunctionDescriptor{
		{Name: "target", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "target"},
		{Name: "write", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "write"},
		{Name: "equal", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "equal"},
		{Name: "notEqual", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "notEqual"},
	}
	bundle := `
__pbvex.registerFunction({name:"target",type:"mutation",visibility:"public",modulePath:"x",exportName:"target"},ctx=>ctx.db.insert("targets",{kind:"anchor"}));
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"x",exportName:"write"},(ctx,args)=>{ctx.db.insert("values",{asID:args.id,asString:args.id,asAny:args.id,asUnion:args.id});});
__pbvex.registerFunction({name:"equal",type:"query",visibility:"public",modulePath:"x",exportName:"equal"},ctx=>ctx.db.query("values").filter(q=>q.and(q.eq(q.field("asID"),q.field("asString")),q.and(q.eq(q.field("asString"),q.field("asAny")),q.eq(q.field("asAny"),q.field("asUnion"))))).collect());
__pbvex.registerFunction({name:"notEqual",type:"query",visibility:"public",modulePath:"x",exportName:"notEqual"},ctx=>ctx.db.query("values").filter(q=>q.neq(q.field("asID"),q.field("asUnion"))).collect());`
	m := NewManager(DefaultConfig())
	if err := m.Compile("wire_eq", bundle, descriptors, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	target, err := m.InvokeWithDatabase(context.Background(), "wire_eq", "target", nil, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "wire_eq", "write", map[string]any{"id": target}, nil, "", app, manifest); err != nil {
		t.Fatal(err)
	}
	got, err := m.InvokeWithDatabase(context.Background(), "wire_eq", "equal", nil, nil, "", app, manifest)
	if err != nil || len(got.([]any)) != 1 {
		t.Fatalf("canonical field equality %#v, %v", got, err)
	}
	got, err = m.InvokeWithDatabase(context.Background(), "wire_eq", "notEqual", nil, nil, "", app, manifest)
	if err != nil || len(got.([]any)) != 0 {
		t.Fatalf("canonical field inequality %#v, %v", got, err)
	}
}

func TestIndexedStringIDUnionClassifiesPBV2ByNamespace(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}

	union := func(target string) map[string]any {
		return map[string]any{"type": "union", "validators": []any{
			map[string]any{"type": "string"}, map[string]any{"type": "id", "tableName": target},
		}}
	}
	rootSchema := map[string]any{"tables": []any{
		map[string]any{"tableName": "root_targets", "fields": map[string]any{"label": map[string]any{"type": "string"}}},
		map[string]any{"tableName": "root_values", "fields": map[string]any{"v": union("root_targets")}, "indexes": []any{map[string]any{"name": "by_v", "fields": []any{"v"}}}},
	}}
	componentSchema := map[string]any{"tables": []any{
		map[string]any{"tableName": "targets", "fields": map[string]any{"label": map[string]any{"type": "string"}}},
		map[string]any{"tableName": "values", "fields": map[string]any{"v": union("targets")}, "indexes": []any{map[string]any{"name": "by_v", "fields": []any{"v"}}}},
	}}
	graph := &deploy.ComponentGraph{
		Definitions: []deploy.ComponentDefinition{{ComponentID: "widget", ModulePaths: []string{"store.ts"}, Schema: componentSchema}},
		Mounts:      []deploy.ComponentMount{{Name: "widget", ComponentID: "widget"}},
	}
	namespaces, err := deploy.ComponentNamespaces(graph)
	if err != nil {
		t.Fatal(err)
	}
	component := namespaces["widget"]

	createBacking := func(name string, indexed bool) {
		t.Helper()
		col := core.NewBaseCollection(name)
		col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
		col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
		col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
		if indexed {
			col.AddIndex("idx_"+name+"_by_v", false, "json_extract(_pbvex_order, "+schema.SQLiteJSONPathLiteral("v")+"), created, id", "")
		}
		if err := app.Save(col); err != nil {
			t.Fatal(err)
		}
	}
	createBacking("root_targets", false)
	createBacking("root_values", true)
	createBacking(component.PhysicalByTable["targets"], false)
	createBacking(component.PhysicalByTable["values"], true)

	descriptors := []deploy.FunctionDescriptor{
		{Name: "rootSeed", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "root.ts", ExportName: "rootSeed"},
		{Name: "rootCheck", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "root.ts", ExportName: "rootCheck"},
		{Name: "componentSeed", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/components/widget/store.ts", ExportName: "componentSeed"},
		{Name: "componentCheck", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/components/widget/store.ts", ExportName: "componentCheck"},
	}
	bundle := `
function seed(ctx, targets, values) {
  const id = ctx.db.insert(targets, {label:"anchor"});
  ctx.db.insert(values, {v:"before"}); ctx.db.insert(values, {v:"zzzz"}); ctx.db.insert(values, {v:id});
  return id;
}
function check(ctx, args, values) {
  return {
    equal: ctx.db.query(values).withIndex("by_v", q=>q.eq("v", args.id)).collect(),
    range: ctx.db.query(values).withIndex("by_v", q=>q.gte("v", args.id)).collect(),
    ordered: ctx.db.query(values).withIndex("by_v").order("asc").collect()
  };
}
__pbvex.registerFunction({name:"rootSeed",type:"mutation",visibility:"public",modulePath:"root.ts",exportName:"rootSeed"},ctx=>seed(ctx,"root_targets","root_values"));
__pbvex.registerFunction({name:"rootCheck",type:"query",visibility:"public",modulePath:"root.ts",exportName:"rootCheck"},(ctx,args)=>check(ctx,args,"root_values"));
__pbvex.registerFunction({name:"componentSeed",type:"mutation",visibility:"internal",modulePath:"pbvex/components/widget/store.ts",exportName:"componentSeed"},ctx=>seed(ctx,"targets","values"));
__pbvex.registerFunction({name:"componentCheck",type:"query",visibility:"internal",modulePath:"pbvex/components/widget/store.ts",exportName:"componentCheck"},(ctx,args)=>check(ctx,args,"values"));`
	manager := NewManager(DefaultConfig())
	if err := manager.Compile("pbv2_union_index", bundle, descriptors, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	manifest := deploy.DeploymentManifest{DeploymentID: "pbv2_union_index", Functions: descriptors, Schema: rootSchema, Components: graph}

	for _, tc := range []struct{ name, seed, check, namespace string }{
		{name: "root", seed: "rootSeed", check: "rootCheck", namespace: deploy.RootNamespace},
		{name: "component", seed: "componentSeed", check: "componentCheck", namespace: component.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idValue, err := manager.InvokeWithDatabase(context.Background(), "pbv2_union_index", tc.seed, nil, nil, "", app, manifest)
			if err != nil {
				t.Fatal(err)
			}
			id, ok := idValue.(string)
			if !ok {
				t.Fatalf("seed returned %#v", idValue)
			}
			payload, _, _, ok := parseOpaqueID(id)
			if !ok || payload.V != 2 || payload.N != tc.namespace {
				t.Fatalf("seed returned wrong namespace ID %#v", id)
			}
			result, err := manager.InvokeWithDatabase(context.Background(), "pbv2_union_index", tc.check, map[string]any{"id": id}, nil, "", app, manifest)
			if err != nil {
				t.Fatal(err)
			}
			got := result.(map[string]any)
			for _, key := range []string{"equal", "range"} {
				rows := got[key].([]any)
				if len(rows) != 1 || rows[0].(map[string]any)["v"] != id {
					t.Fatalf("%s query returned %#v", key, rows)
				}
			}
			rows := got["ordered"].([]any)
			if len(rows) != 3 || rows[0].(map[string]any)["v"] != "before" || rows[1].(map[string]any)["v"] != "zzzz" || rows[2].(map[string]any)["v"] != id {
				t.Fatalf("ordered query returned %#v", rows)
			}
		})
	}
}

// TestDatabaseEmptyResultsAreEmptyArrays guards against a regression where
// goja's variadic NewArray was being called with the length as its sole
// element: an empty collect/take/page encoded as [0] instead of [], which a
// host could not distinguish from a single integer document.
func TestDatabaseEmptyResultsAreEmptyArrays(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("empties")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, Hidden: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	manifest := deploy.DeploymentManifest{DeploymentID: "empty", Schema: map[string]any{"tables": []any{
		map[string]any{"tableName": "empties", "fields": map[string]any{"body": map[string]any{"type": "string"}}},
	}}}
	descriptors := []deploy.FunctionDescriptor{
		{Name: "collect", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "collect"},
		{Name: "take", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "take"},
		{Name: "page", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "page"},
	}
	bundle := `
__pbvex.registerFunction({name:"collect",type:"query",visibility:"public",modulePath:"x",exportName:"collect"},ctx=>ctx.db.query("empties").filter(q=>q.eq(q.field("body"),q.literal("missing"))).collect());
__pbvex.registerFunction({name:"take",type:"query",visibility:"public",modulePath:"x",exportName:"take"},ctx=>ctx.db.query("empties").filter(q=>q.eq(q.field("body"),q.literal("missing"))).take(5));
__pbvex.registerFunction({name:"page",type:"query",visibility:"public",modulePath:"x",exportName:"page"},ctx=>ctx.db.query("empties").filter(q=>q.eq(q.field("body"),q.literal("missing"))).paginate({numItems:5}));`
	m := NewManager(DefaultConfig())
	if err := m.Compile("empty", bundle, descriptors, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	collect, err := m.InvokeWithDatabase(context.Background(), "empty", "collect", nil, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := collect.([]any); !ok || len(got) != 0 {
		t.Fatalf("empty collect = %#v (type %T)", collect, collect)
	}
	take, err := m.InvokeWithDatabase(context.Background(), "empty", "take", nil, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := take.([]any); !ok || len(got) != 0 {
		t.Fatalf("empty take = %#v (type %T)", take, take)
	}
	page, err := m.InvokeWithDatabase(context.Background(), "empty", "page", nil, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	pageMap, ok := page.(map[string]any)
	if !ok {
		t.Fatalf("page = %#v (type %T)", page, page)
	}
	pageRows, ok := pageMap["page"].([]any)
	if !ok || len(pageRows) != 0 {
		t.Fatalf("empty page.page = %#v", pageMap["page"])
	}
}

func TestIndexedKeysetUsesCreationTimeBeforeRawID(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("ordered")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, Hidden: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	col.AddIndex("idx_pbvex_ordered_by_v", false, "json_extract(_pbvex_order, '$.\"v\"'), created, id", "")
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{"v": map[string]any{"type": "string"}}
	for _, row := range []struct {
		id      string
		created string
	}{
		{"zzzzzzzzzzzzzzz", "2024-01-01 00:00:00.000Z"},
		{"aaaaaaaaaaaaaaa", "2024-01-01 00:00:01.000Z"},
	} {
		r := core.NewRecord(col)
		dt, err := types.ParseDateTime(row.created)
		if err != nil {
			t.Fatal(err)
		}
		projection, err := schema.OrderData(fields, map[string]any{"v": "same"})
		if err != nil {
			t.Fatal(err)
		}
		r.Set("id", row.id)
		r.Set("created", dt)
		r.Set(documentDataField, map[string]any{"v": "same"})
		r.Set(schema.DocumentOrderField, projection)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	manifest := deploy.DeploymentManifest{Schema: map[string]any{"tables": []any{map[string]any{
		"tableName": "ordered", "fields": fields, "indexes": []any{map[string]any{"name": "by_v", "fields": []any{"v"}}},
	}}}}
	db := &database{vm: goja.New(), ctx: context.Background(), app: app, manifest: manifest}
	q := &queryBuilder{db: db, table: "ordered", index: "by_v", indexFields: []string{"v"}}
	if columns := db.orderColumns(q); len(columns) != 3 || !strings.Contains(columns[1], ".created") || !strings.Contains(columns[2], ".id") {
		t.Fatalf("indexed ordering tuple %#v", columns)
	}
	page, last, extra, err := db.boundedDocuments(q, 2, 1, nil)
	if err != nil || !extra || len(page) != 1 {
		t.Fatalf("first page %#v last=%#v extra=%v err=%v", page, last, extra, err)
	}
	if got := page[0]["_id"].(string); !db.validIDForTable(got, "ordered") {
		t.Fatalf("invalid first id %q", got)
	}
	firstPayload, err := db.verifyID(page[0]["_id"].(string))
	if err != nil || firstPayload.R != "zzzzzzzzzzzzzzz" {
		t.Fatalf("raw id won before creation time: %#v, %v", firstPayload, err)
	}
	state := &cursorState{Tuple: db.queryRowTuple(q, last), ID: last.id}
	if len(state.Tuple) != 2 || state.Tuple[1] != last.created {
		t.Fatalf("cursor omitted creation tuple %#v", state)
	}
	next, _, _, err := db.boundedDocuments(q, 2, 1, state)
	if err != nil || len(next) != 1 {
		t.Fatalf("second page %#v, %v", next, err)
	}
	nextPayload, err := db.verifyID(next[0]["_id"].(string))
	if err != nil || nextPayload.R != "aaaaaaaaaaaaaaa" {
		t.Fatalf("creation-time keyset continuation %#v, %v", nextPayload, err)
	}
}

func TestIndexedKeysetOrdersProtocolScalars(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	col := core.NewBaseCollection("mixed")
	col.Fields.Add(&core.DateField{Name: "created", System: true, Hidden: true})
	col.Fields.Add(&core.JSONField{Name: documentDataField, Required: true, MaxSize: 1 << 20})
	col.Fields.Add(&core.JSONField{Name: schema.DocumentOrderField, Required: true, Hidden: true, MaxSize: 4 << 20})
	col.AddIndex("idx_pbvex_mixed_by_v", false, "json_extract(_pbvex_order, '$.v'), created, id", "")
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}
	union := map[string]any{"type": "union", "validators": []any{
		map[string]any{"type": "null"}, map[string]any{"type": "boolean"}, map[string]any{"type": "number"},
		map[string]any{"type": "string"}, map[string]any{"type": "int64"}, map[string]any{"type": "bytes"},
	}}
	manifest := deploy.DeploymentManifest{DeploymentID: "mixed", Schema: map[string]any{"tables": []any{map[string]any{
		"tableName": "mixed", "fields": map[string]any{"v": union}, "indexes": []any{map[string]any{"name": "by_v", "fields": []any{"v"}}},
	}}}}
	desc := []deploy.FunctionDescriptor{
		{Name: "write", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "write"},
		{Name: "late", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "late"},
		{Name: "page", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "page"},
	}
	bundle := `
__pbvex.registerFunction({name:"write",type:"mutation",visibility:"public",modulePath:"x",exportName:"write"},function(ctx){
 return [ctx.db.insert("mixed",{v:null}),ctx.db.insert("mixed",{v:false}),ctx.db.insert("mixed",{v:true}),ctx.db.insert("mixed",{v:-1.5}),ctx.db.insert("mixed",{v:"same"}),ctx.db.insert("mixed",{v:"same"}),ctx.db.insert("mixed",{v:-9n}),ctx.db.insert("mixed",{v:new Uint8Array([0,255]).buffer})];
});
__pbvex.registerFunction({name:"late",type:"mutation",visibility:"public",modulePath:"x",exportName:"late"},function(ctx){return ctx.db.insert("mixed",{v:"later"})});
__pbvex.registerFunction({name:"page",type:"query",visibility:"public",modulePath:"x",exportName:"page"},function(ctx,args){let q=ctx.db.query("mixed").withIndex("by_v");if(args.desc)q=q.order("desc");return q.paginate({numItems:args.n,cursor:args.cursor===undefined?null:args.cursor});});`
	m := NewManager(DefaultConfig())
	if err := m.Compile("mixed", bundle, desc, deploy.DefaultDeploymentConfig); err != nil {
		t.Fatal(err)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "mixed", "write", nil, nil, "", app, manifest); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"n": float64(2), "desc": false, "cursor": nil}
	first, err := m.InvokeWithDatabase(context.Background(), "mixed", "page", args, nil, "", app, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.InvokeWithDatabase(context.Background(), "mixed", "late", nil, nil, "", app, manifest); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	page := first.(map[string]any)
	for {
		for _, raw := range page["page"].([]any) {
			id := raw.(map[string]any)["_id"].(string)
			if seen[id] {
				t.Fatalf("keyset repeated %q", id)
			}
			seen[id] = true
		}
		cursor := page["continueCursor"].(string)
		if cursor == "" {
			break
		}
		args = map[string]any{"n": float64(2), "desc": false, "cursor": cursor}
		next, err := m.InvokeWithDatabase(context.Background(), "mixed", "page", args, nil, "", app, manifest)
		if err != nil {
			t.Fatal(err)
		}
		page = next.(map[string]any)
	}
	if len(seen) != 9 {
		rows, _ := app.FindRecordsByFilter("mixed", "", "", 20, 0)
		t.Fatalf("keyset skipped scalar values (%d stored): %#v", len(rows), seen)
	}
	// A cursor is bound to ordering, page size, canonical arguments and the
	// function/deployment plan. It cannot be replayed across any of them.
	firstPage := first.(map[string]any)
	cursor := firstPage["continueCursor"].(string)
	for _, badArgs := range []map[string]any{
		{"n": float64(3), "desc": false, "cursor": cursor},
		{"n": float64(2), "desc": true, "cursor": cursor},
		{"n": float64(2), "desc": false, "cursor": cursor[:len(cursor)-1] + "A"},
	} {
		if _, err := m.InvokeWithDatabase(context.Background(), "mixed", "page", badArgs, nil, "", app, manifest); err == nil {
			t.Fatalf("cursor binding accepted %#v", badArgs)
		}
	}
}
