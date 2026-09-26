package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

type executionRecorder struct {
	mu     sync.Mutex
	begin  func(context.Context, ExecutionInfo) (context.Context, error)
	starts []ExecutionInfo
	ends   map[string][]ExecutionResult
}

func (r *executionRecorder) Begin(ctx context.Context, info ExecutionInfo) (context.Context, error) {
	r.mu.Lock()
	r.starts = append(r.starts, info)
	r.mu.Unlock()
	if r.begin != nil {
		return r.begin(ctx, info)
	}
	return ctx, nil
}

func (r *executionRecorder) End(_ context.Context, info ExecutionInfo, result ExecutionResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ends == nil {
		r.ends = make(map[string][]ExecutionResult)
	}
	r.ends[info.ID] = append(r.ends[info.ID], result)
}

func executionBundle(descriptors []deploy.FunctionDescriptor, handlers ...string) string {
	var bundle strings.Builder
	for i, descriptor := range descriptors {
		raw, _ := json.Marshal(descriptor)
		fmt.Fprintf(&bundle, "__pbvex.registerFunction(%s,%s);\n", raw, handlers[i])
	}
	return bundle.String()
}

func TestExecutionAdmissionGatesLoadAndHandler(t *testing.T) {
	denied := errors.New("provider secret diagnostic")
	for _, origin := range []string{"bundle_load", "call"} {
		t.Run(origin, func(t *testing.T) {
			r := &executionRecorder{begin: func(ctx context.Context, info ExecutionInfo) (context.Context, error) {
				if info.Origin == origin {
					return nil, denied
				}
				return ctx, nil
			}}
			m := NewManager(Config{ExecutionObserver: r, Timeout: time.Second})
			var ran atomic.Int32
			m.AddContextExtender(func(vm *goja.Runtime, _ context.Context, _ core.App, _ deploy.FunctionDescriptor, obj *goja.Object) error {
				return obj.Set("mark", func() { ran.Add(1) })
			})
			descriptors := []deploy.FunctionDescriptor{queryDescriptor("hello")}
			bundle := executionBundle(descriptors, `function(ctx){ctx.mark();return null;}`)
			if origin == "bundle_load" {
				bundle = `while(true){}` + bundle
			}
			if err := m.Compile("dep", bundle, descriptors); err != nil {
				t.Fatal(err)
			}
			_, err := m.Invoke(context.Background(), "dep", "hello", nil)
			if !errors.Is(err, denied) || !deploy.IsExecutionAdmissionError(err) {
				t.Fatalf("lost denial: %v", err)
			}
			if strings.Contains(err.Error(), "secret") || ran.Load() != 0 {
				t.Fatalf("denial leaked or ran: %v / %d", err, ran.Load())
			}
			for _, start := range r.starts {
				want := 1
				if start.Origin == origin {
					want = 0
				}
				if len(r.ends[start.ID]) != want {
					t.Fatalf("end count for %s: %v", start.Origin, r.ends)
				}
			}
		})
	}
}

func TestExecutionLifecycleOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, handler string
		timeout       bool
		wantErr       bool
	}{
		{"success", `async function(){return 42;}`, false, false},
		{"throw", `function(){throw new Error("boom");}`, false, true},
		{"rejection", `async function(){throw new Error("boom");}`, false, true},
		{"timeout", `function(){while(true){}}`, true, true},
		{"pending", `function(){return new Promise(function(){});}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &executionRecorder{}
			m := NewManager(Config{ExecutionObserver: r, Timeout: 80 * time.Millisecond})
			descriptors := []deploy.FunctionDescriptor{queryDescriptor("hello")}
			if err := m.Compile("dep", executionBundle(descriptors, tc.handler), descriptors); err != nil {
				t.Fatal(err)
			}
			_, err := m.Invoke(context.Background(), "dep", "hello", nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("result: %v", err)
			}
			if tc.timeout && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("not a timeout: %v", err)
			}
			if len(r.starts) != 2 {
				t.Fatalf("expected load and handler: %#v", r.starts)
			}
			for _, start := range r.starts {
				ends := r.ends[start.ID]
				if len(ends) != 1 || ends[0].Duration < 0 {
					t.Fatalf("invalid completion: %#v", ends)
				}
				if start.Origin == "call" && ((ends[0].Err != nil) != tc.wantErr || (tc.timeout && !errors.Is(ends[0].Err, context.DeadlineExceeded))) {
					t.Fatalf("wrong outcome: %#v", ends)
				}
			}
		})
	}
}

func TestExecutionNestedIdentityAuthAndSingleSlot(t *testing.T) {
	r := &executionRecorder{}
	m := NewManager(Config{PoolSize: 1, MaxConcurrentExecutions: 1, ExecutionObserver: r, Timeout: time.Second})
	identity := testIdentity()
	type reservationKey struct{}
	r.begin = func(ctx context.Context, info ExecutionInfo) (context.Context, error) {
		ac, ok := AuthFromContext(ctx)
		if !ok || ac.Identity != identity || ac.RequestID != "caller-request" {
			t.Error("caller auth metadata was replaced")
		}
		return context.WithValue(ctx, reservationKey{}, info.ID), nil
	}
	m.AddContextExtender(func(vm *goja.Runtime, ctx context.Context, _ core.App, _ deploy.FunctionDescriptor, obj *goja.Object) error {
		info, ok := ExecutionFromContext(ctx)
		if !ok || ctx.Value(reservationKey{}) != info.ID {
			t.Error("reservation context did not reach handler")
		}
		return nil
	})
	descriptors := []deploy.FunctionDescriptor{actionDescriptor("outer"), actionDescriptor("middle"), queryDescriptor("read"), mutationDescriptor("write")}
	bundle := executionBundle(descriptors,
		`async function(ctx){await ctx.runAction("middle",null);return ctx.runMutation("write",null);}`,
		`async function(ctx){return ctx.runQuery("read",null);}`,
		`async function(ctx){return (await ctx.auth.getUserIdentity()).subject;}`,
		`function(){return "written";}`)
	if err := m.Compile("dep", bundle, descriptors); err != nil {
		t.Fatal(err)
	}
	// A real app exercises the nested mutation transaction with the observer's
	// child context cleanup; a cleanup cancellation must not roll back success.
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()
	result, err := m.InvokeWithDatabase(context.Background(), "dep", "outer", nil, identity, "caller-request", app, deploy.DeploymentManifest{})
	if err != nil || result != "written" {
		t.Fatalf("nested transaction: %v / %v", result, err)
	}
	byName := map[string]ExecutionInfo{}
	ids := map[string]bool{}
	for _, info := range r.starts {
		if info.ID == "" || info.ID == "caller-request" || ids[info.ID] {
			t.Fatalf("invalid identity: %#v", info)
		}
		ids[info.ID] = true
		if len(r.ends[info.ID]) != 1 || r.ends[info.ID][0].Err != nil {
			t.Fatalf("unsettled execution: %#v", info)
		}
		if info.Origin == "call" {
			byName[info.FunctionName] = info
		}
	}
	root := byName["outer"]
	if root.RootID != root.ID || root.ParentID != "" || root.Depth != 0 {
		t.Fatalf("bad root: %#v", root)
	}
	for name, parentName := range map[string]string{"middle": "outer", "read": "middle", "write": "outer"} {
		child, parent := byName[name], byName[parentName]
		if child.RootID != root.ID || child.ParentID != parent.ID || child.Depth != parent.Depth+1 || child.Namespace != deploy.RootNamespace {
			t.Fatalf("bad child: %#v", child)
		}
	}
}

func TestExecutionNestedDenialPreservesErrorWithoutExposingProvider(t *testing.T) {
	secret := errors.New("private credential")
	for _, caught := range []bool{false, true} {
		r := &executionRecorder{begin: func(ctx context.Context, info ExecutionInfo) (context.Context, error) {
			if info.FunctionName == "inner" {
				return nil, secret
			}
			return ctx, nil
		}}
		m := NewManager(Config{ExecutionObserver: r})
		ds := []deploy.FunctionDescriptor{actionDescriptor("outer"), queryDescriptor("inner")}
		handler := `async function(ctx){return ctx.runQuery("inner",null);}`
		if caught {
			handler = `async function(ctx){try{await ctx.runQuery("inner",null);}catch(e){return String(e)+JSON.stringify(e.value);}}`
		}
		if err := m.Compile("dep", executionBundle(ds, handler, `function(){throw new Error("must not run");}`), ds); err != nil {
			t.Fatal(err)
		}
		result, err := m.Invoke(context.Background(), "dep", "outer", nil)
		if caught {
			if err != nil || strings.Contains(fmt.Sprint(result), "credential") {
				t.Fatalf("provider leaked: %v / %v", result, err)
			}
		} else {
			var admission *deploy.ExecutionAdmissionError
			if !errors.Is(err, secret) || !errors.As(err, &admission) || !admission.Started {
				t.Fatalf("lost nested denial: %v", err)
			}
		}
		for _, start := range r.starts {
			if start.FunctionName == "inner" && len(r.ends[start.ID]) != 0 {
				t.Fatal("denied child completed")
			}
		}
	}
}

func TestExecutionConcurrencyAcrossPoolsAndCanceledContexts(t *testing.T) {
	r := &executionRecorder{}
	m := NewManager(Config{PoolSize: 4, MaxConcurrentExecutions: 1, ExecutionObserver: r, Timeout: time.Second})
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.AddContextExtender(func(vm *goja.Runtime, _ context.Context, _ core.App, _ deploy.FunctionDescriptor, obj *goja.Object) error {
		return obj.Set("wait", func() { once.Do(func() { close(entered) }); <-release })
	})
	ds := []deploy.FunctionDescriptor{queryDescriptor("hello")}
	for _, id := range []string{"a", "b"} {
		if err := m.Compile(id, executionBundle(ds, `function(ctx){ctx.wait();return null;}`), ds); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() { _, err := m.Invoke(context.Background(), "a", "hello", nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("handler did not enter")
	}
	_, err := m.Invoke(context.Background(), "b", "hello", nil)
	if !errors.Is(err, deploy.ErrExecutionBusy) {
		t.Errorf("other deployment bypassed cap: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = m.Invoke(ctx, "b", "hello", nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled call admitted: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := m.Invoke(context.Background(), "b", "hello", nil); err != nil {
		t.Fatalf("slot leaked: %v", err)
	}
	if len(r.starts) != 4 {
		t.Fatalf("rejected roots generated executions: %#v", r.starts)
	}
}

func TestExecutionObserverDeadlines(t *testing.T) {
	for _, shorten := range []bool{false, true} {
		r := &executionRecorder{}
		var cancels []context.CancelFunc
		r.begin = func(ctx context.Context, info ExecutionInfo) (context.Context, error) {
			if info.Origin == "call" && shorten {
				next, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
				cancels = append(cancels, cancel)
				return next, nil
			}
			return ctx, nil
		}
		m := NewManager(Config{ExecutionObserver: r, Timeout: time.Second})
		ds := []deploy.FunctionDescriptor{queryDescriptor("hello")}
		if err := m.Compile("dep", executionBundle(ds, `function(){while(true){}}`), ds); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		start := time.Now()
		_, err := m.Invoke(ctx, "dep", "hello", nil)
		cancel()
		for _, cancel := range cancels {
			cancel()
		}
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 500*time.Millisecond {
			t.Fatalf("deadline lost: %v", err)
		}
		for _, info := range r.starts {
			if len(r.ends[info.ID]) != 1 {
				t.Fatal("deadline completion missing")
			}
		}
	}
}

func TestExecutionHTTPOriginAndInvalidArgsPreflight(t *testing.T) {
	r := &executionRecorder{}
	m := NewManager(Config{ExecutionObserver: r})
	ds := []deploy.FunctionDescriptor{httpActionDescriptor("http"), queryDescriptor("query")}
	ds[1].Args = map[string]any{"type": "string"}
	if err := m.Compile("dep", executionBundle(ds, `function(){return new Response("ok");}`, `function(){return null;}`), ds); err != nil {
		t.Fatal(err)
	}
	response, err := m.InvokeHTTP(context.Background(), "dep", "http", &deploy.HTTPRequestEnvelope{Method: "POST", URL: "https://example.test/echo"}, nil, "request")
	if err != nil || response.Status != 200 {
		t.Fatalf("HTTP: %v / %v", response, err)
	}
	_, err = m.Invoke(context.Background(), "dep", "query", float64(123))
	if err == nil {
		t.Fatal("invalid arguments accepted")
	}
	var sawHTTP bool
	for _, info := range r.starts {
		if info.FunctionName == "query" {
			t.Fatal("handler admission ran before validation")
		}
		if info.FunctionName == "http" {
			sawHTTP = info.Origin == "http_action" && info.FunctionType == deploy.FunctionTypeHTTPAction
		}
	}
	if !sawHTTP {
		t.Fatal("HTTP lifecycle missing")
	}
}

func TestExecutionMigrationAndVerificationAdmission(t *testing.T) {
	from := map[string]any{"type": "object", "shape": map[string]any{"name": map[string]any{"type": "string"}}}
	to := map[string]any{"type": "object", "shape": map[string]any{"name": map[string]any{"type": "string"}, "active": map[string]any{"type": "boolean"}}}
	fromHash, _ := deploy.CanonicalHash(from)
	toHash, _ := deploy.CanonicalHash(to)
	descriptor := deploy.MigrationDescriptor{ID: "add_active", Table: "users", Mode: "transactional", From: from, To: to, SourceSchemaHash: fromHash, TargetSchemaHash: toHash, Checksum: strings.Repeat("a", 64), ModulePath: "pbvex/migrations/add.ts", ExportName: "default", Reversibility: "reversible"}
	raw, _ := json.Marshal(descriptor)
	bundle := `__pbvex.registerMigration(` + string(raw) + `, function(doc){return {name:doc.name,active:true};},function(){throw new Error("failed down");});`
	denied := errors.New("migration denied")
	denyOrigin := "bundle_load"
	r := &executionRecorder{begin: func(ctx context.Context, info ExecutionInfo) (context.Context, error) {
		if info.Origin == denyOrigin {
			return nil, denied
		}
		return ctx, nil
	}}
	m := NewManager(Config{ExecutionObserver: r, MaxConcurrentExecutions: 1})
	if err := m.VerifyDeployment(context.Background(), "dep", bundle, nil, []deploy.MigrationDescriptor{descriptor}); !errors.Is(err, denied) {
		t.Fatalf("verification lost denial: %v", err)
	}
	denyOrigin = ""
	if err := m.VerifyDeployment(context.Background(), "dep", bundle, nil, []deploy.MigrationDescriptor{descriptor}); err != nil {
		t.Fatal(err)
	}
	if err := m.CompileDeployment("dep", bundle, nil, []deploy.MigrationDescriptor{descriptor}); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"up", "down"} {
		_, err := m.InvokeMigration(context.Background(), "dep", descriptor.ID, direction, map[string]any{"name": "Ada"}, 1)
		if (err != nil) != (direction == "down") {
			t.Fatalf("migration %s: %v", direction, err)
		}
	}
	denyOrigin = "migration"
	if _, err := m.InvokeMigration(context.Background(), "dep", descriptor.ID, "up", map[string]any{"name": "Ada"}, 1); !errors.Is(err, denied) {
		t.Fatalf("migration lost denial: %v", err)
	}
	var migrationStarts int
	for _, info := range r.starts {
		if info.Origin != "migration" {
			continue
		}
		migrationStarts++
		ends := r.ends[info.ID]
		if migrationStarts == 3 {
			if len(ends) != 0 {
				t.Fatal("denied migration ended")
			}
			continue
		}
		if info.FunctionType != "" || len(ends) != 1 || (ends[0].Err != nil) != (info.FunctionName == "add_active:down") {
			t.Fatalf("invalid migration telemetry: %#v / %#v", info, ends)
		}
	}
	if migrationStarts != 3 {
		t.Fatalf("migration boundaries missing: %#v", r.starts)
	}
}

func TestExecutionCanceledDuringAdmissionSettlesOnce(t *testing.T) {
	for _, beginFails := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		denied := errors.New("observer canceled")
		r := &executionRecorder{begin: func(ctx context.Context, info ExecutionInfo) (context.Context, error) {
			if info.Origin == "call" {
				cancel()
				if beginFails {
					return nil, denied
				}
			}
			return ctx, nil
		}}
		m := NewManager(Config{ExecutionObserver: r})
		ds := []deploy.FunctionDescriptor{queryDescriptor("hello")}
		if err := m.Compile("dep", executionBundle(ds, `function(){throw new Error("should never enter");}`), ds); err != nil {
			t.Fatal(err)
		}
		_, err := m.Invoke(ctx, "dep", "hello", nil)
		cancel()
		wantErr := context.Canceled
		if beginFails {
			wantErr = denied
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("cancellation swallowed admission: %v", err)
		}
		for _, info := range r.starts {
			want := 1
			if info.Origin == "call" && beginFails {
				want = 0
			}
			if len(r.ends[info.ID]) != want {
				t.Fatalf("bad cancellation completion: %#v", r.ends)
			}
		}
	}
}

func TestExecutionCanceledPoolWaitReleasesRootSlot(t *testing.T) {
	r := &executionRecorder{}
	m := NewManager(Config{ExecutionObserver: r, PoolSize: 1, MaxConcurrentExecutions: 2, Timeout: time.Second})
	entered, release := make(chan struct{}), make(chan struct{})
	m.AddContextExtender(func(vm *goja.Runtime, ctx context.Context, _ core.App, _ deploy.FunctionDescriptor, obj *goja.Object) error {
		info, _ := ExecutionFromContext(ctx)
		return obj.Set("wait", func() {
			if info.DeploymentID == "a" {
				close(entered)
				<-release
			}
		})
	})
	ds := []deploy.FunctionDescriptor{queryDescriptor("hello")}
	for _, id := range []string{"a", "b"} {
		if err := m.Compile(id, executionBundle(ds, `function(ctx){ctx.wait();return null;}`), ds); err != nil {
			t.Fatal(err)
		}
	}
	first := make(chan error, 1)
	go func() { _, err := m.Invoke(context.Background(), "a", "hello", nil); first <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("first execution did not start")
	}
	defer func() {
		close(release)
		if err := <-first; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan error, 1)
	go func() { _, err := m.Invoke(ctx, "a", "hello", nil); waiting <- err }()
	deadline := time.Now().Add(time.Second)
	for len(m.executionSlots) != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(m.executionSlots) != 2 {
		t.Fatal("second root did not reach pool wait")
	}
	cancel()
	select {
	case err := <-waiting:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled pool wait blocked")
	}
	if _, err := m.Invoke(context.Background(), "b", "hello", nil); err != nil {
		t.Fatalf("canceled wait leaked root slot: %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.starts) != 4 {
		t.Fatalf("waiting root ran user code: %#v", r.starts)
	}
}

func TestExecutionHostPanicIsNotReportedAsSuccess(t *testing.T) {
	r := &executionRecorder{}
	m := NewManager(Config{ExecutionObserver: r, MaxConcurrentExecutions: 1})
	panicValue := errors.New("host private panic")
	m.AddContextExtender(func(_ *goja.Runtime, _ context.Context, _ core.App, _ deploy.FunctionDescriptor, _ *goja.Object) error {
		panic(panicValue)
	})
	ds := []deploy.FunctionDescriptor{queryDescriptor("hello")}
	if err := m.Compile("dep", executionBundle(ds, `function(){return null;}`), ds); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != panicValue {
				t.Fatalf("panic changed: %v", recovered)
			}
		}()
		_, _ = m.Invoke(context.Background(), "dep", "hello", nil)
		t.Fatal("host panic swallowed")
	}()
	if len(m.executionSlots) != 0 {
		t.Fatal("panic leaked root slot")
	}
	for _, info := range r.starts {
		if info.Origin != "call" {
			continue
		}
		ends := r.ends[info.ID]
		if len(ends) != 1 || ends[0].Err == nil || strings.Contains(ends[0].Err.Error(), "private") {
			t.Fatalf("panic outcome: %#v", ends)
		}
	}
}
