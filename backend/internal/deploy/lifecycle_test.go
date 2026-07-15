package deploy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

func storedDocument(t *testing.T, row *core.Record) map[string]any {
	t.Helper()
	data := map[string]any{}
	b, err := json.Marshal(row.Get("_pbvex_data"))
	if err != nil || json.Unmarshal(b, &data) != nil {
		t.Fatalf("stored document %#v: %v", row.Get("_pbvex_data"), err)
	}
	return data
}

func signedTestOpaqueID(t *testing.T, app core.App, version int, namespace, table, raw string) string {
	t.Helper()
	state := &core.Record{}
	if err := app.RecordQuery(schema.CollectionSchemaState).AndWhere(dbx.HashExp{schema.CollectionSchemaState + "." + schema.FieldKey: schema.StateKeyActive}).Limit(1).One(state); err != nil {
		t.Fatal(err)
	}
	root, err := base64.RawURLEncoding.DecodeString(state.GetString(schema.FieldIDSecret))
	if err != nil {
		t.Fatal(err)
	}
	keyID := state.GetInt(schema.FieldIDKeyID)
	payload, err := json.Marshal(struct {
		V int    `json:"v"`
		K int    `json:"k"`
		N string `json:"n"`
		T string `json:"t"`
		R string `json:"r"`
	}{V: version, K: keyID, N: namespace, T: table, R: raw})
	if err != nil {
		t.Fatal(err)
	}
	prefix := "pbv2"
	if version == 1 {
		prefix = "pbv1"
	}
	return prefix + "." + fmt.Sprint(keyID) + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(schema.OpaqueIDVersionMAC(root, keyID, payload))
}

func TestComponentMountArgsAuthenticateExactNamespaceWithoutMutatingManifest(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	aNamespace, _ := ComponentNamespaceID("a")
	bNamespace, _ := ComponentNamespaceID("b")
	state := &core.Record{}
	if err := app.RecordQuery(schema.CollectionSchemaState).AndWhere(dbx.HashExp{schema.CollectionSchemaState + "." + schema.FieldKey: schema.StateKeyActive}).Limit(1).One(state); err != nil {
		t.Fatal(err)
	}
	descriptor := map[string]any{"type": "object", "shape": map[string]any{"ref": map[string]any{"type": "id", "tableName": "targets"}}}
	definition := ComponentDefinition{ComponentID: "component", Args: descriptor}
	manifestFor := func(value string, defaulted bool) DeploymentManifest {
		args := any(map[string]any{"ref": value})
		argsPresent := true
		def := definition
		if defaulted {
			def.Args = map[string]any{"type": "defaulted", "validator": descriptor, "defaultValue": args}
			args, argsPresent = nil, false
		}
		return DeploymentManifest{DeploymentID: "immutable", Components: &ComponentGraph{
			Definitions: []ComponentDefinition{def},
			Mounts:      []ComponentMount{{Name: "a", ComponentID: "component", Args: args, ArgsPresent: argsPresent}},
		}}
	}
	own := signedTestOpaqueID(t, app, 2, aNamespace, "targets", "abcdefghijklmno")
	sibling := signedTestOpaqueID(t, app, 2, bNamespace, "targets", "abcdefghijklmno")
	root := signedTestOpaqueID(t, app, 2, RootNamespace, "targets", "abcdefghijklmno")
	legacy := signedTestOpaqueID(t, app, 1, state.Id, "targets", "abcdefghijklmno")
	forgedParts := strings.Split(own, ".")
	forgedParts[3] = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	forged := strings.Join(forgedParts, ".")
	for _, tc := range []struct {
		name      string
		value     string
		defaulted bool
		wantOK    bool
	}{
		{"own", own, false, true},
		{"own default", own, true, true},
		{"sibling", sibling, false, false},
		{"root v2", root, false, false},
		{"root legacy", legacy, false, false},
		{"forged", forged, false, false},
		{"forged default", forged, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := manifestFor(tc.value, tc.defaulted)
			before, _ := json.Marshal(manifest)
			err := authenticateComponentMountArgs(context.Background(), app, manifest)
			if (err == nil) != tc.wantOK {
				t.Fatalf("authentication result err=%v", err)
			}
			after, _ := json.Marshal(manifest)
			if string(before) != string(after) {
				t.Fatal("component arg authentication mutated content-addressed manifest")
			}
		})
	}
}

