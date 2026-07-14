package storage

import (
	"context"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestRepoGetActiveFilesCount(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()
	if err := schema.Bootstrap(app); err != nil {
		t.Fatalf("failed to bootstrap schema: %v", err)
	}

	repo := NewRepo()
	count, err := repo.GetActiveFilesCount(schema.WithInternalContext(context.Background()), app)
	if err != nil {
		t.Fatalf("GetActiveFilesCount failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 active files, got %d", count)
	}
}
