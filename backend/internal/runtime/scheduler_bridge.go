package runtime

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

func (e *entry) parseDelay(v goja.Value) (int64, error) {
	if goja.IsUndefined(v) || goja.IsNull(v) {
		return 0, fmt.Errorf("runAfter: delayMs required")
	}
	f := v.ToFloat()
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, fmt.Errorf("runAfter: delayMs must be an integer")
	}
	if f < 0 {
		return 0, fmt.Errorf("runAfter: delayMs must be non-negative")
	}
	const maxDelayMs = int64(5 * 365 * 24 * time.Hour / time.Millisecond)
	if f > float64(maxDelayMs) {
		return 0, fmt.Errorf("runAfter: delayMs out of range")
	}
	return v.ToInteger(), nil
}

func (e *entry) parseRunAt(v goja.Value) (int64, error) {
	if goja.IsUndefined(v) || goja.IsNull(v) {
		return 0, fmt.Errorf("runAt: timestamp required")
	}
	if t, ok := v.Export().(time.Time); ok {
		return t.UnixMilli(), nil
	}
	f := v.ToFloat()
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, fmt.Errorf("runAt: timestamp must be an integer")
	}
	return v.ToInteger(), nil
}

func (e *entry) bindScheduler(ctxObj *goja.Object, invocation *Invocation) error {
	owner := invocation.Namespace
	if owner == "" {
		owner = deploy.RootNamespace
	}
	ctx := invocation.Ctx
	schedulerObj := e.vm.NewObject()
	_ = schedulerObj.Set("runAfter", e.wrapSchedulerRunAfter(ctx, invocation.Manifest, owner))
	_ = schedulerObj.Set("runAt", e.wrapSchedulerRunAt(ctx, invocation.Manifest, owner))
	_ = schedulerObj.Set("cancel", e.wrapSchedulerCancel(WithScheduleNamespaces(ctx, owner, "")))
	return ctxObj.Set("scheduler", schedulerObj)
}

func (e *entry) resolveFunctionRef(ref goja.Value) (string, error) {
	if goja.IsUndefined(ref) || goja.IsNull(ref) {
		return "", fmt.Errorf("function reference required")
	}
	obj, ok := ref.Export().(map[string]any)
	if !ok {
		return "", fmt.Errorf("function reference must be an object")
	}
	path, _ := obj["_path"].(string)
	typ, _ := obj["_type"].(string)
	visibility, _ := obj["_visibility"].(string)
	desc, ok := e.bridge.descriptorsByPath[path]
	if path == "" || typ == "" || visibility == "" || !ok {
		return "", fmt.Errorf("invalid function reference")
	}
	if string(desc.Type) != typ || string(desc.Visibility) != visibility {
		return "", fmt.Errorf("function reference mismatch")
	}
	if desc.Type != deploy.FunctionTypeMutation && desc.Type != deploy.FunctionTypeAction {
		return "", fmt.Errorf("function is not schedulable")
	}
	return desc.Name, nil
}

func (e *entry) scheduledArgs(v goja.Value) (any, error) {
	if v == nil || goja.IsUndefined(v) {
		return map[string]any{}, nil
	}
	return encodeWire(e.vm, v)
}

func (e *entry) wrapSchedulerRunAfter(ctx context.Context, manifest deploy.DeploymentManifest, owner string) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 || len(call.Arguments) > 3 {
			panic(e.vm.NewTypeError("runAfter: expected 2 or 3 arguments"))
		}
		delay, err := e.parseDelay(call.Argument(0))
		if err != nil {
			panic(e.vm.NewTypeError(err.Error()))
		}
		name, err := e.resolveFunctionRef(call.Argument(1))
		if err != nil {
			panic(e.vm.NewTypeError(err.Error()))
		}
		args, err := e.scheduledArgs(call.Argument(2))
		if err != nil {
			panic(e.vm.NewTypeError(err.Error()))
		}
		target := deploy.RootNamespace
		if descriptor, ok := e.bridge.descriptors[name]; ok {
			target = namespaceForDescriptor(manifest, descriptor)
		}
		id, err := e.scheduler.RunAfter(WithScheduleNamespaces(ctx, owner, target), delay, e.deploymentID, name, args)
		if err != nil {
			panic(e.vm.NewTypeError(err.Error()))
		}
		return e.vm.ToValue(id)
	}
}

func (e *entry) wrapSchedulerRunAt(ctx context.Context, manifest deploy.DeploymentManifest, owner string) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 || len(call.Arguments) > 3 {
			panic(e.vm.NewTypeError("runAt: expected 2 or 3 arguments"))
		}
		ts, err := e.parseRunAt(call.Argument(0))
		if err != nil {
			panic(e.vm.NewTypeError(err.Error()))
		}
		name, err := e.resolveFunctionRef(call.Argument(1))
		if err != nil {
			panic(e.vm.NewTypeError(err.Error()))
		}
		args, err := e.scheduledArgs(call.Argument(2))
		if err != nil {
			panic(e.vm.NewTypeError(err.Error()))
		}
		target := deploy.RootNamespace
		if descriptor, ok := e.bridge.descriptors[name]; ok {
			target = namespaceForDescriptor(manifest, descriptor)
		}
		id, err := e.scheduler.RunAt(WithScheduleNamespaces(ctx, owner, target), ts, e.deploymentID, name, args)
		if err != nil {
			panic(e.vm.NewTypeError(err.Error()))
		}
		return e.vm.ToValue(id)
	}
}

func (e *entry) wrapSchedulerCancel(ctx context.Context) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) != 1 {
			panic(e.vm.NewTypeError("cancel: expected 1 argument"))
		}
		if err := e.scheduler.Cancel(ctx, call.Argument(0).String()); err != nil {
			panic(e.vm.NewTypeError(err.Error()))
		}
		return goja.Undefined()
	}
}