func TestComponentActivationRejectsUnownedABICompatiblePhysicalCollection(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	namespace, _ := ComponentNamespaceID("mounted")
	physical, _ := ComponentCollectionName(namespace, "items")
	if err := app.Save(backingCollection(physical)); err != nil {
		t.Fatal(err)
	}
	manifest := DeploymentManifest{Components: &ComponentGraph{
		Definitions: []ComponentDefinition{{ComponentID: "component", Schema: map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{}}}}}},
		Mounts:      []ComponentMount{{Name: "mounted", ComponentID: "component"}},
	}}
	if err := materializeSchema(context.Background(), app, manifest); err == nil || !strings.Contains(err.Error(), "ownership conflict") {
		t.Fatalf("unowned ABI-compatible collection was adopted: %v", err)
	}
	if _, err := app.FindCollectionByNameOrId(physical); err != nil {
		t.Fatal("failed activation mutated the colliding collection")
	}
}

func TestComponentCatalogRejectsMismatchedHistoricalOwnership(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	manifest := DeploymentManifest{DeploymentID: "catalog_owner", Components: &ComponentGraph{
		Definitions: []ComponentDefinition{{ComponentID: "component", Schema: map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{}}}}}},
		Mounts:      []ComponentMount{{Name: "mounted", ComponentID: "component"}},
	}}
	if err := materializeSchema(context.Background(), app, manifest); err != nil {
		t.Fatal(err)
	}
	namespace, _ := ComponentNamespaceID("mounted")
	record := &core.Record{}
	if err := app.RecordQuery(schema.CollectionComponents).
		AndWhere(dbx.HashExp{schema.CollectionComponents + "." + schema.FieldName: namespace}).
		Limit(1).One(record); err != nil {
		t.Fatal(err)
	}
	record.Set(schema.FieldMetadata, map[string]any{
		"componentId": "component",
		"collections": map[string]any{"items": "pbvex_cmp_forged"},
	})
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := materializeSchema(context.Background(), app, manifest); err == nil || !strings.Contains(err.Error(), "ownership conflict") {
		t.Fatalf("mismatched historical ownership was adopted: %v", err)
	}
}

func TestComponentMigrationAuthenticatesStoredAndDefaultedIDs(t *testing.T) {
	for _, mode := range []string{"stored", "defaulted"} {
		t.Run(mode, func(t *testing.T) {
			app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer app.Cleanup()
			if err := schema.Bootstrap(app); err != nil {
				t.Fatal(err)
			}
			componentSchema := map[string]any{"tables": []any{
				map[string]any{"tableName": "targets", "fields": map[string]any{"name": map[string]any{"type": "string"}}},
				map[string]any{"tableName": "refs", "fields": map[string]any{"value": map[string]any{"type": "string"}}},
			}}
			manifest := DeploymentManifest{DeploymentID: "component_base", Components: &ComponentGraph{
				Definitions: []ComponentDefinition{{ComponentID: "component", Schema: componentSchema}},
				Mounts:      []ComponentMount{{Name: "mounted", ComponentID: "component"}},
			}}
			if err := materializeSchema(context.Background(), app, manifest); err != nil {
				t.Fatal(err)
			}
			namespaces, _ := ComponentNamespaces(manifest.Components)
			namespace := namespaces["mounted"]
			refs := namespace.PhysicalByTable["refs"]
			collection, err := app.FindCollectionByNameOrId(refs)
			if err != nil {
				t.Fatal(err)
			}
			foreignNamespace, _ := ComponentNamespaceID("sibling")
			foreign := signedTestOpaqueID(t, app, 2, foreignNamespace, "targets", "abcdefghijklmno")
			if mode == "defaulted" {
				foreign = signedTestOpaqueID(t, app, 2, RootNamespace, "targets", "abcdefghijklmno")
			}
			data := map[string]any{"value": "legacy"}
			if mode == "stored" {
				data["value"] = foreign
			}
			projection, err := schema.OrderData(map[string]any{"value": map[string]any{"type": "string"}}, data)
			if err != nil {
				t.Fatal(err)
			}
			row := core.NewRecord(collection)
			row.Set("_pbvex_data", data)
			row.Set(schema.DocumentOrderField, projection)
			if err := app.Save(row); err != nil {
				t.Fatal(err)
			}
			refValidator := any(map[string]any{"type": "id", "tableName": "targets"})
			fields := map[string]any{"value": refValidator}
			if mode == "defaulted" {
				fields = map[string]any{
					"value": map[string]any{"type": "string"},
					"ref":   map[string]any{"type": "defaulted", "validator": refValidator, "defaultValue": foreign},
				}
			}
			upgradedSchema := map[string]any{"tables": []any{
				map[string]any{"tableName": "targets", "fields": map[string]any{"name": map[string]any{"type": "string"}}},
				map[string]any{"tableName": "refs", "fields": fields},
			}}
			manifest.Components.Definitions[0].Schema = upgradedSchema
			if err := app.RunInTransaction(func(tx core.App) error { return materializeSchema(context.Background(), tx, manifest) }); err == nil {
				t.Fatalf("foreign %s component ID was accepted during migration", mode)
			}
			reloaded, err := app.FindRecordById(refs, row.Id)
			if err != nil {
				t.Fatal(err)
			}
			if got := storedDocument(t, reloaded); got["value"] != data["value"] || got["ref"] != nil {
				t.Fatalf("failed component ID migration changed row: %#v", got)
			}
		})
	}
}

