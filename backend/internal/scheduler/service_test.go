package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

type testExecutor struct {
	mu                sync.Mutex
	calls             []testInvocation
	callCnt           atomic.Int32
	err               error
	resp              any
	sleep             time.Duration
	resolveErr        error
	resolveHash       string
	resolveVisibility deploy.FunctionVisibility
	resolveFuncType   deploy.FunctionType
	resolveManifest   *deploy.DeploymentManifest
	invokeFn          func(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error)
}

type testInvocation struct {
	DeploymentID string
	BundleHash   string
	FunctionName string
	Args         any
}

func (e *testExecutor) InvokeDeploymentSnapshot(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error) {
	e.callCnt.Add(1)
	if e.invokeFn != nil {
		return e.invokeFn(ctx, deploymentID, bundleHash, functionName, args)
	}
	e.mu.Lock()
	e.calls = append(e.calls, testInvocation{DeploymentID: deploymentID, BundleHash: bundleHash, FunctionName: functionName, Args: args})
	e.mu.Unlock()
	if e.sleep > 0 {
		select {
		case <-time.After(e.sleep):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if e.err != nil {
		return nil, e.err
	}
	return e.resp, nil
}

func (e *testExecutor) Resolve(ctx context.Context, deploymentID string) (deploy.DeploymentManifest, string, string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.resolveErr != nil {
		return deploy.DeploymentManifest{}, "", "", e.resolveErr
	}
	if e.resolveManifest != nil {
		manifest := *e.resolveManifest
		manifest.DeploymentID = deploymentID
		hash := e.resolveHash
		if hash == "" {
			hash = "h1"
		}
		return manifest, hash, "bundle", nil
	}
	hash := e.resolveHash
	if hash == "" {
		hash = "h1"
	}
	vis := e.resolveVisibility
	if vis == "" {
		vis = deploy.FunctionVisibilityPublic
	}
	fnType := e.resolveFuncType
	if fnType == "" {
		fnType = deploy.FunctionTypeMutation
	}
	return deploy.DeploymentManifest{
		DeploymentID: deploymentID,
		Functions: []deploy.FunctionDescriptor{
			{
				Name:       "hello",
				Type:       fnType,
				Visibility: vis,
				ModulePath: "pbvex/hello.ts",
				ExportName: "hello",
			},
		},
	}, hash, "js", nil
}

func (e *testExecutor) Pin(ctx context.Context, deploymentID string, delta int) error {
	return nil
}

type noopInvoker struct{ dropped []string }

func (noopInvoker) Compile(string, string, []deploy.FunctionDescriptor) error { return nil }
func (noopInvoker) Verify(context.Context, string, string, []deploy.FunctionDescriptor) error {
	return nil
}
func (noopInvoker) Invoke(context.Context, string, string, any) (any, error) { return nil, nil }
func (n *noopInvoker) Drop(id string)                                        { n.dropped = append(n.dropped, id) }

func createTestDeployment(t *testing.T, ctx context.Context, app core.App, repo *deploy.Repo, id string) {
	t.Helper()
	createTestDeploymentWith(t, ctx, app, repo, id, deploy.FunctionVisibilityPublic, deploy.FunctionTypeMutation)
}

func createTestDeploymentWith(t *testing.T, ctx context.Context, app core.App, repo *deploy.Repo, id string, vis deploy.FunctionVisibility, fnType deploy.FunctionType) {
	t.Helper()
	manifest := deploy.DeploymentManifest{
		ProtocolVersion: "v1",
		DeploymentID:    id,
		Functions: []deploy.FunctionDescriptor{
			{
				Name:       "hello",
				Type:       fnType,
				Visibility: vis,
				ModulePath: "pbvex/hello.ts",
				ExportName: "hello",
			},
		},
	}
	if _, err := repo.CreateDeployment(ctx, app, manifest, "x", "0000000000000000000000000000000000000000000000000000000000000000", 1); err != nil {
		t.Fatalf("CreateDeployment(%s): %v", id, err)
	}
}

func newSchedulerTestApp(t *testing.T) (*tests.TestApp, *Service) {
	t.Helper()
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	if err := app.Bootstrap(); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap app: %v", err)
	}
	if err := schema.Bootstrap(app); err != nil {
		app.Cleanup()
		t.Fatalf("failed to bootstrap schema: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Clock = NewRealClock()
	cfg.PollInterval = 100 * time.Millisecond
	cfg.LeaseDuration = 1 * time.Second
	cfg.MaxExecutionDuration = 5 * time.Second
	cfg.RetryInitialDelay = 100 * time.Millisecond
	cfg.RetryMaxDelay = 1 * time.Second
	cfg.MaxAttempts = 2
	cfg.ClaimBatch = 10
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	svc := NewService(app, &testExecutor{resp: map[string]any{"done": true}}, cfg)
	if err := svc.Start(context.Background()); err != nil {
		app.Cleanup()
		t.Fatalf("failed to start scheduler: %v", err)
	}

	t.Cleanup(func() {
		svc.Stop()
		app.Cleanup()
	})

	return app, svc
}

func waitForStatus(t *testing.T, ctx context.Context, svc *Service, id string, want string, timeout time.Duration) JobStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := svc.Get(ctx, id)
		if err != nil {
			t.Fatalf("failed to get job: %v", err)
		}
		if status.Status == want {
			return *status
		}
		if want == JobStatusCompleted && status.Status == JobStatusFailed {
			t.Fatalf("expected completed, got failed: %s", status.Error)
		}
		if want == JobStatusFailed && status.Status == JobStatusCompleted {
			t.Fatalf("expected failed, got completed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for status %s", want)
	return JobStatus{}
}

func waitGroupWait(t *testing.T, wg *sync.WaitGroup, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for WaitGroup")
	}
}

func waitForWorkerIdle(t *testing.T, svc *Service, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(svc.worker.sem) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for worker runJob goroutines to finish")
}

// expireLease manually sets leaseExpiresAt to the past so a running job
// becomes eligible for reclaim without advancing the fake clock (which
// would trigger heartbeat renewal and re-extend the lease).
func expireLease(t *testing.T, app core.App, clock Clock, id string) {
	t.Helper()
	_, err := app.DB().Update(
		schema.CollectionJobs,
		dbx.Params{schema.FieldLeaseExpiresAt: dateTime(clock.Now().Add(-time.Millisecond))},
		dbx.NewExp("id = {:id}", dbx.Params{"id": id}),
	).Execute()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerRunAfter(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := svc.executor.(*testExecutor)
	id, err := svc.RunAfter(ctx, 0, "d1", "hello", map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("RunAfter failed: %v", err)
	}

	status := waitForStatus(t, ctx, svc, id, JobStatusCompleted, 2*time.Second)
	if status.Attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", status.Attempts)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(exec.calls))
	}
	if exec.calls[0].DeploymentID != "d1" || exec.calls[0].FunctionName != "hello" {
		t.Fatalf("unexpected invocation: %#v", exec.calls[0])
	}
	if exec.calls[0].BundleHash != "h1" {
		t.Fatalf("expected bundle hash pinned, got %q", exec.calls[0].BundleHash)
	}
	if status.Metadata != nil {
		t.Fatalf("expected no metadata for completed job, got %v", status.Metadata)
	}
}

func TestSchedulerRunAt(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	now := time.Now().UnixMilli()
	id, err := svc.RunAt(ctx, now, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAt failed: %v", err)
	}

	status := waitForStatus(t, ctx, svc, id, JobStatusCompleted, 2*time.Second)
	if status.Attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", status.Attempts)
	}
}

func TestSchedulerRunAfterExplicitNullArgs(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := svc.executor.(*testExecutor)
	id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter failed: %v", err)
	}
	waitForStatus(t, ctx, svc, id, JobStatusCompleted, 2*time.Second)
	if exec.calls[0].Args != nil {
		t.Fatalf("expected null args, got %v", exec.calls[0].Args)
	}
}

