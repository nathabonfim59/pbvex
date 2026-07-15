package scheduler

import (
	"context"
	"sync"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/core"
)

const cronJobPrefix = "pbvex:"

type cronEnqueuer interface {
	RunAfter(ctx context.Context, delayMs int64, deploymentID, functionName string, args any) (string, error)
}

// CronManager mirrors the active deployment's recurring definitions into the
// PocketBase app-level cron registry. Cron ticks enqueue ordinary durable PBVex
// jobs rather than invoking application code in PocketBase's cron goroutine.
type CronManager struct {
	app      core.App
	enqueuer cronEnqueuer

	mu         sync.Mutex
	registered map[string]struct{}
}

func NewCronManager(app core.App, enqueuer cronEnqueuer) *CronManager {
	return &CronManager{app: app, enqueuer: enqueuer, registered: map[string]struct{}{}}
}

// ActiveDeploymentChanged implements deploy.ActivationObserver.
func (m *CronManager) ActiveDeploymentChanged(deploymentID string, manifest deploy.DeploymentManifest) {
	m.mu.Lock()
	defer m.mu.Unlock()

	desired := make(map[string]struct{}, len(manifest.CronJobs))
	for _, definition := range manifest.CronJobs {
		jobID := cronJobPrefix + definition.Name
		desired[jobID] = struct{}{}
	}
	for jobID := range m.registered {
		if _, keep := desired[jobID]; !keep {
			m.app.Cron().Remove(jobID)
			delete(m.registered, jobID)
		}
	}

	for _, definition := range manifest.CronJobs {
		definition := definition
		jobID := cronJobPrefix + definition.Name
		err := m.app.Cron().Add(jobID, definition.Schedule, func() {
			if _, err := m.enqueuer.RunAfter(context.Background(), 0, deploymentID, definition.FunctionName, definition.Args); err != nil {
				m.app.Logger().Error(
					"PBVex cron job enqueue failed",
					"cronId", jobID,
					"deploymentId", deploymentID,
					"functionName", definition.FunctionName,
					"error", err,
				)
			}
		})
		if err != nil {
			// Manifest validation uses the same PocketBase parser, so this is an
			// unexpected defensive path rather than a user-input failure.
			m.app.Logger().Error("PBVex cron job registration failed", "cronId", jobID, "error", err)
			continue
		}
		m.registered[jobID] = struct{}{}
	}
}

// Clear removes only jobs registered by this manager, preserving PocketBase
// built-ins and jobs installed by Go or JS extensions.
func (m *CronManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for jobID := range m.registered {
		m.app.Cron().Remove(jobID)
		delete(m.registered, jobID)
	}
}
