package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

// ExecutionInfo contains metadata only, never arguments, results or identity.
// bundle_load and migration use an empty FunctionType (they are not functions).
// StartedAt is the admission-boundary timestamp. A denied Begin is an attempted
// admission, not proof that code ran. Root bundle loads are independent roots;
// nested bundle loads and handlers are children of the calling handler.
type ExecutionInfo struct {
	ID, RootID, ParentID, DeploymentID, FunctionName, Namespace string
	FunctionType                                                deploy.FunctionType
	Origin                                                      string
	Depth                                                       int
	StartedAt                                                   time.Time
}

type ExecutionResult struct {
	// Duration is wall time from the admission boundary through result
	// validation, including Begin latency but excluding End reporting. Handler
	// results are reported before the caller's surrounding transaction commits.
	Duration time.Duration
	// Err is available for trusted classification. It may contain application
	// data and must not be serialized verbatim into telemetry.
	Err error
}

// ExecutionObserver admits each Go-to-user-JS execution and observes its result.
// Begin must return a context derived from its input. End is synchronous and
// called exactly once iff Begin succeeded. Implementations must bound reporting
// and use cancellation-independent reporting contexts themselves when needed.
// Begin errors are preserved through ExecutionAdmissionError; End must not panic.
type ExecutionObserver interface {
	Begin(context.Context, ExecutionInfo) (context.Context, error)
	End(context.Context, ExecutionInfo, ExecutionResult)
}

type executionInfoKey struct{}

// ExecutionFromContext exposes the current invocation to trusted Go adapters.
func ExecutionFromContext(ctx context.Context) (ExecutionInfo, bool) {
	info, ok := ctx.Value(executionInfoKey{}).(ExecutionInfo)
	return info, ok
}

func (m *Manager) beginExecution(ctx context.Context, info ExecutionInfo) (context.Context, func(error), error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	if m == nil || m.config.ExecutionObserver == nil {
		return ctx, func(error) {}, nil
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return ctx, nil, &deploy.ExecutionAdmissionError{Err: err}
	}
	info.ID = hex.EncodeToString(id[:])
	info.RootID = info.ID
	if parent, ok := ExecutionFromContext(ctx); ok {
		info.ParentID, info.RootID, info.Depth = parent.ID, parent.RootID, parent.Depth+1
	}
	if info.Origin == "" {
		info.Origin = deploy.ExecutionOrigin(ctx)
	}
	info.StartedAt = time.Now()
	observed, err := m.config.ExecutionObserver.Begin(ctx, info)
	if err != nil {
		return ctx, nil, &deploy.ExecutionAdmissionError{Err: err}
	}
	// A broken adapter must never turn a successful reservation into unreported
	// work or remove an earlier caller deadline/cancellation.
	if observed == nil {
		err = &deploy.ExecutionAdmissionError{Err: fmt.Errorf("observer returned nil context")}
		m.config.ExecutionObserver.End(ctx, info, ExecutionResult{Duration: time.Since(info.StartedAt), Err: err})
		return ctx, nil, err
	}
	bounded, cancel := context.WithCancel(observed)
	stop := context.AfterFunc(ctx, cancel)
	if deadline, ok := ctx.Deadline(); ok {
		var deadlineCancel context.CancelFunc
		bounded, deadlineCancel = context.WithDeadline(bounded, deadline)
		previousCancel := cancel
		cancel = func() { deadlineCancel(); previousCancel() }
	}
	if ctx.Err() != nil {
		cancel()
	}
	bounded = context.WithValue(bounded, executionInfoKey{}, info)
	return bounded, func(err error) {
		defer cancel()
		defer stop()
		m.config.ExecutionObserver.End(bounded, info, ExecutionResult{Duration: time.Since(info.StartedAt), Err: err})
	}, nil
}

// Root slots are acquired without a queue, before any pool wait or evaluation.
// Nested calls bypass this method and stay inside their parent's slot.
func (m *Manager) acquireRoot(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m == nil || m.executionSlots == nil {
		return func() {}, nil
	}
	select {
	case m.executionSlots <- struct{}{}:
		return func() { <-m.executionSlots }, nil
	default:
		return nil, &deploy.ExecutionAdmissionError{Err: deploy.ErrExecutionBusy}
	}
}

func (e *entry) beginInvocation(inv *Invocation) error {
	ctx, finish, err := e.manager.beginExecution(inv.Ctx, ExecutionInfo{
		DeploymentID: inv.DeploymentID, FunctionName: inv.FunctionName,
		FunctionType: inv.FunctionType, Namespace: inv.Namespace,
	})
	if err != nil {
		return err
	}
	inv.Ctx, inv.finishExecution = ctx, finish
	return ctx.Err()
}

// Preserve host panics while avoiding a fabricated successful completion on
// stack unwinding. Process termination without unwinding produces no End.
func finishObservedExecution(finish func(error), resultErr *error) {
	panicked := recover()
	if panicked != nil {
		*resultErr = fmt.Errorf("execution panicked")
	}
	if finish != nil {
		finish(*resultErr)
	}
	if panicked != nil {
		panic(panicked)
	}
}

func (inv *Invocation) endExecution(resultErr *error) {
	panicked := recover()
	if panicked != nil {
		*resultErr = fmt.Errorf("execution panicked")
	}
	if inv.stopExecutionTimer != nil {
		inv.stopExecutionTimer()
	}
	if inv.finishExecution != nil {
		if deploy.IsExecutionAdmissionError(*resultErr) {
			*resultErr = &deploy.ExecutionAdmissionError{Err: *resultErr, Started: true}
		}
		inv.finishExecution(*resultErr)
	}
	if panicked != nil {
		panic(panicked)
	}
}
