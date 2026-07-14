package storage

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

type appContextKey struct{}

// WithApp returns a context carrying the PocketBase app instance to use for storage operations.
// This allows operations to participate in an outer transaction.
func WithApp(ctx context.Context, app core.App) context.Context {
	return context.WithValue(ctx, appContextKey{}, app)
}

// AppFromContext returns the app instance from the context, or nil/false if none.
func AppFromContext(ctx context.Context) (core.App, bool) {
	if app, ok := ctx.Value(appContextKey{}).(core.App); ok && app != nil {
		return app, true
	}
	return nil, false
}
