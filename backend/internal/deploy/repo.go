package deploy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Repo provides persistence access for deployment records.
type Repo struct{}

// NewRepo creates a new deployment repository.
func NewRepo() *Repo { return &Repo{} }

// GetDeployment returns a deployment record by deploymentId.
func (r *Repo) GetDeployment(ctx context.Context, app core.App, id string) (*core.Record, error) {
	record := &core.Record{}
	err := app.RecordQuery(schema.CollectionDeployments).
		WithContext(ctx).
		AndWhere(dbx.NewExp(fmt.Sprintf("%s = {:deploymentId}", schema.FieldDeploymentID), dbx.Params{schema.FieldDeploymentID: id})).
		Limit(1).
		One(record)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeploymentNotFound
		}
		return nil, err
	}
	return record, nil
}

// ListDeployments returns deployments ordered by creation descending.
func (r *Repo) ListDeployments(ctx context.Context, app core.App) ([]*core.Record, error) {
	records := []*core.Record{}
	err := app.RecordQuery(schema.CollectionDeployments).
		WithContext(ctx).
		OrderBy("created DESC").
		All(&records)
	return records, err
}

// CountDeployments returns the total number of deployments.
func (r *Repo) CountDeployments(ctx context.Context, app core.App) (int64, error) {
	var count int64
	err := app.DB().NewQuery("SELECT COUNT(*) FROM [[" + schema.CollectionDeployments + "]] ").WithContext(ctx).Row(&count)
	return count, err
}

// CreateDeployment stores a new deployment record.
func (r *Repo) CreateDeployment(ctx context.Context, app core.App, manifest DeploymentManifest, bundleJS string, bundleHash string, bundleSize int64) (*core.Record, error) {
	col, err := app.FindCollectionByNameOrId(schema.CollectionDeployments)
	if err != nil {
		return nil, err
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize manifest: %w", err)
	}

	record := core.NewRecord(col)
	record.Set(schema.FieldDeploymentID, manifest.DeploymentID)
	record.Set(schema.FieldManifest, string(manifestJSON))
	record.Set(schema.FieldBundleHash, bundleHash)
	record.Set(schema.FieldBundleSize, bundleSize)
	record.Set(schema.FieldBundle, bundleJS)
	record.Set(schema.FieldActive, false)
	now := types.NowDateTime()
	record.Set("created", now)
	record.Set("updated", now)

	if err := app.SaveWithContext(ctx, record); err != nil {
		return nil, err
	}

	return record, nil
}

// GetState returns the active state record.
func (r *Repo) GetState(ctx context.Context, app core.App) (*core.Record, error) {
	record := &core.Record{}
	err := app.RecordQuery(schema.CollectionSchemaState).
		WithContext(ctx).
		AndWhere(dbx.NewExp(fmt.Sprintf("%s = {:key}", schema.FieldKey), dbx.Params{schema.FieldKey: schema.StateKeyActive})).
		Limit(1).
		One(record)
	if err != nil {
		return nil, err
	}
	return record, nil
}

// SaveState persists the state record.
func (r *Repo) SaveState(ctx context.Context, app core.App, state *core.Record) error {
	return app.SaveWithContext(ctx, state)
}

// SetDeploymentActive updates the active boolean on a deployment.
func (r *Repo) SetDeploymentActive(ctx context.Context, app core.App, id string, active bool) error {
	record, err := r.GetDeployment(ctx, app, id)
	if err != nil {
		return err
	}
	record.Set(schema.FieldActive, active)
	record.Set("updated", types.NowDateTime())
	return app.SaveWithContext(ctx, record)
}

// SetDeploymentActivatedAt updates the activated timestamp on a deployment.
func (r *Repo) SetDeploymentActivatedAt(ctx context.Context, app core.App, id string, activatedAt types.DateTime) error {
	record, err := r.GetDeployment(ctx, app, id)
	if err != nil {
		return err
	}
	record.Set(schema.FieldActivatedAt, activatedAt)
	record.Set("updated", types.NowDateTime())
	return app.SaveWithContext(ctx, record)
}

