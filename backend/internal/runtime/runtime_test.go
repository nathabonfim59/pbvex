package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

const testBundle = `__pbvex.registerFunction({name:"block",type:"query",visibility:"public",modulePath:"block",exportName:"default"}, function(ctx,args) { var end = Date.now() + 60000; while (Date.now() < end) {} return "done"; });`

func TestManagerInvokeCancelsWithContext(t *testing.T) {
	m := NewManager(Config{PoolSize: 2, Timeout: 30 * time.Second})

	descriptors := []deploy.FunctionDescriptor{
		{
			Name:       "block",
			Type:       deploy.FunctionTypeQuery,
			Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "block",
			ExportName: "default",
		},
	}

	if err := m.Compile("d1", testBundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := m.Invoke(ctx, "d1", "block", map[string]any{})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancellation took too long: %v", time.Since(start))
	}
}

func TestManagerInvokeTimesOut(t *testing.T) {
	m := NewManager(Config{PoolSize: 2, Timeout: 200 * time.Millisecond})

	descriptors := []deploy.FunctionDescriptor{
		{
			Name:       "block",
			Type:       deploy.FunctionTypeQuery,
			Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "block",
			ExportName: "default",
		},
	}

	if err := m.Compile("d1", testBundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	start := time.Now()
	_, err := m.Invoke(context.Background(), "d1", "block", map[string]any{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("timeout took too long: %v", time.Since(start))
	}
}

func TestManagerShorterManagerTimeoutOverridesParentDeadline(t *testing.T) {
	m := NewManager(Config{PoolSize: 2, Timeout: 200 * time.Millisecond})

	descriptors := []deploy.FunctionDescriptor{
		{
			Name:       "block",
			Type:       deploy.FunctionTypeQuery,
			Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "block",
			ExportName: "default",
		},
	}

	if err := m.Compile("d1", testBundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	// Parent deadline is much longer than manager timeout.
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	_, err := m.Invoke(parent, "d1", "block", map[string]any{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > 1*time.Second {
		t.Fatalf("manager timeout did not override parent deadline: %v", time.Since(start))
	}
}

func TestManagerInvokeFastFunction(t *testing.T) {
	fastBundle := `__pbvex.registerFunction({name:"hello",type:"query",visibility:"public",modulePath:"hello",exportName:"default"}, function(ctx,args) { return "Hello, " + args.name + "!"; });`
	m := NewManager(Config{PoolSize: 2, Timeout: 2 * time.Second})

	descriptors := []deploy.FunctionDescriptor{
		{
			Name:       "hello",
			Type:       deploy.FunctionTypeQuery,
			Visibility: deploy.FunctionVisibilityPublic,
			ModulePath: "hello",
			ExportName: "default",
		},
	}

	if err := m.Compile("d1", fastBundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	result, err := m.Invoke(context.Background(), "d1", "hello", map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if fmt.Sprint(result) != "Hello, world!" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestManagerInvokeDoesNotShareModuleState(t *testing.T) {
	bundle := `let calls = 0;
__pbvex.registerFunction({name:"count",type:"query",visibility:"public",modulePath:"count",exportName:"default"}, function(ctx,args) { calls += 1; return calls; });`
	descriptors := []deploy.FunctionDescriptor{{
		Name:       "count",
		Type:       deploy.FunctionTypeQuery,
		Visibility: deploy.FunctionVisibilityPublic,
		ModulePath: "count",
		ExportName: "default",
	}}
	m := NewManager(Config{PoolSize: 1, Timeout: 2 * time.Second})
	if err := m.Compile("d1", bundle, descriptors); err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	for i := 0; i < 2; i++ {
		got, err := m.Invoke(context.Background(), "d1", "count", map[string]any{})
		if err != nil {
			t.Fatalf("invoke %d failed: %v", i+1, err)
		}
		if fmt.Sprint(got) != "1" {
			t.Fatalf("invoke %d observed state from a previous request: got %v, want 1", i+1, got)
		}
	}
}