func TestSchedulerCancel(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	id, err := svc.RunAfter(ctx, 60000, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter failed: %v", err)
	}

	status := waitForStatus(t, ctx, svc, id, JobStatusPending, 500*time.Millisecond)
	if status.Status != JobStatusPending {
		t.Fatalf("expected pending, got %s", status.Status)
	}

	if err := svc.Cancel(ctx, id); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	canceled, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if canceled.Status != JobStatusCanceled {
		t.Fatalf("expected canceled, got %s", canceled.Status)
	}
}

func TestSchedulerPersistsNamespacesAndEnforcesCancelOwner(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	base := schema.WithApp(context.Background(), app)
	owner := "cmp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	target := deploy.RootNamespace
	ctx := runtime.WithScheduleNamespaces(base, owner, target)
	id, err := svc.RunAfter(ctx, 60_000, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter: %v", err)
	}
	record, err := app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		t.Fatal(err)
	}
	storedOwner, storedTarget, err := payloadNamespaces(record)
	if err != nil || storedOwner != owner || storedTarget != target {
		t.Fatalf("namespace payload = %q/%q, %v", storedOwner, storedTarget, err)
	}
	wrong := runtime.WithScheduleNamespaces(base, "cmp_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "")
	if err := svc.Cancel(wrong, id); !errors.Is(err, ErrJobNotCancelable) {
		t.Fatalf("cross-owner cancel = %v", err)
	}
	svc.Stop()
	restarted := NewService(app, svc.executor, svc.config)
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer restarted.Stop()
	if err := restarted.Cancel(ctx, id); err != nil {
		t.Fatalf("owner cancel: %v", err)
	}
}

func TestWorkerRejectsPersistedTargetNamespaceMismatch(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)
	exec := &testExecutor{resp: map[string]any{"done": true}}
	svc.executor = exec
	id, err := svc.RunAfter(ctx, 60_000, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := record.UnmarshalJSONField(schema.FieldPayload, &payload); err != nil {
		t.Fatal(err)
	}
	payload["targetNamespace"] = "cmp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	record.Set(schema.FieldPayload, payload)
	record.Set(schema.FieldScheduledAt, dateTime(time.Now().Add(-time.Second)))
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	svc.worker.Wake()
	waitForStatus(t, ctx, svc, id, JobStatusFailed, 2*time.Second)
	if exec.callCnt.Load() != 0 {
		t.Fatalf("namespace-mismatched job invoked executor %d times", exec.callCnt.Load())
	}
}

func TestWorkerExecutesPinnedComponentNamespace(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)
	definition := deploy.ComponentDefinition{ComponentID: "def_store", ModulePaths: []string{"store.ts"}, ModuleHashes: map[string]string{"store.ts": strings.Repeat("0", 64)}}
	manifest := deploy.DeploymentManifest{
		Functions:  []deploy.FunctionDescriptor{{Name: "componentTask", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityInternal, ModulePath: "pbvex/components/store/store.ts", ExportName: "task"}},
		Components: &deploy.ComponentGraph{Definitions: []deploy.ComponentDefinition{definition}, Mounts: []deploy.ComponentMount{{Name: "store", ComponentID: definition.ComponentID}}},
	}
	exec := &testExecutor{resp: map[string]any{"done": true}, resolveManifest: &manifest}
	svc.executor = exec
	namespace, err := deploy.ComponentNamespaceID("store")
	if err != nil {
		t.Fatal(err)
	}
	scheduleCtx := runtime.WithScheduleNamespaces(ctx, namespace, namespace)
	id, err := svc.RunAfter(scheduleCtx, 60_000, "d1", "componentTask", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, ctx, svc, id, JobStatusPending, 500*time.Millisecond)
	svc.Stop()
	record, err := app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		t.Fatal(err)
	}
	record.Set(schema.FieldScheduledAt, dateTime(time.Now().Add(-time.Second)))
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(app, exec, svc.config)
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer restarted.Stop()
	restarted.worker.Wake()
	waitForStatus(t, ctx, restarted, id, JobStatusCompleted, 2*time.Second)
	if exec.callCnt.Load() != 1 {
		t.Fatalf("component scheduled invocation count = %d", exec.callCnt.Load())
	}
}