type recordingInvoker struct{ dropped []string }

func (*recordingInvoker) Compile(string, string, []FunctionDescriptor, DeploymentConfig) error {
	return nil
}
func (*recordingInvoker) Verify(context.Context, string, string, []FunctionDescriptor) error {
	return nil
}

func TestMaterializeSchemaComparesOwnedIndexDefinitionExactly(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	manifest := DeploymentManifest{Schema: map[string]any{"tables": []any{map[string]any{
		"tableName": "notes",
		"fields":    map[string]any{"body": map[string]any{"type": "string"}},
		"indexes":   []any{map[string]any{"name": "by_body", "fields": []any{"body"}}},
	}}}}
	if err := materializeSchema(context.Background(), app, manifest); err != nil {
		t.Fatal(err)
	}
	collection, err := app.FindCollectionByNameOrId("notes")
	if err != nil {
		t.Fatal(err)
	}
	if got := collection.GetIndex("idx_pbvex_notes_by_body"); got == "" || !strings.Contains(got, schema.DocumentOrderField) || !strings.Contains(got, ", created, id") {
		t.Fatalf("materialized index %#v", got)
	}
	// Repair only the exact prior PBVex index ABI (declared keys then raw id)
	// into the current Convex tuple. A collision that merely shares the name is
	// still rejected below.
	collection.AddIndex("idx_pbvex_notes_by_body", false, "json_extract(_pbvex_order, '$.\"body\"'), id", "")
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	if err := materializeSchema(context.Background(), app, manifest); err != nil {
		t.Fatalf("owned legacy index was not reconciled: %v", err)
	}
	collection, err = app.FindCollectionByNameOrId("notes")
	if err != nil {
		t.Fatal(err)
	}
	if got := collection.GetIndex("idx_pbvex_notes_by_body"); !strings.Contains(got, ", created, id") {
		t.Fatalf("legacy index was not upgraded: %q", got)
	}
	collection.AddIndex("idx_pbvex_notes_stale", false, "created, id", "")
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	if err := materializeSchema(context.Background(), app, manifest); err != nil {
		t.Fatal(err)
	}
	collection, err = app.FindCollectionByNameOrId("notes")
	if err != nil {
		t.Fatal(err)
	}
	if collection.GetIndex("idx_pbvex_notes_stale") != "" {
		t.Fatal("stale owned index survived reconciliation")
	}
	// The same name with a uniqueness change is an incompatible external
	// collision, not a harmless column substring match or an index we replace.
	collection.AddIndex("idx_pbvex_notes_by_body", true, "json_extract(_pbvex_order, '$.body'), id", "")
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	if err := materializeSchema(context.Background(), app, manifest); err == nil {
		t.Fatal("unique index drift accepted")
	}
	collection, err = app.FindCollectionByNameOrId("notes")
	if err != nil {
		t.Fatal(err)
	}
	if got := collection.GetIndex("idx_pbvex_notes_by_body"); !strings.Contains(got, "UNIQUE") {
		t.Fatalf("incompatible collision was rewritten: %q", got)
	}
}

