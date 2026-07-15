package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/core"
)

type mockScheduler struct {
	mu            sync.Mutex
	runAfterCalls []mockRunAfterCall
}

func TestContextExtendersComposeInOrderForActionsWithoutErasingScheduler(t *testing.T) {
	desc := deploy.FunctionDescriptor{
		Name: "compose", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic,
		ModulePath: "caps", ExportName: "compose",
	}
	bundle := `__pbvex.registerFunction({name:"compose",type:"action",visibility:"public",modulePath:"caps",exportName:"compose"}, async function(ctx) {
  return {first:ctx.first, second:ctx.second, scheduler:typeof ctx.scheduler.runAfter === "function"};
});`
	m := NewManager(DefaultConfig())
	m.Scheduler = &mockScheduler{}
	m.AddContextExtender(func(_ *goja.Runtime, _ context.Context, _ core.App, _ deploy.FunctionDescriptor, obj *goja.Object) error {
		return obj.Set("first", "one")
	})
	m.AddContextExtender(func(_ *goja.Runtime, _ context.Context, _ core.App, _ deploy.FunctionDescriptor, obj *goja.Object) error {
		if obj.Get("first").String() != "one" {
			return errors.New("first extender capability missing")
		}
		return obj.Set("second", "two")
	})
	if err := m.Compile("dep", bundle, []deploy.FunctionDescriptor{desc}); err != nil {
		t.Fatal(err)
	}
	result, err := m.Invoke(context.Background(), "dep", "compose", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(map[string]any)
	if !ok || got["first"] != "one" || got["second"] != "two" || got["scheduler"] != true {
		t.Fatalf("composed action context: %#v", result)
	}
}

type mockRunAfterCall struct {
	DelayMs      int64
	DeploymentID string
	FunctionName string
	Args         any
}

func (m *mockScheduler) RunAfter(ctx context.Context, delayMs int64, deploymentID, functionName string, args any) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runAfterCalls = append(m.runAfterCalls, mockRunAfterCall{delayMs, deploymentID, functionName, args})
	return "job-1", nil
}

func (m *mockScheduler) RunAt(ctx context.Context, epochMs int64, deploymentID, functionName string, args any) (string, error) {
	return "job-1", nil
}

func (m *mockScheduler) Cancel(ctx context.Context, jobID string) error {
	return nil
}

func (m *mockScheduler) runAfterCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runAfterCalls)
}