func TestSchedulerRetry(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := &testExecutor{err: errors.New("boom")}
	svc.executor = exec

	id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter failed: %v", err)
	}

	status := waitForStatus(t, ctx, svc, id, JobStatusFailed, 3*time.Second)
	if status.Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", status.Attempts)
	}
	if exec.callCnt.Load() != 2 {
		t.Fatalf("expected 2 invocations, got %d", exec.callCnt.Load())
	}
	if status.Error == "" {
		t.Fatal("expected error on failed job")
	}
	if status.Metadata == nil {
		t.Fatal("expected metadata on failed job")
	}
}

func TestSchedulerListAndGet(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	id, err := svc.RunAfter(ctx, 60000, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter failed: %v", err)
	}

	result, err := svc.List(ctx, "", 10, "")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("expected 1 job, got total=%d items=%d", result.Total, len(result.Items))
	}
	if result.Items[0].ID != id {
		t.Fatalf("expected job id %s, got %s", id, result.Items[0].ID)
	}

	got, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != id {
		t.Fatalf("expected id %s, got %s", id, got.ID)
	}

	filtered, err := svc.List(ctx, JobStatusPending, 10, "")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if filtered.Total != 1 {
		t.Fatalf("expected 1 pending job, got %d", filtered.Total)
	}

	_, err = svc.List(ctx, "badstatus", 10, "")
	if !errors.Is(err, ErrJobInvalidStatus) {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}

func TestSchedulerRetryAPI(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := &testExecutor{err: errors.New("boom")}
	svc.executor = exec

	id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter failed: %v", err)
	}

	status := waitForStatus(t, ctx, svc, id, JobStatusFailed, 3*time.Second)
	if status.Status != JobStatusFailed {
		t.Fatalf("expected failed, got %s", status.Status)
	}

	// Retry wakes the worker after committing. Stop this worker so the reset
	// state can be observed deterministically before a new attempt claims it.
	svc.Stop()
	if err := svc.Retry(ctx, id); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}

	retried, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retried.Status != JobStatusPending {
		t.Fatalf("expected pending after retry, got %s", retried.Status)
	}
	if retried.Attempts != 0 {
		t.Fatalf("expected attempts reset to 0, got %d", retried.Attempts)
	}

	restarted := NewService(app, exec, svc.config)
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatalf("restart scheduler: %v", err)
	}
	defer restarted.Stop()

	status = waitForStatus(t, ctx, restarted, id, JobStatusFailed, 3*time.Second)
	if status.Attempts != 2 {
		t.Fatalf("expected 2 attempts after retry, got %d", status.Attempts)
	}
}