func TestMaterializeSchemaRejectsBackingFieldContractDrift(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	manifest := DeploymentManifest{Schema: map[string]any{"tables": []any{map[string]any{
		"tableName": "owned", "fields": map[string]any{"name": map[string]any{"type": "string"}},
	}}}}
	if err := materializeSchema(context.Background(), app, manifest); err != nil {
		t.Fatal(err)
	}
	owned, err := app.FindCollectionByNameOrId("owned")
	if err != nil {
		t.Fatal(err)
	}
	owned.Fields.GetByName("_pbvex_data").(*core.JSONField).Required = false
	if err := app.Save(owned); err != nil {
		t.Fatal(err)
	}
	if err := materializeSchema(context.Background(), app, manifest); err == nil {
		t.Fatal("incompatible backing field contract was accepted")
	}
}

func TestValidateBackingCollectionLocksRawStorageABI(t *testing.T) {
	if err := validateBackingCollection(backingCollection("locked"), "locked"); err != nil {
		t.Fatalf("fresh backing collection rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*core.Collection)
	}{
		{"list rule", func(c *core.Collection) { v := ""; c.ListRule = &v }},
		{"view rule", func(c *core.Collection) { v := ""; c.ViewRule = &v }},
		{"create rule", func(c *core.Collection) { v := ""; c.CreateRule = &v }},
		{"update rule", func(c *core.Collection) { v := ""; c.UpdateRule = &v }},
		{"delete rule", func(c *core.Collection) { v := ""; c.DeleteRule = &v }},
		{"raw data visible", func(c *core.Collection) { c.Fields.GetByName("_pbvex_data").(*core.JSONField).Hidden = false }},
		{"injected raw field", func(c *core.Collection) { c.Fields.Add(&core.TextField{Name: "bypass", Max: 32}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := backingCollection("locked")
			tc.mutate(c)
			if err := validateBackingCollection(c, "locked"); err == nil {
				t.Fatal("unsafe backing ABI was accepted")
			}
		})
	}
}

func TestMigrationBudgetChargesNormalizedDocumentAndProjection(t *testing.T) {
	var used int64
	if err := chargeMigrationBytes(&used, "{}"); err != nil {
		t.Fatal(err)
	}
	// A small legacy document can expand through defaults. Charging only the
	// source would admit this pair; charging both normalized data and its order
	// projection rejects it before record materialization.
	if err := chargeMigrationBytes(&used, strings.Repeat("n", 33<<20)); err != nil {
		t.Fatal(err)
	}
	if err := chargeMigrationBytes(&used, strings.Repeat("p", 33<<20)); err == nil {
		t.Fatal("normalized/default-expanded migration work exceeded 64 MiB")
	}
}

func TestMaterializeSchemaNormalizesExistingDocumentsAtomically(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	baseFields := map[string]any{"name": map[string]any{"type": "string"}}
	base := DeploymentManifest{Schema: map[string]any{"tables": []any{map[string]any{
		"tableName": "people", "fields": baseFields,
	}}}}
	if err := materializeSchema(context.Background(), app, base); err != nil {
		t.Fatal(err)
	}
	people, err := app.FindCollectionByNameOrId("people")
	if err != nil {
		t.Fatal(err)
	}
	row := core.NewRecord(people)
	row.Set("_pbvex_data", map[string]any{"name": "Ada"})
	order, err := schema.OrderData(baseFields, map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	row.Set(schema.DocumentOrderField, order)
	if err := app.Save(row); err != nil {
		t.Fatal(err)
	}

	withDefaultFields := map[string]any{
		"name":    map[string]any{"type": "string"},
		"channel": map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "general"},
	}
	withDefault := DeploymentManifest{Schema: map[string]any{"tables": []any{map[string]any{
		"tableName": "people", "fields": withDefaultFields,
	}}}}
	if err := app.RunInTransaction(func(tx core.App) error {
		return materializeSchema(context.Background(), tx, withDefault)
	}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := app.FindRecordById("people", row.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := storedDocument(t, reloaded)["channel"]; got != "general" {
		t.Fatalf("default was not backfilled: %#v", storedDocument(t, reloaded))
	}

	// New required fields, removed fields and type changes are all rejected in
	// the transaction; no partial data/projection rewrite may escape.
	invalids := []DeploymentManifest{
		{Schema: map[string]any{"tables": []any{map[string]any{"tableName": "people", "fields": map[string]any{
			"name": map[string]any{"type": "string"}, "channel": map[string]any{"type": "string"}, "required": map[string]any{"type": "string"},
		}}}}},
		{Schema: map[string]any{"tables": []any{map[string]any{"tableName": "people", "fields": map[string]any{
			"name": map[string]any{"type": "string"},
		}}}}},
		{Schema: map[string]any{"tables": []any{map[string]any{"tableName": "people", "fields": map[string]any{
			"name": map[string]any{"type": "boolean"}, "channel": map[string]any{"type": "string"},
		}}}}},
	}
	for i, manifest := range invalids {
		if err := app.RunInTransaction(func(tx core.App) error {
			return materializeSchema(context.Background(), tx, manifest)
		}); err == nil {
			t.Fatalf("invalid migration %d succeeded", i)
		}
		reloaded, err := app.FindRecordById("people", row.Id)
		if err != nil {
			t.Fatal(err)
		}
		if got := storedDocument(t, reloaded)["channel"]; got != "general" {
			t.Fatalf("migration %d escaped transaction: %#v", i, storedDocument(t, reloaded))
		}
	}
}

func TestMaterializeSchemaAuthenticatesPersistedIDReferences(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	base := DeploymentManifest{Schema: map[string]any{"tables": []any{
		map[string]any{"tableName": "targets", "fields": map[string]any{"name": map[string]any{"type": "string"}}},
		map[string]any{"tableName": "refs", "fields": map[string]any{"ref": map[string]any{"type": "string"}}},
	}}}
	if err := materializeSchema(context.Background(), app, base); err != nil {
		t.Fatal(err)
	}
	refs, err := app.FindCollectionByNameOrId("refs")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(struct {
		V int    `json:"v"`
		K int    `json:"k"`
		N string `json:"n"`
		T string `json:"t"`
		R string `json:"r"`
	}{V: 1, K: 1, N: "foreign", T: "targets", R: "abcdefghijklmno"})
	if err != nil {
		t.Fatal(err)
	}
	forged := "pbv1.1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	data := map[string]any{"ref": forged}
	projection, err := schema.OrderData(map[string]any{"ref": map[string]any{"type": "string"}}, data)
	if err != nil {
		t.Fatal(err)
	}
	row := core.NewRecord(refs)
	row.Set("_pbvex_data", data)
	row.Set(schema.DocumentOrderField, projection)
	if err := app.Save(row); err != nil {
		t.Fatal(err)
	}
	upgraded := DeploymentManifest{Schema: map[string]any{"tables": []any{
		map[string]any{"tableName": "targets", "fields": map[string]any{"name": map[string]any{"type": "string"}}},
		map[string]any{"tableName": "refs", "fields": map[string]any{"ref": map[string]any{"type": "id", "tableName": "targets"}}},
	}}}
	if err := app.RunInTransaction(func(tx core.App) error {
		return materializeSchema(context.Background(), tx, upgraded)
	}); err == nil {
		t.Fatal("forged persisted id was accepted during activation")
	}
	reloaded, err := app.FindRecordById("refs", row.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := storedDocument(t, reloaded)["ref"]; got != forged {
		t.Fatalf("failed migration changed stored reference: %#v", got)
	}
}

func TestMaterializeSchemaAuthenticatesDefaultedIDValues(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	base := DeploymentManifest{Schema: map[string]any{"tables": []any{
		map[string]any{"tableName": "targets", "fields": map[string]any{"name": map[string]any{"type": "string"}}},
		map[string]any{"tableName": "refs", "fields": map[string]any{"name": map[string]any{"type": "string"}}},
	}}}
	if err := materializeSchema(context.Background(), app, base); err != nil {
		t.Fatal(err)
	}
	refs, err := app.FindCollectionByNameOrId("refs")
	if err != nil {
		t.Fatal(err)
	}
	row := core.NewRecord(refs)
	row.Set("_pbvex_data", map[string]any{"name": "legacy"})
	order, err := schema.OrderData(map[string]any{"name": map[string]any{"type": "string"}}, map[string]any{"name": "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	row.Set(schema.DocumentOrderField, order)
	if err := app.Save(row); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(struct {
		V int    `json:"v"`
		K int    `json:"k"`
		N string `json:"n"`
		T string `json:"t"`
		R string `json:"r"`
	}{V: 1, K: 1, N: "foreign", T: "targets", R: "abcdefghijklmno"})
	if err != nil {
		t.Fatal(err)
	}
	forged := "pbv1.1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	upgraded := DeploymentManifest{Schema: map[string]any{"tables": []any{
		map[string]any{"tableName": "targets", "fields": map[string]any{"name": map[string]any{"type": "string"}}},
		map[string]any{"tableName": "refs", "fields": map[string]any{
			"name": map[string]any{"type": "string"},
			"ref":  map[string]any{"type": "defaulted", "validator": map[string]any{"type": "id", "tableName": "targets"}, "defaultValue": forged},
		}},
	}}}
	if err := app.RunInTransaction(func(tx core.App) error {
		return materializeSchema(context.Background(), tx, upgraded)
	}); err == nil {
		t.Fatal("forged defaulted v.id was accepted during activation")
	}
	reloaded, err := app.FindRecordById("refs", row.Id)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := storedDocument(t, reloaded)["ref"]; exists {
		t.Fatal("failed default-id migration escaped its transaction")
	}
}
func (*recordingInvoker) Invoke(context.Context, string, string, any, *auth.UserIdentity, string) (any, error) {
	return nil, nil
}
func (*recordingInvoker) InvokeHTTP(context.Context, string, string, *HTTPRequestEnvelope, *auth.UserIdentity, string) (*HTTPResponseEnvelope, error) {
	return nil, nil
}
func (r *recordingInvoker) Drop(id string) { r.dropped = append(r.dropped, id) }

func TestTrimHistoryPreservesActiveAndRollbackTargetAndDropsOnlyDeleted(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	spy := &recordingInvoker{}
	service := NewService(app, NewRepo(), spy, Config{HistoryLimit: 1})
	for _, id := range []string{"old_a", "old_b", "previous", "active"} {
		if _, err := service.repo.CreateDeployment(service.internalCtx(), app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: id}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
			t.Fatal(err)
		}
	}
	// Explicit ordering: protected deployments are oldest; only eligible
	// deployments compete for the single inactive history slot.
	base := types.NowDateTime().Add(-10 * time.Hour)
	for i, id := range []string{"active", "previous", "old_a", "old_b"} {
		record, err := service.repo.GetDeployment(context.Background(), app, id)
		if err != nil {
			t.Fatal(err)
		}
		record.Set("created", base.Add(time.Duration(i)*time.Hour))
		if err := app.SaveWithContext(service.internalCtx(), record); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.repo.SetDeploymentActive(service.internalCtx(), app, "active", true); err != nil {
		t.Fatal(err)
	}
	state, err := service.repo.GetState(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	state.Set(schema.FieldActiveID, "active")
	state.Set(schema.FieldPreviousID, "previous")
	if err := service.repo.SaveState(service.internalCtx(), app, state); err != nil {
		t.Fatal(err)
	}
	if err := service.trimHistory(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"active", "previous", "old_b"} {
		if _, err := service.repo.GetDeployment(context.Background(), app, id); err != nil {
			t.Fatalf("preserved %s: %v", id, err)
		}
	}
	if _, err := service.repo.GetDeployment(context.Background(), app, "old_a"); err == nil {
		t.Fatal("oldest eligible deployment retained")
	}
	if len(spy.dropped) != 1 || spy.dropped[0] != "old_a" {
		t.Fatalf("drops %#v", spy.dropped)
	}
}

func TestDeleteOldestInactiveRespectsPinCount(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}

	spy := &recordingInvoker{}
	service := NewService(app, NewRepo(), spy, Config{HistoryLimit: 0})
	ctx := schema.WithApp(context.Background(), app)

	if _, err := service.repo.CreateDeployment(ctx, app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "pinned"}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	if err := service.Pin(ctx, "pinned", +1); err != nil {
		t.Fatalf("Pin failed: %v", err)
	}

	deleted, err := service.repo.DeleteOldestInactive(ctx, app, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected no deletion while pinned, deleted %#v", deleted)
	}

	if err := service.Pin(ctx, "pinned", -1); err != nil {
		t.Fatalf("Pin -1 failed: %v", err)
	}
	deleted, err = service.repo.DeleteOldestInactive(ctx, app, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "pinned" {
		t.Fatalf("expected pinned deleted after unpin, got %#v", deleted)
	}
}

func TestDeleteOldestInactiveRejectsReferencedJob(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}

	spy := &recordingInvoker{}
	service := NewService(app, NewRepo(), spy, Config{HistoryLimit: 0})
	ctx := schema.WithApp(context.Background(), app)

	if _, err := service.repo.CreateDeployment(ctx, app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "referenced"}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}

	// Simulate a job referencing the deployment without touching pinCount.
	jobCol, err := app.FindCollectionByNameOrId(schema.CollectionJobs)
	if err != nil {
		t.Fatal(err)
	}
	job := core.NewRecord(jobCol)
	job.Set(schema.FieldDeploymentID, "referenced")
	job.Set(schema.FieldType, "scheduled")
	job.Set(schema.FieldStatus, "completed")
	job.Set(schema.FieldPayload, "null")
	job.Set(schema.FieldScheduledAt, types.NowDateTime())
	job.Set(schema.FieldAttempts, 0)
	if err := app.SaveWithContext(ctx, job); err != nil {
		t.Fatal(err)
	}

	deleted, err := service.repo.DeleteOldestInactive(ctx, app, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected no deletion while a job exists, deleted %#v", deleted)
	}

	if err := app.DeleteWithContext(ctx, job); err != nil {
		t.Fatal(err)
	}
	deleted, err = service.repo.DeleteOldestInactive(ctx, app, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "referenced" {
		t.Fatalf("expected referenced deleted after job removed, got %#v", deleted)
	}
}

func TestPinFailsForTrimmedDeployment(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}

	spy := &recordingInvoker{}
	service := NewService(app, NewRepo(), spy, Config{HistoryLimit: 0})
	ctx := schema.WithApp(context.Background(), app)

	if _, err := service.repo.CreateDeployment(ctx, app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "trimmed"}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	if err := service.Pin(ctx, "trimmed", +1); err != nil {
		t.Fatalf("Pin failed: %v", err)
	}

	// Remove the row directly, as a trim would, after the counter was read.
	rec, err := service.repo.GetDeployment(ctx, app, "trimmed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB().Delete(
		schema.CollectionDeployments,
		dbx.NewExp("id = {:id}", dbx.Params{"id": rec.Id}),
	).WithContext(ctx).Execute(); err != nil {
		t.Fatal(err)
	}

	if err := service.Pin(ctx, "trimmed", +1); err == nil {
		t.Fatal("expected Pin to fail for trimmed deployment")
	}
}

// TestDeleteOldestInactiveDoesNotStarveEligibleDeployments verifies that
// non-deletable candidates (pinned or with terminal jobs) are skipped instead
// of consuming deletion quota, so later eligible deployments are still trimmed.
func TestDeleteOldestInactiveDoesNotStarveEligibleDeployments(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}

	spy := &recordingInvoker{}
	service := NewService(app, NewRepo(), spy, Config{HistoryLimit: 0})
	ctx := schema.WithApp(context.Background(), app)

	// "pinned" — oldest, has pinCount > 0 so cannot be deleted.
	if _, err := service.repo.CreateDeployment(ctx, app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "pinned"}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	if err := service.Pin(ctx, "pinned", +1); err != nil {
		t.Fatal(err)
	}

	// "eligible" — newer, no jobs, no pin — should be deletable.
	if _, err := service.repo.CreateDeployment(ctx, app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "eligible"}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}

	// keep=0 so both are candidates. "pinned" is older but cannot be deleted.
	// Without the starvation fix, "pinned" would consume the quota slot and
	// "eligible" would never be considered.
	deleted, err := service.repo.DeleteOldestInactive(ctx, app, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "eligible" {
		t.Fatalf("expected only eligible deleted, got %#v", deleted)
	}

	// Verify "pinned" is still present.
	if _, err := service.repo.GetDeployment(ctx, app, "pinned"); err != nil {
		t.Fatalf("pinned should still exist: %v", err)
	}
}

// TestDeleteOldestInactiveKeepQuotaAppliesOnlyToEligible verifies that the
// keep quota retains the newest eligible deployments, not counting pinned
// or job-referenced ones. With pinned d1 + eligible d2,d3 + keep=1, only
// d2 (oldest eligible) is deleted and d3 (newest eligible) is retained.
func TestDeleteOldestInactiveKeepQuotaAppliesOnlyToEligible(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}

	spy := &recordingInvoker{}
	service := NewService(app, NewRepo(), spy, Config{HistoryLimit: 0})
	ctx := schema.WithApp(context.Background(), app)

	// d1 — oldest, pinned (pinCount > 0).
	if _, err := service.repo.CreateDeployment(ctx, app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "d1"}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}
	if err := service.Pin(ctx, "d1", +1); err != nil {
		t.Fatal(err)
	}

	// d2 — eligible, older than d3.
	if _, err := service.repo.CreateDeployment(ctx, app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "d2"}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}

	// d3 — eligible, newest.
	if _, err := service.repo.CreateDeployment(ctx, app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "d3"}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}

	// keep=1: among eligible [d2, d3], retain d3 (newest), delete d2 (oldest).
	// d1 is pinned and must not consume the keep quota.
	deleted, err := service.repo.DeleteOldestInactive(ctx, app, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "d2" {
		t.Fatalf("expected only d2 deleted, got %#v", deleted)
	}

	// d1 (pinned) and d3 (retained by keep) must still exist.
	if _, err := service.repo.GetDeployment(ctx, app, "d1"); err != nil {
		t.Fatalf("d1 should still exist: %v", err)
	}
	if _, err := service.repo.GetDeployment(ctx, app, "d3"); err != nil {
		t.Fatalf("d3 should still exist: %v", err)
	}
}