func TestRuntimeSchedulerRunAfterAcceptsInternalMutation(t *testing.T) {
	internalDesc := deploy.FunctionDescriptor{
		Name: "internalTask", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityInternal,
		ModulePath: "pbvex/internalTask.ts", ExportName: "run",
	}
	schedulerDesc := deploy.FunctionDescriptor{
		Name: "scheduler", Type: deploy.FunctionTypeMutation, Visibility: deploy.FunctionVisibilityPublic,
		ModulePath: "pbvex/scheduler.ts", ExportName: "schedule",
	}
	bundle := `(function(){
__pbvex.registerFunction({name:"scheduler",type:"mutation",visibility:"public",modulePath:"pbvex/scheduler.ts",exportName:"schedule"}, async function(ctx,args) {
  ctx.scheduler.runAfter(0, {_path:"internalTask", _type:"mutation", _visibility:"internal"}, {foo:"bar"});
  return {ok:true};
});
__pbvex.registerFunction({name:"internalTask",type:"mutation",visibility:"internal",modulePath:"pbvex/internalTask.ts",exportName:"run"}, async function(ctx,args) { return {ok:true}; });
})();`

	m := NewManager(DefaultConfig())
	sched := &mockScheduler{}
	m.Scheduler = sched
	if err := m.Compile("dep", bundle, []deploy.FunctionDescriptor{schedulerDesc, internalDesc}); err != nil {
		t.Fatal(err)
	}

	result, err := m.Invoke(context.Background(), "dep", "scheduler", map[string]any{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if sched.runAfterCount() != 1 {
		t.Fatalf("expected 1 runAfter call, got %d", sched.runAfterCount())
	}
	call := sched.runAfterCalls[0]
	if call.FunctionName != "internalTask" {
		t.Fatalf("expected function name internalTask, got %q", call.FunctionName)
	}
	if call.DeploymentID != "dep" {
		t.Fatalf("expected deployment ID dep, got %q", call.DeploymentID)
	}
}

func TestRuntimeSchedulerRunAfterAcceptsInternalAction(t *testing.T) {
	internalDesc := deploy.FunctionDescriptor{
		Name: "internalAction", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityInternal,
		ModulePath: "pbvex/internalAction.ts", ExportName: "run",
	}
	schedulerDesc := deploy.FunctionDescriptor{
		Name: "scheduler", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic,
		ModulePath: "pbvex/scheduler.ts", ExportName: "schedule",
	}
	bundle := `(function(){
__pbvex.registerFunction({name:"scheduler",type:"action",visibility:"public",modulePath:"pbvex/scheduler.ts",exportName:"schedule"}, async function(ctx,args) {
  ctx.scheduler.runAfter(100, {_path:"internalAction", _type:"action", _visibility:"internal"});
  return {ok:true};
});
__pbvex.registerFunction({name:"internalAction",type:"action",visibility:"internal",modulePath:"pbvex/internalAction.ts",exportName:"run"}, async function(ctx,args) { return {ok:true}; });
})();`

	m := NewManager(DefaultConfig())
	sched := &mockScheduler{}
	m.Scheduler = sched
	if err := m.Compile("dep", bundle, []deploy.FunctionDescriptor{schedulerDesc, internalDesc}); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Invoke(context.Background(), "dep", "scheduler", map[string]any{}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if sched.runAfterCount() != 1 {
		t.Fatalf("expected 1 runAfter call, got %d", sched.runAfterCount())
	}
	if sched.runAfterCalls[0].FunctionName != "internalAction" {
		t.Fatalf("expected function name internalAction, got %q", sched.runAfterCalls[0].FunctionName)
	}
}

// TestRuntimeInvokeChecksCtxErrBeforeInvoke verifies that pool.invoke
// checks ctx.Err() after acquiring the semaphore and before invoking the
// Goja function. A context that is already canceled must not produce
// side effects.
func TestRuntimeInvokeChecksCtxErrBeforeInvoke(t *testing.T) {
	desc := deploy.FunctionDescriptor{
		Name: "sideeffect", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic,
		ModulePath: "x.js", ExportName: "sideeffect",
	}
	bundle := `(function(){__pbvex.registerFunction({name:"sideeffect",type:"action",visibility:"public",modulePath:"x.js",exportName:"sideeffect"}, async function(ctx,args) { return {ran:true}; }); })();`

	m := NewManager(Config{PoolSize: 1, Timeout: 5 * time.Second})
	if err := m.Compile("dep", bundle, []deploy.FunctionDescriptor{desc}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := m.Invoke(ctx, "dep", "sideeffect", nil)
	if err == nil {
		t.Fatalf("expected error from canceled ctx, got result %#v", result)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestRuntimeInvokeInterruptsOnContextCancel verifies that the early-
// cancellation watcher interrupts a running Goja VM when ctx is canceled
// mid-invocation, not just at the deadline.
func TestRuntimeInvokeInterruptsOnContextCancel(t *testing.T) {
	desc := deploy.FunctionDescriptor{
		Name: "loop", Type: deploy.FunctionTypeAction, Visibility: deploy.FunctionVisibilityPublic,
		ModulePath: "x.js", ExportName: "loop",
	}
	bundle := `(function(){__pbvex.registerFunction({name:"loop",type:"action",visibility:"public",modulePath:"x.js",exportName:"loop"}, async function(ctx,args) { while(true){} }); })();`

	m := NewManager(Config{PoolSize: 1, Timeout: 30 * time.Second})
	if err := m.Compile("dep", bundle, []deploy.FunctionDescriptor{desc}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := m.Invoke(ctx, "dep", "loop", nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error from interrupted VM")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("VM was not interrupted promptly, took %v", elapsed)
	}
}