func TestSchedulerRollbackDoesNotCreateJob(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := svc.executor.(*testExecutor)
	err := app.RunInTransaction(func(txApp core.App) error {
		txCtx := schema.WithApp(context.Background(), txApp)
		id, err := svc.RunAfter(txCtx, 0, "d1", "hello", nil)
		if err != nil {
			return err
		}
		if id == "" {
			return errors.New("expected job id")
		}
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("expected rollback error")
	}

	time.Sleep(200 * time.Millisecond)
	result, err := svc.List(ctx, "", 10, "")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if result.Total != 0 {
		t.Fatalf("expected 0 jobs after rollback, got %d", result.Total)
	}
	if exec.callCnt.Load() != 0 {
		t.Fatalf("expected no worker invocation after rollback, got %d", exec.callCnt.Load())
	}
}

func TestSchedulerCommitWakesWorker(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := svc.executor.(*testExecutor)
	var id string
	err := app.RunInTransaction(func(txApp core.App) error {
		txCtx := schema.WithApp(context.Background(), txApp)
		var err error
		id, err = svc.RunAfter(txCtx, 0, "d1", "hello", nil)
		return err
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	waitForStatus(t, ctx, svc, id, JobStatusCompleted, 2*time.Second)
	if exec.callCnt.Load() != 1 {
		t.Fatalf("expected worker invocation after commit, got %d", exec.callCnt.Load())
	}
}

func TestSchedulerTwoWorkersClaimRace(t *testing.T) {
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

	clock := NewFakeClock(time.Now())
	cfg := DefaultConfig()
	cfg.Clock = clock
	cfg.PollInterval = 10 * time.Millisecond
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.RenewInterval = 30 * time.Millisecond
	cfg.MaxExecutionDuration = 500 * time.Millisecond
	cfg.RetryInitialDelay = 10 * time.Millisecond
	cfg.RetryMaxDelay = 100 * time.Millisecond
	cfg.MaxAttempts = 5
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	exec := &testExecutor{sleep: 200 * time.Millisecond, resp: map[string]any{"done": true}}

	svc1 := NewService(app, exec, cfg)
	svc2 := NewService(app, exec, cfg)

	if err := svc1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc1.Stop()
	defer svc2.Stop()

	ctx := schema.WithApp(context.Background(), app)
	id, err := svc1.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Drive both workers past the original lease expiry.
	clock.Advance(150 * time.Millisecond)

	status := JobStatus{}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, err := svc1.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if s.Status == JobStatusCompleted {
			status = *s
			break
		}
		clock.Advance(50 * time.Millisecond)
		time.Sleep(5 * time.Millisecond)
	}
	if status.Status != JobStatusCompleted {
		t.Fatalf("expected completed, got %s", status.Status)
	}
	if exec.callCnt.Load() < 1 {
		t.Fatalf("expected at least one invocation, got %d", exec.callCnt.Load())
	}
	// At-least-once allows one or more, but only one result should be persisted.
	if status.Attempts < 1 {
		t.Fatalf("expected at least 1 attempt persisted, got %d", status.Attempts)
	}
}

func TestSchedulerLongJobBeyondLease(t *testing.T) {
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

	clock := NewFakeClock(time.Now())
	cfg := DefaultConfig()
	cfg.Clock = clock
	cfg.PollInterval = 10 * time.Millisecond
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.RenewInterval = 30 * time.Millisecond
	cfg.MaxExecutionDuration = 500 * time.Millisecond
	cfg.RetryInitialDelay = 10 * time.Millisecond
	cfg.RetryMaxDelay = 100 * time.Millisecond
	cfg.MaxAttempts = 2
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	exec := &testExecutor{sleep: 200 * time.Millisecond, resp: map[string]any{"done": true}}

	svc := NewService(app, exec, cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	ctx := schema.WithApp(context.Background(), app)
	id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Advance the clock to trigger several renew ticks.
	for i := 0; i < 10; i++ {
		clock.Advance(50 * time.Millisecond)
		time.Sleep(5 * time.Millisecond)
	}

	status := waitForStatus(t, ctx, svc, id, JobStatusCompleted, 2*time.Second)
	if status.Status != JobStatusCompleted {
		t.Fatalf("expected completed, got %s", status.Status)
	}
	if exec.callCnt.Load() != 1 {
		t.Fatalf("expected exactly one invocation, got %d", exec.callCnt.Load())
	}
}

func TestSchedulerRestartReleasesRunning(t *testing.T) {
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

	cfg := DefaultConfig()
	cfg.PollInterval = 1 * time.Hour
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.RenewInterval = 1 * time.Hour
	cfg.MaxExecutionDuration = 500 * time.Millisecond
	cfg.RetryInitialDelay = 10 * time.Millisecond
	cfg.RetryMaxDelay = 100 * time.Millisecond
	cfg.MaxAttempts = 5
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	var started sync.WaitGroup
	started.Add(1)

	exec := &testExecutor{resp: map[string]any{"done": true}}
	exec.invokeFn = func(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error) {
		if exec.callCnt.Load() == 1 {
			started.Done()
			// Block until the worker is stopped; shutdown should release the lease.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1 * time.Minute):
				return map[string]any{"done": true}, nil
			}
		}
		return map[string]any{"done": true}, nil
	}

	svc1 := NewService(app, exec, cfg)
	if err := svc1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx := schema.WithApp(context.Background(), app)
	id, err := svc1.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	waitGroupWait(t, &started, 1*time.Second)
	svc1.Stop()

	// After svc1 stops, the running job should be released and requeued.
	svc2 := NewService(app, exec, cfg)
	if err := svc2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc2.Stop()
	svc2.worker.Wake()

	status := waitForStatus(t, ctx, svc2, id, JobStatusCompleted, 2*time.Second)
	if status.Status != JobStatusCompleted {
		t.Fatalf("expected completed after restart, got %s", status.Status)
	}
	if exec.callCnt.Load() != 2 {
		t.Fatalf("expected 2 invocations (original + restart), got %d", exec.callCnt.Load())
	}
	if status.Attempts < 1 {
		t.Fatalf("expected at least one attempt persisted, got %d", status.Attempts)
	}
}

func TestSchedulerRetryRejectsTrimmedSnapshot(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := &testExecutor{err: errors.New("boom")}
	svc.executor = exec

	id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter failed: %v", err)
	}

	waitForStatus(t, ctx, svc, id, JobStatusFailed, 3*time.Second)

	exec.resolveErr = deploy.ErrDeploymentNotFound
	if err := svc.Retry(ctx, id); !errors.Is(err, ErrDeploymentSnapshotNotFound) {
		t.Fatalf("expected ErrDeploymentSnapshotNotFound, got %v", err)
	}

	status, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if status.Status != JobStatusFailed {
		t.Fatalf("expected status to remain failed, got %s", status.Status)
	}
}

func TestSchedulerRetryRejectsSnapshotMismatch(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := &testExecutor{err: errors.New("boom")}
	svc.executor = exec

	id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter failed: %v", err)
	}

	waitForStatus(t, ctx, svc, id, JobStatusFailed, 3*time.Second)

	exec.resolveHash = "h2"
	if err := svc.Retry(ctx, id); !errors.Is(err, ErrDeploymentSnapshotNotFound) {
		t.Fatalf("expected ErrDeploymentSnapshotNotFound, got %v", err)
	}

	status, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if status.Status != JobStatusFailed {
		t.Fatalf("expected status to remain failed, got %s", status.Status)
	}
}

