package realtime

import (
	"context"
	"errors"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	pbtests "github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/logger"
)

func TestRealtimeApplicationErrorPayloadPreservesCategoryAndData(t *testing.T) {
	subscription := &Subscription{requestID: "rid"}
	payload := subscription.errorPayload(&deploy.ApplicationError{
		Category: deploy.ApplicationErrorConflict,
		Data:     map[string]any{"resource": "note"},
		HasData:  true,
	})
	if payload["code"] != "conflict" || payload["message"] != "Conflict." || payload["requestId"] != "rid" {
		t.Fatalf("unexpected payload %#v", payload)
	}
	if data, ok := payload["data"].(map[string]any); !ok || data["resource"] != "note" {
		t.Fatalf("unexpected application data %#v", payload["data"])
	}
}

func TestRealtimeUnexpectedFailureIsLoggedOnceAndMasked(t *testing.T) {
	app, err := pbtests.NewTestAppWithConfig(core.BaseAppConfig{IsDev: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)
	app.Settings().Logs.MaxDays = 1

	service := deploy.NewService(app, nil, nil, deploy.DefaultConfig())
	subscription := &Subscription{
		id: "sub-1", path: "notes:list", requestID: "rid-realtime", service: service,
	}
	payload := subscription.executionErrorPayload(errors.New("realtime handler secret"))
	if payload["code"] != "internal" || payload["requestId"] != "rid-realtime" {
		t.Fatalf("unexpected masked payload: %#v", payload)
	}
	if payload["message"] == "realtime handler secret" {
		t.Fatal("unexpected cause leaked to realtime payload")
	}

	handler, ok := app.Logger().Handler().(*logger.BatchHandler)
	if !ok {
		t.Fatalf("logger handler is %T", app.Logger().Handler())
	}
	if err := handler.WriteAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	var logs []*core.Log
	if err := app.AuxDB().Select("*").From(core.LogsTableName).
		Where(dbx.HashExp{"message": "PBVex handler failed"}).All(&logs); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("handler log count = %d, want 1", len(logs))
	}
	data := logs[0].Data
	if data["requestId"] != "rid-realtime" || data["subscriptionId"] != "sub-1" ||
		data["function"] != "notes:list" || data["functionType"] != "query" || data["phase"] != "handler_execution" {
		t.Fatalf("unexpected log context: %#v", data)
	}

	subscription.executionErrorPayload(&deploy.ApplicationError{Category: deploy.ApplicationErrorConflict})
	if err := handler.WriteAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	logs = nil
	if err := app.AuxDB().Select("*").From(core.LogsTableName).
		Where(dbx.HashExp{"message": "PBVex handler failed"}).All(&logs); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("application error produced an unexpected log; count = %d", len(logs))
	}
}
