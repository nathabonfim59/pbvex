package deploy

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"reflect"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// FailurePhase identifies the stage at which a function invocation failed.
type FailurePhase string

const (
	FailurePhaseHandlerExecution   FailurePhase = "handler_execution"
	FailurePhaseArgumentValidation FailurePhase = "argument_validation"
	FailurePhaseReturnValidation   FailurePhase = "return_validation"
	FailurePhaseTimeout            FailurePhase = "timeout"
	FailurePhaseArgumentLimit      FailurePhase = "argument_limit"
	FailurePhaseReturnLimit        FailurePhase = "return_limit"
	FailurePhaseRuntimeSetup       FailurePhase = "runtime_setup"
)

// FunctionFailure adds safe invocation metadata while retaining the original error.
type FunctionFailure struct {
	FunctionName string
	FunctionType FunctionType
	Phase        FailurePhase
	Err          error
}

func (e *FunctionFailure) Error() string { return e.Err.Error() }
func (e *FunctionFailure) Unwrap() error { return e.Err }

// WrapFunctionFailure adds function context unless an inner invocation already did.
func WrapFunctionFailure(err error, name string, functionType FunctionType, phase FailurePhase) error {
	if err == nil {
		return nil
	}
	var existing *FunctionFailure
	if errors.As(err, &existing) {
		return err
	}
	if phase == "" {
		phase = FailurePhaseFor(err)
	}
	return &FunctionFailure{FunctionName: name, FunctionType: functionType, Phase: phase, Err: err}
}

// FailurePhaseFor classifies failures without inspecting or logging invocation values.
func FailurePhaseFor(err error) FailurePhase {
	if errors.Is(err, context.DeadlineExceeded) {
		return FailurePhaseTimeout
	}
	var sizeErr *ValueSizeError
	if errors.As(err, &sizeErr) {
		if strings.Contains(sizeErr.Label, "argument") || strings.Contains(sizeErr.Label, "request body") {
			return FailurePhaseArgumentLimit
		}
		return FailurePhaseReturnLimit
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "invalid function arguments") || strings.Contains(message, "invalid nested function arguments"):
		return FailurePhaseArgumentValidation
	case strings.Contains(message, "invalid function return value"):
		return FailurePhaseReturnValidation
	default:
		return FailurePhaseHandlerExecution
	}
}

// HandlerFailureContext contains only bounded, non-value invocation identifiers.
type HandlerFailureContext struct {
	RequestID      string
	SubscriptionID string
	JobID          string
	FunctionName   string
	FunctionType   FunctionType
	Phase          FailurePhase
}

// LogUnexpectedHandlerFailure records an unexpected function failure once at
// an outward execution boundary. ApplicationError and caller cancellation are
// normal outcomes and are deliberately excluded.
func LogUnexpectedHandlerFailure(app core.App, err error, fields HandlerFailureContext) bool {
	if app == nil {
		return false
	}
	logged := logUnexpectedHandlerFailure(app.Logger(), err, fields)
	if logged && os.Getenv("PBVEX_HANDLER_LOG_STDERR") == "1" && !app.IsDev() {
		failure := functionFailure(err, fields)
		context := []string{"function=" + failure.FunctionName, "type=" + string(failure.FunctionType), "phase=" + string(failure.Phase)}
		if fields.RequestID != "" {
			context = append(context, "requestId="+fields.RequestID)
		}
		if fields.SubscriptionID != "" {
			context = append(context, "subscriptionId="+fields.SubscriptionID)
		}
		if fields.JobID != "" {
			context = append(context, "jobId="+fields.JobID)
		}
		log.Printf("PBVex handler failed %s errorType=%s", strings.Join(context, " "), safeErrorType(err))
	}
	return logged
}

// LogUnexpectedHandlerFailure records a failure through the service's app logger.
func (s *Service) LogUnexpectedHandlerFailure(err error, fields HandlerFailureContext) bool {
	return LogUnexpectedHandlerFailure(s.app, err, fields)
}

func logUnexpectedHandlerFailure(logger *slog.Logger, err error, fields HandlerFailureContext) bool {
	if err == nil || logger == nil || IsExpectedApplicationError(err) || errors.Is(err, context.Canceled) ||
		errors.Is(err, ErrFunctionNotFound) || errors.Is(err, ErrDeploymentNotFound) ||
		errors.Is(err, ErrActiveNotFound) || errors.Is(err, ErrForbidden) {
		return false
	}
	failure := functionFailure(err, fields)
	attrs := []any{
		"function", failure.FunctionName,
		"functionType", string(failure.FunctionType),
		"phase", string(failure.Phase),
		"errorType", safeErrorType(err),
	}
	if fields.RequestID != "" {
		attrs = append(attrs, "requestId", fields.RequestID)
	}
	if fields.SubscriptionID != "" {
		attrs = append(attrs, "subscriptionId", fields.SubscriptionID)
	}
	if fields.JobID != "" {
		attrs = append(attrs, "jobId", fields.JobID)
	}
	logger.Error("PBVex handler failed", attrs...)
	return true
}

func safeErrorType(err error) string {
	for {
		switch typed := err.(type) {
		case *FunctionFailure:
			err = typed.Err
		default:
			typeName := reflect.TypeOf(err).String()
			if strings.HasPrefix(typeName, "*goja.") {
				return "javascript_exception"
			}
			return typeName
		}
	}
}

func functionFailure(err error, fallback HandlerFailureContext) *FunctionFailure {
	var failure *FunctionFailure
	if errors.As(err, &failure) {
		return failure
	}
	phase := fallback.Phase
	if phase == "" {
		phase = FailurePhaseFor(err)
	}
	return &FunctionFailure{FunctionName: fallback.FunctionName, FunctionType: fallback.FunctionType, Phase: phase, Err: err}
}

// IsExpectedApplicationError reports whether err is a handler-authored normal outcome.
func IsExpectedApplicationError(err error) bool {
	var applicationErr *ApplicationError
	return errors.As(err, &applicationErr)
}