func TestSchedulerLeaseTheftDoesNotAllowStaleCompletion(t *testing.T) {
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

	clock := NewFakeClock(time.Now())
	cfg := DefaultConfig()
	cfg.Clock = clock
	cfg.PollInterval = 10 * time.Hour // driven manually
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.RenewInterval = 1 * time.Hour // disable renew so the lease can be stolen
	cfg.MaxExecutionDuration = 500 * time.Millisecond
	cfg.RetryInitialDelay = 10 * time.Millisecond
	cfg.RetryMaxDelay = 100 * time.Millisecond
	cfg.MaxAttempts = 5
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	blockCh := make(chan struct{})
	var started, stolen sync.WaitGroup
	started.Add(1)
	stolen.Add(1)

	exec := &testExecutor{resp: map[string]any{"done": true}}
	exec.invokeFn = func(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error) {
		cnt := exec.callCnt.Load()
		if cnt == 1 {
			started.Done()
			// First owner blocks until the test closes the channel.
			// The lease expires and is stolen by svc2.
			<-blockCh
			return map[string]any{"done": true}, nil
		}
		stolen.Done()
		return map[string]any{"done": true}, nil
	}

	svc1 := NewService(app, exec, cfg)
	svc2 := NewService(app, exec, cfg)
	if err := svc1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc1.Stop()
	defer svc2.Stop()

	ctx := schema.WithApp(context.Background(), app)
	id, err := svc1.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for svc1 to start the invocation.
	waitGroupWait(t, &started, 1*time.Second)

	// Steal the lease with svc2. Expire the lease manually instead of
	// advancing the fake clock (which would trigger heartbeat renewal).
	expireLease(t, app, clock, id)
	for i := 0; i < 50; i++ {
		svc2.worker.Wake()
		time.Sleep(10 * time.Millisecond)
		if exec.callCnt.Load() >= 2 {
			break
		}
	}

	// Wait for svc2 to complete the new invocation.
	waitGroupWait(t, &stolen, 1*time.Second)
	status := waitForStatus(t, ctx, svc1, id, JobStatusCompleted, 2*time.Second)

	if exec.callCnt.Load() != 2 {
		t.Fatalf("expected exactly 2 invocations, got %d", exec.callCnt.Load())
	}
	if status.Attempts != 2 {
		t.Fatalf("expected 2 attempts persisted, got %d", status.Attempts)
	}

	// Unblock the stale worker so it can attempt (and fail) to complete.
	close(blockCh)
	// Stop svc1 and wait for its stale completion attempt to be ignored.
	svc1.Stop()

	final, err := svc1.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != JobStatusCompleted {
		t.Fatalf("expected completion by new owner, got %s", final.Status)
	}
	if final.Attempts != 2 {
		t.Fatalf("expected stale completion to be ignored (attempts=2), got %d", final.Attempts)
	}
	if exec.callCnt.Load() != 2 {
		t.Fatalf("expected no extra invocations after stale completion, got %d", exec.callCnt.Load())
	}
}

func TestWorkerCancelScoping(t *testing.T) {
	svc := &Service{config: Config{MaxConcurrency: 1, Clock: NewRealClock()}}
	w := NewWorker(svc)

	c1 := &cancellation{cancel: func() {}}
	c2 := &cancellation{cancel: func() {}}
	w.cancellers.Store("id/lease1", c1)
	w.cancellers.Store("id/lease2", c2)

	if !w.Cancel("id", "lease1") {
		t.Fatal("expected Cancel(lease1) to succeed")
	}
	if w.Cancel("id", "lease1") {
		t.Fatal("expected Cancel(lease1) to fail after first cancel")
	}
	if !w.Cancel("id", "lease2") {
		t.Fatal("expected Cancel(lease2) to succeed")
	}
	if w.Cancel("id", "lease2") {
		t.Fatal("expected Cancel(lease2) to fail after first cancel")
	}

	if _, ok := w.cancellers.Load("id/lease2"); ok {
		t.Fatal("expected cancellers to be empty after both cancels")
	}
}

