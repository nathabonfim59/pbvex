package deploy

import (
	"context"
	"errors"
)

type executionOriginKey struct{}

// WithExecutionOrigin marks the server-owned entry path. Nested invocations
// inherit it; it does not replace authentication or request identity.
func WithExecutionOrigin(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, executionOriginKey{}, origin)
}

// ExecutionOrigin returns the entry path, defaulting to an ordinary call.
func ExecutionOrigin(ctx context.Context) string {
	if origin, _ := ctx.Value(executionOriginKey{}).(string); origin != "" {
		return origin
	}
	return "call"
}

// ExecutionAdmissionError identifies work rejected before entering user code.
// Its public text deliberately excludes provider diagnostics and secrets. Err
// remains available through errors.Is/As for trusted Go integrations.
type ExecutionAdmissionError struct {
	Err error
	// Started is true when an admitted parent propagated a nested denial.
	// Such work may have side effects and must not be automatically refunded
	// and replayed as though admission prevented the entire root attempt.
	Started bool
}

func (e *ExecutionAdmissionError) Error() string { return "execution admission unavailable or denied" }
func (e *ExecutionAdmissionError) Unwrap() error { return e.Err }

func IsExecutionAdmissionError(err error) bool {
	var admission *ExecutionAdmissionError
	return errors.As(err, &admission)
}

// ErrExecutionBusy indicates the instance-wide root execution cap is full.
var ErrExecutionBusy = errors.New("execution concurrency limit reached")
