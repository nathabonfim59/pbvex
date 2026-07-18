package deploy

import (
	"context"
	"strings"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

type migrationTestInvoker struct {
	invalid     bool
	invalidDown bool
}

func (*migrationTestInvoker) VerifyDeployment(context.Context, string, string, []FunctionDescriptor, []MigrationDescriptor) error {
	return nil
}
func (*migrationTestInvoker) CompileDeployment(string, string, []FunctionDescriptor, []MigrationDescriptor, ...DeploymentConfig) error {
	return nil
}
func (m *migrationTestInvoker) InvokeMigration(_ context.Context, _, _, direction string, document any, _ int64) (any, error) {
	if m.invalid || (m.invalidDown && direction == "down") {
		return map[string]any{"name": float64(42)}, nil
	}
	doc := document.(map[string]any)
	if direction == "down" {
		return map[string]any{"name": doc["name"]}, nil
	}
	return map[string]any{"name": strings.TrimSpace(doc["name"].(string)), "active": true}, nil
}

func migrationTestManifest(id string, fields map[string]any, migrations []MigrationDescriptor) DeploymentManifest {
	return DeploymentManifest{ProtocolVersion: "v1", DeploymentID: id, Schema: map[string]any{"tables": []any{map[string]any{"tableName": "migration_users", "fields": fields}}}, Migrations: migrations}
}

func TestTransactionalMigrationActivationRollbackAndHistory(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	invoker := &migrationTestInvoker{}
	service := NewService(app, NewRepo(), invoker, DefaultConfig())
	fromFields := map[string]any{"name": map[string]any{"type": "string"}}
	toFields := map[string]any{
		"name":    map[string]any{"type": "string"},
		"active":  map[string]any{"type": "boolean"},
		"channel": map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "general"},
	}
	from := map[string]any{"type": "object", "shape": fromFields}
	to := map[string]any{"type": "object", "shape": toFields}
	fromHash, _ := CanonicalHash(from)
	toHash, _ := CanonicalHash(to)
	migration := MigrationDescriptor{ID: "20260718_users", Table: "migration_users", Mode: "transactional", From: from, To: to, SourceSchemaHash: fromHash, TargetSchemaHash: toHash, Checksum: strings.Repeat("a", 64), ModulePath: "pbvex/migrations/users.ts", ExportName: "default", Reversibility: "reversible"}
	source := migrationTestManifest("source", fromFields, nil)
	target := migrationTestManifest("target", toFields, []MigrationDescriptor{migration})
	for _, manifest := range []DeploymentManifest{source, target} {
		if _, err := service.repo.CreateDeployment(service.internalCtx(), app, manifest, "bundle", strings.Repeat("0", 64), 6); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Activate("source", true); err != nil {
		t.Fatal(err)
	}
	collection, err := app.FindCollectionByNameOrId("migration_users")
	if err != nil {
		t.Fatal(err)
	}
	row := core.NewRecord(collection)
	row.Set("created", types.NowDateTime())
	row.Set("_pbvex_data", map[string]any{"name": " Ada "})
	projection, _ := schema.OrderData(fromFields, map[string]any{"name": " Ada "})
	row.Set(schema.DocumentOrderField, projection)
	if err := app.Save(row); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("target", true); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := app.FindRecordById("migration_users", row.Id)
	got := storedDocument(t, reloaded)
	if got["name"] != "Ada" || got["active"] != true || got["channel"] != "general" {
		t.Fatalf("up result %#v", got)
	}
	history := []*core.Record{}
	if err := app.RecordQuery(schema.CollectionMigrationHistory).All(&history); err != nil || len(history) != 1 || history[0].GetString(schema.FieldDirection) != "up" {
		t.Fatalf("up history %#v, %v", history, err)
	}
	invoker.invalidDown = true
	if _, err := service.Rollback("target"); err == nil {
		t.Fatal("invalid down output rolled back deployment")
	}
	activeAfterFailure, _ := service.Active()
	reloaded, _ = app.FindRecordById("migration_users", row.Id)
	if activeAfterFailure.DeploymentID != "target" || storedDocument(t, reloaded)["active"] != true {
		t.Fatal("failed down migration was not atomic")
	}
	invoker.invalidDown = false
	if _, err := service.Rollback("target"); err != nil {
		t.Fatal(err)
	}
	reloaded, _ = app.FindRecordById("migration_users", row.Id)
	got = storedDocument(t, reloaded)
	if got["name"] != "Ada" || got["active"] != nil || got["channel"] != nil {
		t.Fatalf("down result %#v", got)
	}
	history = nil
	if err := app.RecordQuery(schema.CollectionMigrationHistory).All(&history); err != nil || len(history) != 2 {
		t.Fatalf("rollback history count=%d err=%v", len(history), err)
	}

	bad := migration
	bad.Checksum = strings.Repeat("b", 64)
	badTarget := migrationTestManifest("checksum_reuse", toFields, []MigrationDescriptor{bad})
	if _, err := service.repo.CreateDeployment(service.internalCtx(), app, badTarget, "bundle", strings.Repeat("0", 64), 6); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate("checksum_reuse", true); err == nil {
		t.Fatal("migration id checksum reuse was accepted")
	}
	active, err := service.Active()
	if err != nil || active.DeploymentID != "source" {
		t.Fatalf("failed activation switched deployment: %#v, %v", active, err)
	}
}

func TestInvalidMigrationOutputRollsBackWritesAndSwitch(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	invoker := &migrationTestInvoker{}
	service := NewService(app, NewRepo(), invoker, DefaultConfig())
	fromFields := map[string]any{"name": map[string]any{"type": "string"}}
	toFields := map[string]any{"name": map[string]any{"type": "string"}, "active": map[string]any{"type": "boolean"}}
	from := map[string]any{"type": "object", "shape": fromFields}
	to := map[string]any{"type": "object", "shape": toFields}
	fromHash, _ := CanonicalHash(from)
	toHash, _ := CanonicalHash(to)
	migration := MigrationDescriptor{ID: "invalid_output", Table: "migration_users", Mode: "transactional", From: from, To: to, SourceSchemaHash: fromHash, TargetSchemaHash: toHash, Checksum: strings.Repeat("c", 64), ModulePath: "pbvex/migrations/invalid.ts", ExportName: "default", Reversibility: "reversible"}
	for _, manifest := range []DeploymentManifest{migrationTestManifest("old", fromFields, nil), migrationTestManifest("bad", toFields, []MigrationDescriptor{migration})} {
		if _, err := service.repo.CreateDeployment(service.internalCtx(), app, manifest, "bundle", strings.Repeat("0", 64), 6); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Activate("old", true); err != nil {
		t.Fatal(err)
	}
	collection, _ := app.FindCollectionByNameOrId("migration_users")
	row := core.NewRecord(collection)
	row.Set("created", types.NowDateTime())
	row.Set("_pbvex_data", map[string]any{"name": "Ada"})
	projection, _ := schema.OrderData(fromFields, map[string]any{"name": "Ada"})
	row.Set(schema.DocumentOrderField, projection)
	if err := app.Save(row); err != nil {
		t.Fatal(err)
	}
	invoker.invalid = true
	if _, err := service.Activate("bad", true); err == nil {
		t.Fatal("invalid migration output activated")
	}
	reloaded, _ := app.FindRecordById("migration_users", row.Id)
	if got := storedDocument(t, reloaded); got["name"] != "Ada" || got["active"] != nil {
		t.Fatalf("failed migration changed row: %#v", got)
	}
	active, _ := service.Active()
	if active.DeploymentID != "old" {
		t.Fatalf("failed migration activated %q", active.DeploymentID)
	}
}

func TestMigrationRowLimitRollsBackEarlierWrites(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := schema.Bootstrap(app); err != nil {
		t.Fatal(err)
	}
	fromFields := map[string]any{"name": map[string]any{"type": "string"}}
	toFields := map[string]any{"name": map[string]any{"type": "string"}, "active": map[string]any{"type": "boolean"}}
	from := map[string]any{"type": "object", "shape": fromFields}
	to := map[string]any{"type": "object", "shape": toFields}
	fromHash, _ := CanonicalHash(from)
	toHash, _ := CanonicalHash(to)
	descriptor := MigrationDescriptor{ID: "row_limit", Table: "migration_users", Mode: "transactional", From: from, To: to, SourceSchemaHash: fromHash, TargetSchemaHash: toHash, Checksum: strings.Repeat("d", 64), ModulePath: "pbvex/migrations/limit.ts", ExportName: "default", Reversibility: "reversible"}
	source := migrationTestManifest("limit_source", fromFields, nil)
	target := migrationTestManifest("limit_target", toFields, []MigrationDescriptor{descriptor})
	if err := materializeSchema(context.Background(), app, source); err != nil {
		t.Fatal(err)
	}
	collection, _ := app.FindCollectionByNameOrId("migration_users")
	rows := make([]*core.Record, 2)
	for i := range rows {
		rows[i] = core.NewRecord(collection)
		rows[i].Set("created", types.NowDateTime())
		rows[i].Set("_pbvex_data", map[string]any{"name": " Ada "})
		projection, _ := schema.OrderData(fromFields, map[string]any{"name": " Ada "})
		rows[i].Set(schema.DocumentOrderField, projection)
		if err := app.Save(rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(app, NewRepo(), &migrationTestInvoker{}, DefaultConfig())
	plans, err := planMigrations(source, target)
	if err != nil {
		t.Fatal(err)
	}
	err = app.RunInTransaction(func(tx core.App) error {
		return service.applyMigrationPlans(context.Background(), tx, "limit_target", "up", target, plans, types.NowDateTime(), &migrationBudget{rows: maxSchemaMigrationRows - 1})
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("row limit result: %v", err)
	}
	for _, row := range rows {
		reloaded, _ := app.FindRecordById("migration_users", row.Id)
		if got := storedDocument(t, reloaded); got["name"] != " Ada " || got["active"] != nil {
			t.Fatalf("limit failure retained a partial write: %#v", got)
		}
	}
	sourceJSON, _ := CanonicalJSON(map[string]any{"name": " Ada "})
	output := map[string]any{"name": "Ada", "active": true}
	outputJSON, _ := CanonicalJSON(output)
	projection, _ := schema.OrderData(toFields, output)
	projectionJSON, _ := CanonicalJSON(projection)
	rowCharge := int64(len(sourceJSON)*2 + len(outputJSON)*3 + len(projectionJSON))
	err = app.RunInTransaction(func(tx core.App) error {
		return service.applyMigrationPlans(context.Background(), tx, "limit_target", "up", target, plans, types.NowDateTime(), &migrationBudget{bytes: maxSchemaMigrationBytes - rowCharge})
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("byte limit result: %v", err)
	}
	for _, row := range rows {
		reloaded, _ := app.FindRecordById("migration_users", row.Id)
		if got := storedDocument(t, reloaded); got["name"] != " Ada " || got["active"] != nil {
			t.Fatalf("byte limit failure retained a partial write: %#v", got)
		}
	}
}

func TestMigrationPlanningAllowsHistoricalStepsAndRejectsUnreachableChains(t *testing.T) {
	fieldsA := map[string]any{"name": map[string]any{"type": "string"}}
	fieldsB := map[string]any{"name": map[string]any{"type": "string"}, "active": map[string]any{"type": "boolean"}}
	fieldsC := map[string]any{"name": map[string]any{"type": "string"}, "active": map[string]any{"type": "boolean"}, "role": map[string]any{"type": "string"}}
	descriptors := make([]MigrationDescriptor, 2)
	for i, pair := range [][2]map[string]any{{fieldsA, fieldsB}, {fieldsB, fieldsC}} {
		from := map[string]any{"type": "object", "shape": pair[0]}
		to := map[string]any{"type": "object", "shape": pair[1]}
		fromHash, _ := CanonicalHash(from)
		toHash, _ := CanonicalHash(to)
		descriptors[i] = MigrationDescriptor{
			ID: string(rune('a' + i)), Table: "migration_users", Mode: "transactional", From: from, To: to,
			SourceSchemaHash: fromHash, TargetSchemaHash: toHash, Checksum: strings.Repeat(string(rune('a'+i)), 64),
			ModulePath: "pbvex/migrations/step.ts", ExportName: "default", Reversibility: "reversible",
		}
	}
	plans, err := planMigrations(migrationTestManifest("source", fieldsB, nil), migrationTestManifest("target", fieldsC, descriptors))
	if err != nil || len(plans) != 1 || len(plans[0].steps) != 1 || plans[0].steps[0].ID != "b" {
		t.Fatalf("historical chain plan = %#v, %v", plans, err)
	}
	stale := descriptors[0]
	stale.TargetSchemaHash = strings.Repeat("f", 64)
	if _, err := planMigrations(migrationTestManifest("source", fieldsA, nil), migrationTestManifest("target", fieldsC, []MigrationDescriptor{stale})); err == nil || !strings.Contains(err.Error(), "does not reach target schema") {
		t.Fatalf("unreachable chain result = %v", err)
	}
}

func TestFunctionOnlyDeploymentHasNoMigrationWorkOrWarning(t *testing.T) {
	fields := map[string]any{"name": map[string]any{"type": "string"}}
	source := migrationTestManifest("source", fields, nil)
	target := migrationTestManifest("target", fields, nil)
	target.Functions = []FunctionDescriptor{{Name: "updated", Type: FunctionTypeQuery, Visibility: FunctionVisibilityPublic, ModulePath: "updated.ts", ExportName: "default"}}
	plans, err := planMigrations(source, target)
	if err != nil {
		t.Fatal(err)
	}
	work, skipped, err := schemaMigrationWork(source, target, plans)
	if err != nil || len(work) != 0 || !skipped["migration_users"] {
		t.Fatalf("function-only work = %#v skipped=%#v err=%v", work, skipped, err)
	}
	if warning := migrationWarning(8000, 0); warning == nil {
		t.Fatal("test setup must cross the warning threshold")
	}
}