// TestSchedulerSchedulePinsDeploymentAgainstTrim verifies the atomic
// trim-vs-schedule contract: scheduleJob pins the deployment (Pin +1) and
// inserts the job in a single transaction, and DeleteOldestInactive uses a
// conditional DELETE that refuses to remove a pinned or job-referenced row.
// SQLite serializes the two transactions so the scenario is deterministic.
func TestSchedulerSchedulePinsDeploymentAgainstTrim(t *testing.T) {
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

	ctx := schema.WithApp(context.Background(), app)
	repo := deploy.NewRepo()
	deploySvc := deploy.NewService(app, repo, &noopInvoker{}, deploy.Config{})

	createTestDeployment(t, ctx, app, repo, "d1")

	cfg := DefaultConfig()
	cfg.Clock = NewRealClock()
	cfg.PollInterval = 1 * time.Hour
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	svc := NewService(app, deploySvc, cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	// Far-future schedule keeps the job pending so the worker does not run it.
	jobID, err := svc.RunAfter(ctx, 60000, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter: %v", err)
	}

	// The schedule transaction pinned the deployment atomically.
	rec, err := repo.GetDeployment(ctx, app, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.GetInt(schema.FieldPinCount); got != 1 {
		t.Fatalf("pinCount after schedule: got %d, want 1", got)
	}

	// Trim must not delete the deployment while a job references it.
	deleted, err := repo.DeleteOldestInactive(ctx, app, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected no deletion while job pending, got %#v", deleted)
	}

	// Cancel the job (terminal, but pin and job record still exist).
	if err := svc.Cancel(ctx, jobID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	deleted, err = repo.DeleteOldestInactive(ctx, app, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected no deletion while canceled job exists, got %#v", deleted)
	}

	// Simulate cleanup: release the pin and delete the job record.
	if err := deploySvc.Pin(ctx, "d1", -1); err != nil {
		t.Fatalf("Pin(-1): %v", err)
	}
	jobRec, err := app.FindRecordById(schema.CollectionJobs, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteWithContext(ctx, jobRec); err != nil {
		t.Fatal(err)
	}

	// Now the deployment is eligible for trim.
	deleted, err = repo.DeleteOldestInactive(ctx, app, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "d1" {
		t.Fatalf("expected d1 deleted after cleanup, got %#v", deleted)
	}
}

// TestSchedulerHeartbeatExitsBeforeCompletion verifies that the heartbeat
// goroutine is fully joined before terminal handling (complete/fail) runs,
// preventing concurrent mutation of the job record. The invocation blocks
// long enough for several renew ticks, then completes. Run with -race to
// catch any concurrent record access between heartbeat and terminal handlers.
func TestSchedulerHeartbeatExitsBeforeCompletion(t *testing.T) {
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

	clock := NewFakeClock(time.Now())
	cfg := DefaultConfig()
	cfg.Clock = clock
	// RunAfter wakes the worker for the initial claim. Keep periodic polling out
	// of this heartbeat-only test so advancing the fake clock cannot also reclaim
	// an intentionally blocked job whose lease appears to expire.
	cfg.PollInterval = time.Hour
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.RenewInterval = 30 * time.Millisecond
	cfg.MaxExecutionDuration = 10 * time.Second
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	blockCh := make(chan struct{})
	started := make(chan struct{})
	var startedOnce sync.Once

	exec := &testExecutor{resp: map[string]any{"done": true}}
	exec.invokeFn = func(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-blockCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return map[string]any{"done": true}, nil
	}

	svc := NewService(app, exec, cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	ctx := schema.WithApp(context.Background(), app)
	id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for invocation to start")
	}

	// Drive several renew ticks while the invocation is blocked. This
	// exercises concurrent heartbeat + invocation. Under -race any
	// concurrent record mutation (the old renewLoop mutated *core.Record)
	// would be detected.
	for i := 0; i < 5; i++ {
		clock.Advance(50 * time.Millisecond)
		time.Sleep(10 * time.Millisecond)
	}

	// Release the invocation. The heartbeat must be joined (renewWg.Wait)
	// before complete() touches the record.
	close(blockCh)

	status := waitForStatus(t, ctx, svc, id, JobStatusCompleted, 3*time.Second)
	if status.Status != JobStatusCompleted {
		t.Fatalf("expected completed, got %s", status.Status)
	}
	if exec.callCnt.Load() != 1 {
		t.Fatalf("expected exactly 1 invocation, got %d", exec.callCnt.Load())
	}

	// After completion, advancing the clock must not trigger further renew
	// writes — the heartbeat goroutine has exited and the worker is idle.
	for i := 0; i < 3; i++ {
		clock.Advance(50 * time.Millisecond)
		time.Sleep(10 * time.Millisecond)
	}
	waitForWorkerIdle(t, svc, 2*time.Second)
}

// TestSchedulerReclaimPreservesNewCanceller verifies ownership-safe cancellation
// after a lease theft: the stale attempt's cleanup (CompareAndDelete on
// jobID/oldLease) cannot remove the current attempt's canceller (keyed
// jobID/newLease), so Cancel targeting the current lease still succeeds.
func TestSchedulerReclaimPreservesNewCanceller(t *testing.T) {
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

	clock := NewFakeClock(time.Now())
	cfg := DefaultConfig()
	cfg.Clock = clock
	cfg.PollInterval = 10 * time.Hour
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.RenewInterval = 1 * time.Hour
	cfg.MaxExecutionDuration = 10 * time.Second
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	svc1Block := make(chan struct{})
	var svc1Started, svc2Started, svc1Returned sync.WaitGroup
	svc1Started.Add(1)
	svc2Started.Add(1)
	svc1Returned.Add(1)

	exec := &testExecutor{resp: map[string]any{"done": true}}
	exec.invokeFn = func(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error) {
		switch exec.callCnt.Load() {
		case 1:
			svc1Started.Done()
			select {
			case <-svc1Block:
			case <-ctx.Done():
				svc1Returned.Done()
				return nil, ctx.Err()
			}
			svc1Returned.Done()
			return map[string]any{"stale": true}, nil
		case 2:
			svc2Started.Done()
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return map[string]any{"done": true}, nil
	}

	svc1 := NewService(app, exec, cfg)
	svc2 := NewService(app, exec, cfg)
	if err := svc1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc1.Stop()
	defer svc2.Stop()

	ctx := schema.WithApp(context.Background(), app)
	id, err := svc1.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	// svc1 claims and blocks in the invocation.
	waitGroupWait(t, &svc1Started, 2*time.Second)

	// Steal the lease: expire it manually and wake svc2.
	expireLease(t, app, clock, id)
	for i := 0; i < 100; i++ {
		svc2.worker.Wake()
		time.Sleep(10 * time.Millisecond)
		if exec.callCnt.Load() >= 2 {
			break
		}
	}
	waitGroupWait(t, &svc2Started, 2*time.Second)

	// Let svc1 finish so its runJob defer (CompareAndDelete on id/oldLease)
	// runs. The stale completion is ignored (lease mismatch).
	close(svc1Block)
	waitGroupWait(t, &svc1Returned, 2*time.Second)
	// Allow complete() + defer chain to finish.
	waitForWorkerIdle(t, svc1, 2*time.Second)

	// Read the current lease token (assigned to svc2's attempt).
	jobRec, err := app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		t.Fatal(err)
	}
	currentLease := jobRec.GetString(schema.FieldLease)
	if currentLease == "" {
		t.Fatal("expected non-empty lease after reclaim")
	}

	// svc2's canceller must still be present despite svc1's cleanup.
	// If svc1's defer had used unconditional Delete (old code), the key
	// collision would have removed svc2's canceller.
	if !svc2.worker.Cancel(id, currentLease) {
		t.Fatal("expected Cancel(currentLease) to reach svc2 after stale cleanup")
	}

	// svc2's invocation returns promptly after cancel.
	waitForWorkerIdle(t, svc2, 2*time.Second)

	// The stale svc1 result must not have persisted.
	final, err := svc1.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := final.Result.(map[string]any); ok && m["stale"] != nil {
		t.Fatalf("stale svc1 result should not have been persisted: %v", final.Result)
	}
}

// TestSchedulerInternalMutationSchedule verifies that internal mutations
// are accepted by the scheduler (Convex-compatible).
func TestSchedulerInternalMutationSchedule(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := svc.executor.(*testExecutor)
	exec.resolveVisibility = deploy.FunctionVisibilityInternal

	id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter internal mutation: %v", err)
	}

	status := waitForStatus(t, ctx, svc, id, JobStatusCompleted, 2*time.Second)
	if status.Status != JobStatusCompleted {
		t.Fatalf("expected completed, got %s", status.Status)
	}
	if exec.callCnt.Load() != 1 {
		t.Fatalf("expected 1 invocation, got %d", exec.callCnt.Load())
	}
}

// TestSchedulerInternalActionSchedule verifies that internal actions
// are accepted by the scheduler (Convex-compatible).
func TestSchedulerInternalActionSchedule(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := svc.executor.(*testExecutor)
	exec.resolveVisibility = deploy.FunctionVisibilityInternal
	exec.resolveFuncType = deploy.FunctionTypeAction

	id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatalf("RunAfter internal action: %v", err)
	}

	status := waitForStatus(t, ctx, svc, id, JobStatusCompleted, 2*time.Second)
	if status.Status != JobStatusCompleted {
		t.Fatalf("expected completed, got %s", status.Status)
	}
}

// TestSchedulerCancelPreventsInvocationAfterClaim verifies the claim-to-
// canceller race fix: if Cancel sets status=canceled between claimOne and
// runJob's canceller registration, the CountRecords re-check in runJob
// detects it and the invocation is skipped.
func TestSchedulerCancelPreventsInvocationAfterClaim(t *testing.T) {
	app, svc := newSchedulerTestApp(t)
	ctx := schema.WithApp(context.Background(), app)

	exec := svc.executor.(*testExecutor)

	// Schedule far-future so the worker does not claim it automatically.
	id, err := svc.RunAfter(ctx, 60000, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a claim: set status=running with a known lease.
	leaseToken := "claim-test-lease"
	_, err = app.DB().Update(
		schema.CollectionJobs,
		dbx.Params{
			schema.FieldStatus:         JobStatusRunning,
			schema.FieldLease:          leaseToken,
			schema.FieldLeaseExpiresAt: dateTime(time.Now().Add(time.Minute)),
			schema.FieldAttempts:       1,
		},
		dbx.NewExp("id = {:id} AND status = {:pending}", dbx.Params{"id": id, "pending": JobStatusPending}),
	).Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Build the stale record the worker would pass to runJob.
	jobRec, err := app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		t.Fatal(err)
	}

	// Cancel the job (status → canceled, lease → "").
	if err := svc.Cancel(ctx, id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// Now call runJob directly with the stale record. The CountRecords
	// check should find 0 (status=canceled, lease="") and return without
	// invoking.
	svc.worker.sem <- struct{}{}
	svc.worker.wg.Add(1)
	svc.worker.runJob(jobRec)
	waitForWorkerIdle(t, svc, 2*time.Second)

	if exec.callCnt.Load() != 0 {
		t.Fatalf("expected 0 invocations after cancel-before-invoke, got %d", exec.callCnt.Load())
	}

	final, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != JobStatusCanceled {
		t.Fatalf("expected canceled, got %s", final.Status)
	}
}

// TestSchedulerCancelAfterTheftUsesCurrentLease verifies that Cancel's
// lease-CAS update only cancels the current attempt. After a lease theft,
// the old lease token cannot cancel the job (0 rows affected).
func TestSchedulerCancelAfterTheftUsesCurrentLease(t *testing.T) {
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

	clock := NewFakeClock(time.Now())
	cfg := DefaultConfig()
	cfg.Clock = clock
	cfg.PollInterval = 10 * time.Hour
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.RenewInterval = 1 * time.Hour
	cfg.MaxExecutionDuration = 10 * time.Second
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	svc1Block := make(chan struct{})
	var svc1Started, svc2Started sync.WaitGroup
	svc1Started.Add(1)
	svc2Started.Add(1)

	exec := &testExecutor{resp: map[string]any{"done": true}}
	exec.invokeFn = func(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error) {
		switch exec.callCnt.Load() {
		case 1:
			svc1Started.Done()
			<-svc1Block
			return map[string]any{"stale": true}, nil
		case 2:
			svc2Started.Done()
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, nil
	}

	svc1 := NewService(app, exec, cfg)
	svc2 := NewService(app, exec, cfg)
	if err := svc1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc1.Stop()
	defer svc2.Stop()

	schedCtx := schema.WithApp(context.Background(), app)
	id, err := svc1.RunAfter(schedCtx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	waitGroupWait(t, &svc1Started, 2*time.Second)

	// Read the old lease.
	jobRec, err := app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		t.Fatal(err)
	}
	oldLease := jobRec.GetString(schema.FieldLease)

	// Steal the lease: expire it manually and wake svc2.
	expireLease(t, app, clock, id)
	for i := 0; i < 100; i++ {
		svc2.worker.Wake()
		time.Sleep(10 * time.Millisecond)
		if exec.callCnt.Load() >= 2 {
			break
		}
	}
	waitGroupWait(t, &svc2Started, 2*time.Second)

	// Read the new lease.
	jobRec, err = app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		t.Fatal(err)
	}
	newLease := jobRec.GetString(schema.FieldLease)
	if newLease == "" || newLease == oldLease {
		t.Fatalf("expected new lease after theft, got %q", newLease)
	}

	// A stale-lease cancel (simulating Cancel reading the old lease before
	// the reclaim) must fail the CAS and not cancel the job.
	now := clock.Now()
	res, err := app.DB().Update(
		schema.CollectionJobs,
		dbx.Params{
			schema.FieldStatus:         JobStatusCanceled,
			schema.FieldLease:          "",
			schema.FieldLeaseExpiresAt: types.DateTime{},
			schema.FieldFinished:       dateTime(now),
			schema.FieldUpdated:        dateTime(now),
		},
		dbx.NewExp("id = {:id} AND lease = {:lease} AND (status = 'pending' OR status = 'running')", dbx.Params{"id": id, "lease": oldLease}),
	).Execute()
	if err != nil {
		t.Fatal(err)
	}
	affected, _ := res.RowsAffected()
	if affected != 0 {
		t.Fatal("stale-lease cancel should fail CAS after theft")
	}

	// Cancel with the current lease (via Service.Cancel which reads fresh)
	// should succeed and target svc2's attempt.
	if err := svc2.Cancel(schedCtx, id); err != nil {
		t.Fatalf("Cancel with current lease: %v", err)
	}

	// svc2's invocation should return (canceled).
	waitForWorkerIdle(t, svc2, 2*time.Second)

	// Release svc1 — its stale completion is ignored.
	close(svc1Block)
	waitForWorkerIdle(t, svc1, 2*time.Second)

	final, err := svc1.Get(schedCtx, id)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != JobStatusCanceled {
		t.Fatalf("expected canceled, got %s", final.Status)
	}
}

// TestSchedulerRenewIntervalNormalized verifies that RenewInterval >=
// LeaseDuration is clamped to a safe margin, and that tiny LeaseDuration
// values produce RenewInterval = 0 (disabled) rather than violating the
// strict invariant RenewInterval < LeaseDuration.
func TestSchedulerRenewIntervalNormalized(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.RenewInterval = 200 * time.Millisecond
	svc := NewService(nil, &testExecutor{}, cfg)
	if svc.config.RenewInterval >= svc.config.LeaseDuration {
		t.Fatalf("RenewInterval %v should be < LeaseDuration %v", svc.config.RenewInterval, svc.config.LeaseDuration)
	}

	cfg2 := DefaultConfig()
	cfg2.LeaseDuration = 100 * time.Millisecond
	cfg2.RenewInterval = 100 * time.Millisecond // equal
	svc2 := NewService(nil, &testExecutor{}, cfg2)
	if svc2.config.RenewInterval >= svc2.config.LeaseDuration {
		t.Fatalf("RenewInterval %v should be < LeaseDuration %v after normalization", svc2.config.RenewInterval, svc2.config.LeaseDuration)
	}

	// Tiny LeaseDuration: half is 0, so RenewInterval must be 0 (disabled)
	// rather than 1ms which would violate the invariant.
	cfg3 := DefaultConfig()
	cfg3.LeaseDuration = 1 * time.Nanosecond
	cfg3.RenewInterval = 1 * time.Millisecond
	svc3 := NewService(nil, &testExecutor{}, cfg3)
	if svc3.config.RenewInterval >= svc3.config.LeaseDuration {
		t.Fatalf("RenewInterval %v should be < LeaseDuration %v (strict invariant)", svc3.config.RenewInterval, svc3.config.LeaseDuration)
	}
	if svc3.config.RenewInterval != 0 {
		t.Fatalf("expected RenewInterval=0 for tiny LeaseDuration, got %v", svc3.config.RenewInterval)
	}
}

// TestSchedulerCancelRetriesAfterReclaim verifies that Cancel succeeds
// after a lease theft. The retry loop handles the case where the lease
// changes between read and CAS update. Since svc2 owns the current
// attempt, svc2.Cancel can signal its worker.
func TestSchedulerCancelRetriesAfterReclaim(t *testing.T) {
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

	clock := NewFakeClock(time.Now())
	cfg := DefaultConfig()
	cfg.Clock = clock
	cfg.PollInterval = 10 * time.Hour
	cfg.LeaseDuration = 100 * time.Millisecond
	cfg.RenewInterval = 1 * time.Hour
	cfg.MaxExecutionDuration = 10 * time.Second
	cfg.Jitter = func(time.Duration) time.Duration { return 0 }

	svc1Block := make(chan struct{})
	var svc1Started, svc2Started sync.WaitGroup
	svc1Started.Add(1)
	svc2Started.Add(1)

	exec := &testExecutor{resp: map[string]any{"done": true}}
	exec.invokeFn = func(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error) {
		switch exec.callCnt.Load() {
		case 1:
			svc1Started.Done()
			<-svc1Block
			return map[string]any{"stale": true}, nil
		case 2:
			svc2Started.Done()
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, nil
	}

	svc1 := NewService(app, exec, cfg)
	svc2 := NewService(app, exec, cfg)
	if err := svc1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer svc1.Stop()
	defer svc2.Stop()

	ctx := schema.WithApp(context.Background(), app)
	id, err := svc1.RunAfter(ctx, 0, "d1", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	waitGroupWait(t, &svc1Started, 2*time.Second)

	// Steal the lease.
	expireLease(t, app, clock, id)
	for i := 0; i < 100; i++ {
		svc2.worker.Wake()
		time.Sleep(10 * time.Millisecond)
		if exec.callCnt.Load() >= 2 {
			break
		}
	}
	waitGroupWait(t, &svc2Started, 2*time.Second)

	// svc2.Cancel reads the current lease (svc2's) and succeeds.
	// The retry loop handles any CAS miss from concurrent reclaims.
	if err := svc2.Cancel(ctx, id); err != nil {
		t.Fatalf("Cancel should succeed after reclaim: %v", err)
	}

	waitForWorkerIdle(t, svc2, 2*time.Second)
	close(svc1Block)
	waitForWorkerIdle(t, svc1, 2*time.Second)

	final, err := svc1.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != JobStatusCanceled {
		t.Fatalf("expected canceled, got %s", final.Status)
	}
}
