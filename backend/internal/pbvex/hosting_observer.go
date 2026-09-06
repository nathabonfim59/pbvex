package pbvex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nathabonfim59/pbvex/backend/hosting"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
)

// Bounded reporting policy for the execution observer. Attempts reuse
// identical protocol identifiers and content so services can deduplicate.
const (
	executionAdmitAttempts    = 2
	executionStartedAttempts  = 2
	executionCompleteAttempts = 3
	executionRetryBackoff     = 50 * time.Millisecond
	// executionCompleteBudget bounds completion reporting independently of the
	// (possibly already canceled or expired) caller context.
	executionCompleteBudget = 10 * time.Second
	// maxTrackedExecutions bounds the fallback reservation table. Exhausting it
	// denies new execution starts instead of growing without bound.
	maxTrackedExecutions = 1024
)

var (
	// errHostingMeteringUnhealthy is returned by Begin after a reporting
	// exchange stayed uncertain past its bounded retries. New execution starts
	// are rejected until process restart; in-flight completions still settle.
	errHostingMeteringUnhealthy = errors.New("hosting execution metering is unhealthy; new executions are rejected until process restart")
	// errInvalidExecutionIdentity denies executions whose runtime-supplied
	// identity cannot be projected onto the protocol without inventing labels.
	errInvalidExecutionIdentity   = errors.New("hosting execution identity is invalid")
	errTrackedExecutionsExhausted = errors.New("hosting execution tracking bound reached")
)

// executionReservation is the admitted, started protocol state for one
// runtime execution attempt. It travels to End through the context returned
// by Begin and is additionally retained in a bounded fallback table keyed by
// the runtime execution ID.
type executionReservation struct {
	operation hosting.Operation
	decision  hosting.Decision
	ended     atomic.Bool
}

type reservationContextKey struct{}

// hostingExecutionObserver implements runtime.ExecutionObserver over the
// public hosting policy protocol. Admission and the started report happen
// synchronously inside Begin, before any user code runs; Begin errors deny
// the execution. Completion is reported in End exactly once per successful
// Begin, using a cancellation-independent bounded context.
//
// This is a synchronous acknowledgement foundation, not a durable outbox:
// every exchange costs bounded socket round-trips on the execution path and
// uncertainty after exhausted retries latches the observer unhealthy instead
// of silently dropping or duplicating protocol state.
type hostingExecutionObserver struct {
	logger    *slog.Logger
	client    *hosting.Client
	external  runtime.ExecutionObserver
	sessionID string
	sequence  atomic.Uint64
	unhealthy atomic.Bool
	now       func() time.Time

	mu         sync.Mutex
	inFlight   map[string]*executionReservation
	maxTracked int
}

func newHostingExecutionObserver(logger *slog.Logger, client *hosting.Client, external runtime.ExecutionObserver) *hostingExecutionObserver {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &hostingExecutionObserver{
		logger:     logger,
		client:     client,
		external:   external,
		sessionID:  hosting.NewID(),
		inFlight:   make(map[string]*executionReservation),
		maxTracked: maxTrackedExecutions,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// Begin admits the execution and reports started before returning. The
// returned context carries the reservation for End. Denied, malformed,
// saturated and uncertain admissions all prevent execution.
func (o *hostingExecutionObserver) Begin(ctx context.Context, info runtime.ExecutionInfo) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if o.unhealthy.Load() {
		return ctx, errHostingMeteringUnhealthy
	}
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	if o.external != nil {
		// External observers run first so their denial prevents reserving
		// provider capacity for work that would never start.
		derived, err := o.external.Begin(ctx, info)
		if err != nil {
			return ctx, err
		}
		ctx = derived
	}
	res, err := o.admitAndStart(ctx, info)
	if err != nil {
		if o.external != nil {
			// The external observer saw a successful Begin; pair it with one
			// End so its own accounting stays exactly once even though the
			// runtime never observed success and will not call End.
			o.external.End(ctx, info, runtime.ExecutionResult{Err: err})
		}
		return ctx, err
	}
	return context.WithValue(ctx, reservationContextKey{}, res), nil
}

// End settles the reservation with one completed event. It is called exactly
// once per successful Begin by the runtime; duplicate calls are ignored. A
// failed completion latches the observer unhealthy and never fabricates or
// refunds protocol state.
func (o *hostingExecutionObserver) End(ctx context.Context, info runtime.ExecutionInfo, result runtime.ExecutionResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	res := reservationFromContext(ctx)
	if res != nil {
		o.untrack(info.ID)
	} else {
		// The runtime passes a Begin-derived context to End; the bounded
		// fallback covers wrappers that drop context values.
		res = o.untrack(info.ID)
	}
	if res == nil {
		// Completion without a matching admission cannot be settled. Latch:
		// the provider may hold started work that this process can no longer
		// account for.
		o.latchUnhealthy("unknown reservation", "", sanitizedID(info.ID))
		return
	}
	if !res.ended.CompareAndSwap(false, true) {
		o.logger.Error("duplicate hosting execution completion ignored", "invocationId", sanitizedID(info.ID))
		return
	}
	// The caller context may already be canceled or expired; completion must
	// still be attempted within its own bounded budget.
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), executionCompleteBudget)
	defer cancel()
	// Report failure latches the observer unhealthy inside report; the
	// provider may hold a started reservation without completion until its
	// reconciliation. This handling is explicit, never silently ignored.
	_ = o.report(reportCtx, res.event(o.sequence.Add(1), "completed", outcomeFor(result), durationMicros(result.Duration), o.now()), executionCompleteAttempts)
	if o.external != nil {
		o.external.End(ctx, info, result)
	}
}

