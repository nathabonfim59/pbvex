package runtime

import "context"

type scheduleNamespaceContextKey struct{}

type ScheduleNamespaces struct {
	Owner  string
	Target string
}

// WithScheduleNamespaces binds durable component ownership to a scheduler
// operation without changing the public Scheduler interface.
func WithScheduleNamespaces(ctx context.Context, owner, target string) context.Context {
	return context.WithValue(ctx, scheduleNamespaceContextKey{}, ScheduleNamespaces{Owner: owner, Target: target})
}

func ScheduleNamespacesFromContext(ctx context.Context) (ScheduleNamespaces, bool) {
	value, ok := ctx.Value(scheduleNamespaceContextKey{}).(ScheduleNamespaces)
	return value, ok
}
