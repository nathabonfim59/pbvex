package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
)

func TestSchedulerAdmissionDeferralAndOrigin(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "root_denied", true: "nested_denied"}[started], func(t *testing.T) {
			app, svc := newSchedulerTestApp(t)
			svc.Stop()
			origins := make(chan string, 4)
			executor := &testExecutor{invokeFn: func(ctx context.Context, _, _, _ string, _ any) (any, error) {
				origins <- deploy.ExecutionOrigin(ctx)
				return nil, &deploy.ExecutionAdmissionError{Err: errors.New("provider secret"), Started: started}
			}}
			svc = NewService(app, executor, svc.config)
			if err := svc.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer svc.Stop()
			ctx := schema.WithApp(context.Background(), app)
			before := time.Now()
			id, err := svc.RunAfter(ctx, 0, "d1", "hello", nil)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case origin := <-origins:
				if origin != "scheduler" {
					t.Fatalf("origin=%s", origin)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("job never ran")
			}
			waitForWorkerIdle(t, svc, 2*time.Second)
			status, err := svc.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if started {
				if status.Status != JobStatusFailed || status.Attempts != 1 {
					t.Fatalf("partial work was replayed/refunded: %#v", status)
				}
			} else {
				if status.Status != JobStatusPending || status.Attempts != 0 || status.ScheduledAt.Before(before.Add(59*time.Second)) {
					t.Fatalf("denial burned retry or hot-requeued: %#v", status)
				}
			}
			if executor.callCnt.Load() != 1 || strings.Contains(status.Error, "secret") {
				t.Fatalf("denial replayed or leaked: %#v", status)
			}
		})
	}
}