func (o *hostingExecutionObserver) admitAndStart(ctx context.Context, info runtime.ExecutionInfo) (*executionReservation, error) {
	operation, err := protocolOperation(info, o.sessionID)
	if err != nil {
		return nil, err
	}
	res := &executionReservation{operation: operation}
	if !o.track(info.ID, res) {
		return nil, errTrackedExecutionsExhausted
	}
	decision, err := o.admit(ctx, hosting.AdmissionRequest{
		RequestID:  hosting.NewID(),
		Capability: hosting.FunctionExecute,
		Operation:  operation,
	})
	if err != nil {
		o.untrack(info.ID)
		return nil, err
	}
	res.decision = decision
	if err := o.report(ctx, res.event(o.sequence.Add(1), "started", "", 0, o.now()), executionStartedAttempts); err != nil {
		// The started acknowledgement is uncertain: the execution is denied,
		// but the reservation is deliberately not released because the
		// service may already have recorded the start.
		o.untrack(info.ID)
		return nil, fmt.Errorf("hosting started report failed: %w", err)
	}
	return res, nil
}

// admit sends the admission with bounded retries. Identical request
// identifiers and content make retries idempotent. Saturation of the local
// client is a clean deny: nothing reached the service. Any other exhausted
// exchange is uncertain about server-side state and latches unhealthy.
func (o *hostingExecutionObserver) admit(ctx context.Context, request hosting.AdmissionRequest) (hosting.Decision, error) {
	var decision hosting.Decision
	var err error
	for attempt := 1; attempt <= executionAdmitAttempts; attempt++ {
		if attempt > 1 {
			o.pause(ctx, executionRetryBackoff)
		}
		decision, err = o.client.Admit(ctx, request)
		if err == nil {
			if !decision.Allowed {
				return hosting.Decision{}, &hosting.DeniedError{Code: decision.Code}
			}
			return decision, nil
		}
		if errors.Is(err, hosting.ErrBusy) {
			return hosting.Decision{}, errors.Join(deploy.ErrExecutionBusy, err)
		}
	}
	o.latchUnhealthy("admission", request.RequestID, request.Operation.ID)
	return hosting.Decision{}, fmt.Errorf("hosting admission failed: %w", err)
}

// report sends one event with bounded retries on identical content. Any
// exhausted event delivery latches the observer unhealthy: dropped lifecycle
// events are never silently ignored, and there is no spool to replay from.
func (o *hostingExecutionObserver) report(ctx context.Context, event hosting.Event, attempts int) error {
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			o.pause(ctx, executionRetryBackoff)
		}
		if err = o.client.Report(ctx, event); err == nil {
			return nil
		}
	}
	o.latchUnhealthy("event-"+event.Phase, event.EventID, event.Operation.ID)
	return fmt.Errorf("hosting event report failed: %w", err)
}

// latchUnhealthy permanently rejects new execution starts. Only
// protocol-generated or projected identifiers are logged; never error
// strings, arguments, results or payloads.
func (o *hostingExecutionObserver) latchUnhealthy(reason, protocolID, invocationID string) {
	if !o.unhealthy.CompareAndSwap(false, true) {
		return
	}
	o.logger.Error("hosting policy reporting failed; rejecting new execution starts until restart",
		"reason", reason, "protocolId", protocolID, "invocationId", invocationID)
}

func (o *hostingExecutionObserver) track(id string, res *executionReservation) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.inFlight) >= o.maxTracked {
		return false
	}
	o.inFlight[id] = res
	return true
}

