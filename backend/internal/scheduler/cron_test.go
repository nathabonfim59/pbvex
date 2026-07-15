package scheduler

import (
	"context"
	"sync"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

type cronEnqueueCall struct {
	deploymentID string
	functionName string
	args         any
}

type recordingCronEnqueuer struct {
	mu    sync.Mutex
	calls []cronEnqueueCall
}

func (e *recordingCronEnqueuer) RunAfter(_ context.Context, delayMs int64, deploymentID, functionName string, args any) (string, error) {
	if delayMs != 0 {
		panic("cron ticks must enqueue immediately")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, cronEnqueueCall{deploymentID: deploymentID, functionName: functionName, args: args})
	return "job_1", nil
}

func TestCronManagerMirrorsActiveDeploymentAndEnqueuesDurableJobs(t *testing.T) {
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	if err := app.Cron().Add("custom-hook", "@daily", func() {}); err != nil {
		t.Fatal(err)
	}
	enqueuer := &recordingCronEnqueuer{}
	manager := NewCronManager(app, enqueuer)
	manager.ActiveDeploymentChanged("dep_one", deploy.DeploymentManifest{CronJobs: []deploy.CronJobDescriptor{
		{Name: "hourly-cleanup", Schedule: "@hourly", FunctionName: "cleanup", Args: map[string]any{"scope": "expired"}},
	}})

	jobs := app.Cron().Jobs()
	var pbvexJobFound, customJobFound bool
	for _, job := range jobs {
		switch job.Id() {
		case "pbvex:hourly-cleanup":
			pbvexJobFound = true
			if job.Expression() != "0 * * * *" {
				t.Fatalf("expression = %q", job.Expression())
			}
			job.Run()
		case "custom-hook":
			customJobFound = true
		}
	}
	if !pbvexJobFound || !customJobFound {
		t.Fatalf("registered jobs: pbvex=%v custom=%v", pbvexJobFound, customJobFound)
	}

	enqueuer.mu.Lock()
	if len(enqueuer.calls) != 1 || enqueuer.calls[0].deploymentID != "dep_one" || enqueuer.calls[0].functionName != "cleanup" {
		t.Fatalf("enqueue calls = %#v", enqueuer.calls)
	}
	enqueuer.mu.Unlock()

	manager.ActiveDeploymentChanged("dep_two", deploy.DeploymentManifest{CronJobs: []deploy.CronJobDescriptor{
		{Name: "daily-report", Schedule: "0 8 * * *", FunctionName: "report", Args: map[string]any{}},
	}})
	for _, job := range app.Cron().Jobs() {
		if job.Id() == "pbvex:hourly-cleanup" {
			t.Fatal("old active deployment cron was not removed")
		}
	}

	manager.Clear()
	remaining := app.Cron().Jobs()
	customJobFound = false
	for _, job := range remaining {
		if job.Id() == "custom-hook" {
			customJobFound = true
		}
		if job.Id() == "pbvex:daily-report" {
			t.Fatal("clear left a PBVex cron registered")
		}
	}
	if !customJobFound {
		t.Fatalf("clear removed non-PBVex jobs: %#v", remaining)
	}
}
