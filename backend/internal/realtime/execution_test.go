package realtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
)

type realtimeExecutionObserver struct {
	queries   []runtime.ExecutionInfo
	completed int
}

func (o *realtimeExecutionObserver) Begin(ctx context.Context, info runtime.ExecutionInfo) (context.Context, error) {
	if info.FunctionType == deploy.FunctionTypeQuery {
		o.queries = append(o.queries, info)
		if len(o.queries) == 3 {
			return nil, errors.New("provider private diagnostic")
		}
	}
	return ctx, nil
}

func (o *realtimeExecutionObserver) End(_ context.Context, info runtime.ExecutionInfo, _ runtime.ExecutionResult) {
	if info.FunctionType == deploy.FunctionTypeQuery {
		o.completed++
	}
}

func TestRealtimeExecutionOriginAndAdmissionPause(t *testing.T) {
	observer := &realtimeExecutionObserver{}
	manager := runtime.NewManager(runtime.Config{ExecutionObserver: observer})
	service := deploy.NewService(nil, nil, manager, deploy.DefaultConfig())
	descriptor := deploy.FunctionDescriptor{Name: "hello", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x", ExportName: "hello"}
	snapshot := &deploy.CallSnapshot{
		DeploymentID: "dep", Descriptor: &descriptor, Functions: []deploy.FunctionDescriptor{descriptor}, Config: deploy.DefaultDeploymentConfig,
		BundleJS: `__pbvex.registerFunction({name:"hello",type:"query",visibility:"public",modulePath:"x",exportName:"hello"},function(){return "same";});`,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := httptest.NewRecorder()
	s := &Subscription{
		id: "subscription", path: "hello", requestID: "request", snap: snapshot, ctx: ctx, cancel: cancel,
		service: service, broadcaster: NewBroadcaster(service, DefaultConfig()), w: w, flusher: http.NewResponseController(w), maxEventSize: 1 << 20,
	}
	s.runOnce() // initial
	s.runOnce() // invalidation rerun with unchanged result
	if len(observer.queries) != 2 || observer.completed != 2 {
		t.Fatalf("initial/rerun missing: %#v", observer)
	}
	if strings.Count(w.Body.String(), `"op":"message"`) != 1 {
		t.Fatalf("unchanged result was sent twice: %s", w.Body.String())
	}
	s.runOnce() // denied rerun
	for i := 0; i < 20; i++ {
		s.runOnce()
	}
	if !s.admissionPaused || len(observer.queries) != 3 || observer.completed != 2 {
		t.Fatalf("denial hot-reran or settled: %#v", observer)
	}
	if ctx.Err() != nil {
		t.Fatal("denial closed SSE and would trigger reconnect")
	}
	if !strings.Contains(w.Body.String(), "subscription paused") || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("unsafe/missing error: %s", w.Body.String())
	}
	ids := map[string]bool{}
	for _, info := range observer.queries {
		if info.Origin != "realtime" || info.ID == "request" || info.ParentID != "" || info.ID != info.RootID || ids[info.ID] {
			t.Fatalf("bad realtime identity: %#v", info)
		}
		ids[info.ID] = true
	}
}