// DeleteOldestInactive removes the oldest inactive deployments beyond the limit.
// Protected (active/previous), pinned (pinCount > 0), and job-referenced
// deployments are filtered out BEFORE applying the keep quota so they cannot
// consume deletion slots and starve later eligible deployments. The remaining
// deletable candidates are ordered oldest-first; the newest `keep` are retained
// and the rest are deleted with a conditional DELETE (pinCount = 0 AND NOT
// EXISTS job) that remains atomic with concurrent job creation/retry.
func (r *Repo) DeleteOldestInactive(ctx context.Context, app core.App, keep int) ([]string, error) {
	state, err := r.GetState(ctx, app)
	if err != nil {
		return nil, err
	}
	activeID := state.GetString(schema.FieldActiveID)
	previousID := state.GetString(schema.FieldPreviousID)

	// Load all jobs to build the set of job-referenced deployments.
	jobs := []*core.Record{}
	if err := app.RecordQuery(schema.CollectionJobs).All(&jobs); err != nil {
		return nil, err
	}
	jobDeployments := make(map[string]struct{})
	for _, job := range jobs {
		jobDeployments[job.GetString(schema.FieldDeploymentID)] = struct{}{}
	}

	records := []*core.Record{}
	err = app.RecordQuery(schema.CollectionDeployments).
		WithContext(ctx).
		AndWhere(dbx.NewExp("active = FALSE AND deploymentId != {:activeId} AND deploymentId != {:previousId}", dbx.Params{"activeId": activeID, "previousId": previousID})).
		OrderBy("created ASC").
		All(&records)
	if err != nil {
		return nil, err
	}

	// Filter to deletable candidates: no jobs and pinCount = 0.
	deletable := make([]*core.Record, 0, len(records))
	for _, record := range records {
		deploymentID := record.GetString(schema.FieldDeploymentID)
		if _, referenced := jobDeployments[deploymentID]; referenced {
			continue
		}
		if record.GetInt(schema.FieldPinCount) > 0 {
			continue
		}
		deletable = append(deletable, record)
	}
	if len(deletable) <= keep {
		return nil, nil
	}
	toDelete := len(deletable) - keep
	deleted := make([]string, 0, toDelete)
	for _, rec := range deletable[:toDelete] {
		deploymentID := rec.GetString(schema.FieldDeploymentID)
		res, err := app.DB().Delete(
			schema.CollectionDeployments,
			dbx.NewExp(
				"id = {:id} AND active = FALSE AND (pinCount = 0 OR pinCount IS NULL) AND NOT EXISTS (SELECT 1 FROM "+schema.CollectionJobs+" WHERE "+schema.FieldDeploymentID+" = {:deploymentId})",
				dbx.Params{"id": rec.Id, "deploymentId": deploymentID},
			),
		).WithContext(ctx).Execute()
		if err != nil {
			return nil, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected > 0 {
			deleted = append(deleted, deploymentID)
		}
	}
	return deleted, nil
}

// ApiError maps deployment errors to router API errors.
func (r *Repo) ApiError(err error) *router.ApiError {
	switch {
	case errors.Is(err, ErrDeploymentNotFound):
		return router.NewNotFoundError("Deployment not found.", err)
	case errors.Is(err, ErrActiveNotFound):
		return router.NewNotFoundError("No active deployment.", err)
	case errors.Is(err, ErrAlreadyActive):
		return router.NewBadRequestError("Deployment is already active.", err)
	case errors.Is(err, ErrInvalidBundle):
		return router.NewBadRequestError("Invalid bundle.", err)
	case errors.Is(err, ErrInvalidManifest):
		return router.NewBadRequestError("Invalid manifest.", err)
	case errors.Is(err, ErrActivationFailed):
		return router.NewBadRequestError("Activation failed.", err)
	case errors.Is(err, ErrForbidden):
		return router.NewForbiddenError("Forbidden.", err)
	default:
		return router.NewBadRequestError("Deployment operation failed.", err)
	}
}
