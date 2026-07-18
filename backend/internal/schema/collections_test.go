package schema

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"testing"
)

func TestProtocolStorageBounds(t *testing.T) {
	deployment := deploymentsCollection()
	if got := deployment.Fields.GetByName(FieldBundle).(*core.TextField).Max; got != 64<<20 {
		t.Fatalf("bundle max = %d", got)
	}
	for _, name := range []string{FieldActiveID, FieldPreviousID} {
		if got := schemaStateCollection().Fields.GetByName(name).(*core.TextField).Max; got != 1024 {
			t.Fatalf("%s max = %d", name, got)
		}
	}
	for _, col := range []*core.Collection{deploymentsCollection()} {
		if field := col.Fields.GetByName(FieldDeploymentID); field != nil && field.(*core.TextField).Max != 1024 {
			t.Fatalf("%s deploymentId bound", col.Name)
		}
	}
	history := migrationHistoryCollection()
	if !history.System || history.ListRule != nil || history.ViewRule != nil || history.CreateRule != nil || history.UpdateRule != nil || history.DeleteRule != nil {
		t.Fatal("migration history collection is not reserved")
	}
	for _, field := range []string{FieldMigrationID, FieldChecksum, FieldSourceHash, FieldTargetHash, FieldDeploymentID, FieldDirection, FieldAppliedAt} {
		if history.Fields.GetByName(field) == nil {
			t.Fatalf("migration history field %q missing", field)
		}
	}
}

func TestComponentCatalogStoresMaximumCanonicalMountPath(t *testing.T) {
	field := componentsCollection().Fields.GetByName(FieldType).(*core.TextField)
	const maximumPath = 32*1024 + 31
	if field.Max < maximumPath {
		t.Fatalf("component catalog path max = %d, want at least %d", field.Max, maximumPath)
	}
}

func TestMergeCollectionRejectsContractDrift(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	cases := []struct {
		name   string
		mutate func(*core.Collection)
	}{
		{"required", func(c *core.Collection) { c.Fields.GetByName(FieldDeploymentID).(*core.TextField).Required = false }},
		{"text max", func(c *core.Collection) { c.Fields.GetByName(FieldDeploymentID).(*core.TextField).Max-- }},
		{"json max", func(c *core.Collection) { c.Fields.GetByName(FieldManifest).(*core.JSONField).MaxSize-- }},
		{"index expression", func(c *core.Collection) {
			c.Indexes[0] = "create unique index idx_pbvex_deployments_deploymentId on _pbvex_deployments (active)"
		}},
		{"index uniqueness", func(c *core.Collection) {
			c.Indexes[0] = "create index idx_pbvex_deployments_deploymentId on _pbvex_deployments (deploymentId)"
		}},
		{"extra index", func(c *core.Collection) {
			c.Indexes = append(c.Indexes, "create index extra on _pbvex_deployments (active)")
		}},
		{"type", func(c *core.Collection) { c.Type = core.CollectionTypeView }}, {"system", func(c *core.Collection) { c.System = false }},
		{"list rule", func(c *core.Collection) { v := "x"; c.ListRule = &v }}, {"view rule", func(c *core.Collection) { v := "x"; c.ViewRule = &v }}, {"create rule", func(c *core.Collection) { v := "x"; c.CreateRule = &v }}, {"update rule", func(c *core.Collection) { v := "x"; c.UpdateRule = &v }}, {"delete rule", func(c *core.Collection) { v := "x"; c.DeleteRule = &v }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := deploymentsCollection()
			tc.mutate(c)
			if err := mergeCollection(app, c, CollectionDeployments); err == nil {
				t.Fatal("expected drift rejection")
			}
		})
	}
}
