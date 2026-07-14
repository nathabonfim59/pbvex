package runtime

import (
	"context"

	"github.com/nathabonfim59/pbvex/backend/internal/auth"
)

// AuthContext carries the identity of the caller into the runtime so that
// storage and other host services can bind capabilities to the requester.
type AuthContext struct {
	IsAuthenticated bool
	// TokenIdentifier is stable across collection renames and globally unique
	// across PocketBase auth collections.
	TokenIdentifier string
	Identity        *auth.UserIdentity
	RequestID       string
}

type authContextKey struct{}

// WithAuthContext returns a context that carries the auth context.
func WithAuthContext(ctx context.Context, auth AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, auth)
}

// AuthFromContext extracts the auth context, if any.
func AuthFromContext(ctx context.Context) (AuthContext, bool) {
	auth, ok := ctx.Value(authContextKey{}).(AuthContext)
	return auth, ok
}
