package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/core"
)

const (
	// maxNestedDepth is the maximum number of nested function calls allowed in a
	// single invocation tree.
	maxNestedDepth = 8

	// maxTotalWork is the maximum number of nested (runQuery/runMutation/runAction)
	// calls allowed in a single invocation tree.
	maxTotalWork = 64
)

// Invocation carries request-scoped context for a single function invocation.
// It is immutable except for the context/deadline information and the nested
// call bookkeeping.
type Invocation struct {
	// Ctx is the caller context for cancellation and deadlines.
	Ctx context.Context

	// Identity is the authenticated user identity for this invocation, or nil
	// for unauthenticated requests.
	Identity *auth.UserIdentity

	// RequestID is the request identifier propagated through HTTP/realtime and
	// nested calls.
	RequestID string

	// DeploymentID identifies the deployment whose runtime is used.
	DeploymentID string

	// FunctionType is the type of the function being invoked.
	FunctionType deploy.FunctionType
	FunctionName string
	// Namespace is root or the deterministic component namespace selected by
	// the target function descriptor.
	Namespace string

	// HTTPRequest is the parsed HTTP request envelope for httpAction calls.
	HTTPRequest *deploy.HTTPRequestEnvelope

	// App and Manifest are set for database-aware invocations. When App is nil,
	// the runtime uses no-op stubs for db/storage/scheduler.
	App      core.App
	Manifest deploy.DeploymentManifest

	// MaxArgsBytes and MaxReturnBytes are wire size limits from the manifest.
	MaxArgsBytes   int64
	MaxReturnBytes int64
	RequestTimeout time.Duration

	// Depth is the current nested call depth (0 for the outer invocation).
	Depth int

	// Work is a shared, nonrefundable cumulative counter for all nested calls
	// in the invocation tree. It is a pointer so that parent, child, and
	// sibling calls all observe the same monotonically increasing budget.
	Work *int

	// NestedInvoke dispatches nested calls through a fresh runtime entry while
	// preserving this invocation tree's request-scoped state.
	NestedInvoke func(parent *Invocation, functionName string, targetType deploy.FunctionType, args any, depth int) (any, error)
}

func namespaceForDescriptor(manifest deploy.DeploymentManifest, descriptor deploy.FunctionDescriptor) string {
	if namespace, ok := deploy.NamespaceForModule(manifest, descriptor.ModulePath); ok {
		return namespace.ID
	}
	return deploy.RootNamespace
}

// isActionLike returns true if the invocation type may perform nested calls.
func (inv *Invocation) isActionLike() bool {
	return inv.FunctionType == deploy.FunctionTypeAction || inv.FunctionType == deploy.FunctionTypeHTTPAction
}

// canRunNested reports whether the invocation is allowed to make a nested call
// of the given type and depth. The work counter is charged (incremented)
// BEFORE the bounds check and is never refunded on failure.
func (inv *Invocation) canRunNested(target deploy.FunctionType, depth int) error {
	if !inv.isActionLike() {
		return fmt.Errorf("nested calls are not allowed from %s", inv.FunctionType)
	}
	if depth > maxNestedDepth {
		return fmt.Errorf("nested call depth exceeded")
	}
	if inv.Work != nil {
		*inv.Work++
		if *inv.Work > maxTotalWork {
			return fmt.Errorf("nested function work budget exceeded")
		}
	}
	if target == deploy.FunctionTypeHTTPAction {
		return fmt.Errorf("httpAction cannot be invoked from nested calls")
	}
	return nil
}
