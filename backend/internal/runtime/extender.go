package runtime

import (
	"context"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/pocketbase/pocketbase/core"
)

// ContextExtender extends the JS invocation context with host capabilities
// (such as storage and auth) that live outside the runtime package. It is
// invoked after the runtime builds the base context object so the registrar can
// attach services without the runtime depending on them directly.
type ContextExtender func(vm *goja.Runtime, ctx context.Context, app core.App, fd deploy.FunctionDescriptor, obj *goja.Object) error

// AddContextExtender appends a host capability hook. Hooks run in registration
// order after the runtime has installed its base database capability and before
// the scheduler capability is attached. Existing pools are dropped so a hook
// registered after compilation cannot be silently omitted.
func (m *Manager) AddContextExtender(ext ContextExtender) {
	if ext == nil {
		return
	}
	m.mu.Lock()
	m.extenders = append(m.extenders, ext)
	m.pools = make(map[string]*Pool)
	m.mu.Unlock()
}
