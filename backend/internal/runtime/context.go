package runtime

import (
	"fmt"
	"strings"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

// makeAuthObject creates the Goja object exposed as ctx.auth. It contains a
// single getUserIdentity method that returns a Promise resolving to the
// authenticated UserIdentity or null without leaking the underlying PocketBase
// record.
func (e *entry) makeAuthObject(identity *auth.UserIdentity) goja.Value {
	authObj := e.vm.NewObject()
	getUserIdentity := func() goja.Value {
		m := auth.ToMap(identity)
		var v goja.Value
		if m == nil {
			v = goja.Null()
		} else {
			v = e.vm.ToValue(m)
		}
		p, err := e.promiseResolve(e.promiseCtor, v)
		if err != nil {
			panic(err)
		}
		return p
	}
	_ = authObj.Set("getUserIdentity", getUserIdentity)
	return authObj
}

// makeRunQuery creates a ctx.runQuery closure bound to the provided invocation
// depth. It returns a Goja callable.
func (e *entry) makeRunQuery(depth int) goja.Value {
	return e.vm.ToValue(func(name goja.Value, args goja.Value) goja.Value {
		return e.runNested(name, args, deploy.FunctionTypeQuery, depth)
	})
}

// makeRunMutation creates a ctx.runMutation closure.
func (e *entry) makeRunMutation(depth int) goja.Value {
	return e.vm.ToValue(func(name goja.Value, args goja.Value) goja.Value {
		return e.runNested(name, args, deploy.FunctionTypeMutation, depth)
	})
}

// makeRunAction creates a ctx.runAction closure.
func (e *entry) makeRunAction(depth int) goja.Value {
	return e.vm.ToValue(func(name goja.Value, args goja.Value) goja.Value {
		return e.runNested(name, args, deploy.FunctionTypeAction, depth)
	})
}

// makeRun preserves the component API's generic ctx.run(ref, args) while
// dispatching through the same fresh-entry path as the typed auth APIs.
func (e *entry) makeRun(depth int) goja.Value {
	return e.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 || len(call.Arguments) > 2 {
			panic(e.vm.NewTypeError("run: expected a function reference and optional args"))
		}
		descriptor, ok := e.descriptorFromReference(call.Argument(0))
		if !ok {
			panic(e.vm.NewTypeError("function reference is not registered"))
		}
		args := goja.Undefined()
		if len(call.Arguments) == 2 {
			args = call.Argument(1)
		}
		return e.runNested(e.vm.ToValue(descriptor.Name), args, descriptor.Type, depth)
	})
}

func (e *entry) descriptorFromReference(value goja.Value) (deploy.FunctionDescriptor, bool) {
	name := functionNameFromValue(value)
	if descriptor, ok := e.bridge.descriptors[name]; ok {
		return descriptor, true
	}
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return deploy.FunctionDescriptor{}, false
	}
	exportName := parts[len(parts)-1]
	modulePath := "pbvex/" + strings.Join(parts[:len(parts)-1], "/") + ".ts"
	if parts[0] == "components" && len(parts) >= 4 {
		modulePath = "pbvex/components/" + strings.Join(parts[1:len(parts)-1], "/") + ".ts"
	}
	for _, descriptor := range e.bridge.descriptors {
		if descriptor.ModulePath == modulePath && descriptor.ExportName == exportName {
			return descriptor, true
		}
	}
	return deploy.FunctionDescriptor{}, false
}

// runNested serializes values at the parent VM boundary and delegates the call
// to the pool, which executes it in a fresh runtime entry.
func (e *entry) runNested(nameValue goja.Value, argsValue goja.Value, targetType deploy.FunctionType, depth int) goja.Value {
	inv := e.invocation
	if inv == nil {
		panic(e.vm.NewTypeError("invocation is not set"))
	}

	name := functionNameFromValue(nameValue)
	if name == "" {
		panic(e.vm.NewTypeError("function name is required"))
	}

	// canRunNested charges the shared, nonrefundable work counter. Failed
	// attempts still count so that over-budget probing is not possible.
	if err := inv.canRunNested(targetType, depth); err != nil {
		panic(e.vm.NewTypeError(err.Error()))
	}

	if inv.NestedInvoke == nil {
		panic(e.vm.NewTypeError("nested invocation is unavailable"))
	}

	// Normalize and size-limit args by round-tripping through the wire codec.
	encoded, err := encodeWire(e.vm, argsValue)
	if err != nil {
		panic(e.vm.NewTypeError("invalid nested function arguments: %s", err.Error()))
	}
	if err := checkWireSize(encoded, inv.MaxArgsBytes, "nested function arguments"); err != nil {
		panic(e.vm.NewTypeError("%s", err.Error()))
	}
	result, err := inv.NestedInvoke(inv, name, targetType, encoded, depth)
	if err != nil {
		panic(e.vm.NewGoError(err))
	}
	val, err := decodeWire(e.vm, result)
	if err != nil {
		panic(e.vm.NewGoError(fmt.Errorf("invalid nested function result: %w", err)))
	}
	p, err := e.promiseResolve(e.promiseCtor, val)
	if err != nil {
		panic(err)
	}
	return p
}

func functionNameFromValue(v goja.Value) string {
	if v == nil {
		return ""
	}
	if s, ok := v.Export().(string); ok {
		return s
	}
	if o, ok := v.(*goja.Object); ok {
		if name := o.Get("_name"); name != nil {
			if s, ok := name.Export().(string); ok {
				return s
			}
		}
		if path := o.Get("_path"); path != nil {
			if s, ok := path.Export().(string); ok {
				return s
			}
		}
	}
	return v.String()
}

func checkWireSize(value any, limit int64, label string) error {
	canonical, err := deploy.CanonicalJSON(value)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", label, err)
	}
	if int64(len(canonical)) > limit {
		return &deploy.ValueSizeError{Label: label, Limit: limit}
	}
	return nil
}