// TestPinUnderflowReturnsDistinctError verifies that Pin(-1) on a deployment
// that exists but has pinCount=0 returns ErrPinUnderflow, not
// ErrDeploymentNotFound.
func TestPinUnderflowReturnsDistinctError(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}

	spy := &recordingInvoker{}
	service := NewService(app, NewRepo(), spy, Config{HistoryLimit: 0})
	ctx := schema.WithApp(context.Background(), app)

	if _, err := service.repo.CreateDeployment(ctx, app, DeploymentManifest{ProtocolVersion: "v1", DeploymentID: "d1"}, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatal(err)
	}

	// pinCount is 0 (never pinned). Pin(-1) must return ErrPinUnderflow.
	err = service.Pin(ctx, "d1", -1)
	if !errors.Is(err, ErrPinUnderflow) {
		t.Fatalf("expected ErrPinUnderflow, got %v", err)
	}

	// Pin(+1) then Pin(-1) should succeed and bring pinCount back to 0.
	if err := service.Pin(ctx, "d1", +1); err != nil {
		t.Fatal(err)
	}
	if err := service.Pin(ctx, "d1", -1); err != nil {
		t.Fatalf("Pin(-1) after Pin(+1): %v", err)
	}

	// Pin(-1) again should return ErrPinUnderflow (not ErrDeploymentNotFound).
	err = service.Pin(ctx, "d1", -1)
	if !errors.Is(err, ErrPinUnderflow) {
		t.Fatalf("expected ErrPinUnderflow on second decrement, got %v", err)
	}

	// Pin(-1) on a truly missing deployment returns ErrDeploymentNotFound.
	err = service.Pin(ctx, "missing", -1)
	if !errors.Is(err, ErrDeploymentNotFound) {
		t.Fatalf("expected ErrDeploymentNotFound for missing deployment, got %v", err)
	}
}