func (o *hostingExecutionObserver) untrack(id string) *executionReservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	res := o.inFlight[id]
	delete(o.inFlight, id)
	return res
}

func (o *hostingExecutionObserver) pause(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// event builds a protocol event for this reservation. Sequence is a positive
// process-wide counter; event identifiers are unique per event and stable
// across its retries.
func (r *executionReservation) event(sequence uint64, phase, outcome string, durationMicros int64, at time.Time) hosting.Event {
	return hosting.Event{
		EventID:        hosting.NewID(),
		Sequence:       sequence,
		Operation:      r.operation,
		ReservationID:  r.decision.ReservationID,
		PolicyVersion:  r.decision.PolicyVersion,
		Phase:          phase,
		Outcome:        outcome,
		At:             at,
		DurationMicros: durationMicros,
	}
}

func reservationFromContext(ctx context.Context) *executionReservation {
	if ctx == nil {
		return nil
	}
	res, _ := ctx.Value(reservationContextKey{}).(*executionReservation)
	return res
}

// outcomeFor sanitizes the runtime result into a bounded protocol outcome.
// Runtime errors may contain application data and are never transmitted.
func outcomeFor(result runtime.ExecutionResult) string {
	switch {
	case result.Err == nil:
		return "success"
	case errors.Is(result.Err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(result.Err, context.Canceled):
		return "canceled"
	default:
		return "error"
	}
}

func durationMicros(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return d.Microseconds()
}

// protocolOperation projects runtime execution identity onto the protocol.
// Attempt identifiers pass through when already token-safe and are otherwise
// projected onto a deterministic SHA-256 hexadecimal identifier so retries
// and correlation stay stable. Required labels that cannot be represented
// deny the execution; optional labels that cannot be represented are omitted.
func protocolOperation(info runtime.ExecutionInfo, sessionID string) (hosting.Operation, error) {
	id, err := executionID(info.ID)
	if err != nil {
		return hosting.Operation{}, err
	}
	rootID := id
	if sanitized, err := executionID(info.RootID); err == nil {
		rootID = sanitized
	}
	kind, err := protocolKind(info)
	if err != nil {
		return hosting.Operation{}, err
	}
	if !hosting.ValidToken(info.Origin) {
		return hosting.Operation{}, errInvalidExecutionIdentity
	}
	operation := hosting.Operation{
		ID:        id,
		RootID:    rootID,
		SessionID: sessionID,
		Kind:      kind,
		Origin:    info.Origin,
	}
	if parent, err := executionID(info.ParentID); err == nil {
		operation.ParentID = parent
	}
	if info.DeploymentID != "" && hosting.ValidToken(info.DeploymentID) {
		operation.DeploymentID = info.DeploymentID
	}
	if info.FunctionName != "" && hosting.ValidToken(info.FunctionName) {
		operation.FunctionName = info.FunctionName
	}
	if info.Namespace != "" && hosting.ValidToken(info.Namespace) {
		operation.Namespace = info.Namespace
	}
	return operation, nil
}

// protocolKind maps runtime function types onto protocol kind labels. Bundle
// loads and migrations are not functions: the runtime reports an empty
// FunctionType with a dedicated origin, mapped to public bundle.load and
// migration.application labels rather than an invalid request. Unrecognized
// non-empty function types pass through only when token-safe.
func protocolKind(info runtime.ExecutionInfo) (string, error) {
	switch info.FunctionType {
	case deploy.FunctionTypeQuery:
		return "query", nil
	case deploy.FunctionTypeMutation:
		return "mutation", nil
	case deploy.FunctionTypeAction:
		return "action", nil
	case deploy.FunctionTypeHTTPAction:
		return "http", nil
	case "":
		switch info.Origin {
		case "bundle_load":
			return "bundle.load", nil
		case "migration":
			return "migration.application", nil
		}
		return "", errInvalidExecutionIdentity
	}
	if hosting.ValidToken(string(info.FunctionType)) {
		return string(info.FunctionType), nil
	}
	return "", errInvalidExecutionIdentity
}

// executionID projects one runtime attempt identifier onto the protocol.
func executionID(s string) (string, error) {
	if s == "" {
		return "", errInvalidExecutionIdentity
	}
	if hosting.ValidToken(s) {
		return s, nil
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:]), nil
}

// sanitizedID makes a runtime identifier safe for log fields.
func sanitizedID(s string) string {
	if id, err := executionID(s); err == nil {
		return id
	}
	return "invalid"
}
