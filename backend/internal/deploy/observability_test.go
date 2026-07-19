package deploy

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/pocketbase/pocketbase/tools/logger"
)

func TestUnexpectedHandlerFailureLogging(t *testing.T) {
	var records []*logger.Log
	logHandler := logger.NewBatchHandler(logger.BatchOptions{
		BatchSize: 1,
		WriteFunc: func(_ context.Context, logs []*logger.Log) error {
			records = append(records, logs...)
			return nil
		},
	})
	log := slog.New(logHandler)
	cause := errors.New("handler secret")
	err := WrapFunctionFailure(cause, "notes:create", FunctionTypeMutation, FailurePhaseHandlerExecution)
	if !logUnexpectedHandlerFailure(log, err, HandlerFailureContext{RequestID: "rid"}) {
		t.Fatal("unexpected failure was not logged")
	}
	if len(records) != 1 {
		t.Fatalf("log count = %d, want 1", len(records))
	}
	record := records[0]
	if record.Message != "PBVex handler failed" || record.Data["function"] != "notes:create" ||
		record.Data["functionType"] != "mutation" || record.Data["phase"] != "handler_execution" ||
		record.Data["requestId"] != "rid" || record.Data["errorType"] != "*errors.errorString" || record.Data["error"] != nil {
		t.Fatalf("unexpected log: %#v", record)
	}

	applicationErr := &ApplicationError{Category: ApplicationErrorConflict}
	if logUnexpectedHandlerFailure(log, applicationErr, HandlerFailureContext{}) {
		t.Fatal("application error was logged as unexpected")
	}
	if len(records) != 1 {
		t.Fatalf("application error changed log count to %d", len(records))
	}
}

func TestFailurePhaseForValidationLimitsAndTimeout(t *testing.T) {
	cases := []struct {
		err  error
		want FailurePhase
	}{
		{errors.New("invalid function arguments: expected string"), FailurePhaseArgumentValidation},
		{errors.New("invalid function return value: expected string"), FailurePhaseReturnValidation},
		{&ValueSizeError{Label: "function arguments", Limit: 1}, FailurePhaseArgumentLimit},
		{&ValueSizeError{Label: "function return value", Limit: 1}, FailurePhaseReturnLimit},
		{context.DeadlineExceeded, FailurePhaseTimeout},
	}
	for _, tc := range cases {
		if got := FailurePhaseFor(tc.err); got != tc.want {
			t.Errorf("FailurePhaseFor(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestWrapFunctionFailurePreservesRuntimeSetupContext(t *testing.T) {
	err := WrapFunctionFailure(errors.New("compile failed"), "notes:list", FunctionTypeQuery, FailurePhaseRuntimeSetup)
	var failure *FunctionFailure
	if !errors.As(err, &failure) {
		t.Fatal("failure context was not attached")
	}
	if failure.FunctionName != "notes:list" || failure.FunctionType != FunctionTypeQuery || failure.Phase != FailurePhaseRuntimeSetup {
		t.Fatalf("unexpected failure context: %#v", failure)
	}
}
