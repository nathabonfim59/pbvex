package runtime

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/dop251/goja"
	_ "github.com/dop251/goja_nodejs/url"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

//go:embed web_globals.js
var webGlobalsJS string

func (e *entry) loadWebGlobals(vm *goja.Runtime) error {
	consoleObj := vm.NewObject()
	_ = consoleObj.Set("log", e.consoleLog)
	_ = consoleObj.Set("error", e.consoleError)
	_ = consoleObj.Set("warn", e.consoleWarn)
	_ = consoleObj.Set("info", e.consoleInfo)
	_ = consoleObj.Set("debug", e.consoleDebug)
	_ = vm.Set("console", consoleObj)

	_ = vm.Set("__textEncoderEncode", e.textEncoderEncode)
	_ = vm.Set("__textDecoderDecode", e.textDecoderDecode)
	_ = vm.Set("__pbvexHeaderLimits", map[string]int{
		"count":      deploy.MaxHTTPHeaderCount,
		"nameBytes":  deploy.MaxHTTPHeaderNameBytes,
		"valueBytes": deploy.MaxHTTPHeaderValueBytes,
		"totalBytes": deploy.MaxHTTPHeadersBytes,
	})
	_ = vm.Set("__pbvexValidHeaderName", func(name string) bool {
		return deploy.ValidateHTTPHeaderName(name) == nil
	})
	_ = vm.Set("__pbvexValidHeaderValue", func(value string) bool {
		return deploy.ValidateHTTPHeaderValue(value) == nil
	})
	_ = vm.Set("__pbvexHeaderBytes", func(value string) int { return len(value) })

	uint8ArrayCtor, ok := goja.AssertConstructor(vm.Get("Uint8Array"))
	if !ok {
		return fmt.Errorf("Uint8Array is not a constructor")
	}
	e.uint8ArrayCtor = uint8ArrayCtor

	_, err := vm.RunString(webGlobalsJS)
	return err
}

func (e *entry) newUint8Array(data []byte) goja.Value {
	ab := e.vm.NewArrayBuffer(data)
	if e.uint8ArrayCtor == nil {
		panic(e.vm.NewTypeError("Uint8Array constructor is not available"))
	}
	obj, err := e.uint8ArrayCtor(nil, e.vm.ToValue(ab))
	if err != nil {
		panic(err)
	}
	return obj
}

func (e *entry) toBytes(v goja.Value) []byte {
	if v == nil || goja.IsNull(v) || goja.IsUndefined(v) {
		return nil
	}
	switch x := v.Export().(type) {
	case []byte:
		return x
	case goja.ArrayBuffer:
		return x.Bytes()
	}
	return nil
}

func (e *entry) textEncoderEncode(s string) goja.Value {
	return e.newUint8Array([]byte(s))
}

func (e *entry) textDecoderDecode(v goja.Value) string {
	return string(e.toBytes(v))
}

func (e *entry) consoleLog(call goja.FunctionCall) goja.Value {
	return e.consolePrint(os.Stdout, call.Arguments)
}

func (e *entry) consoleError(call goja.FunctionCall) goja.Value {
	return e.consolePrint(os.Stderr, call.Arguments)
}

func (e *entry) consoleWarn(call goja.FunctionCall) goja.Value {
	return e.consolePrint(os.Stderr, call.Arguments)
}

func (e *entry) consoleInfo(call goja.FunctionCall) goja.Value {
	return e.consolePrint(os.Stdout, call.Arguments)
}

func (e *entry) consoleDebug(call goja.FunctionCall) goja.Value {
	return e.consolePrint(os.Stdout, call.Arguments)
}

func (e *entry) consolePrint(w *os.File, args []goja.Value) goja.Value {
	exported := make([]interface{}, len(args))
	for i, a := range args {
		exported[i] = a.Export()
	}
	_, _ = fmt.Fprintln(w, exported...)
	return goja.Undefined()
}
