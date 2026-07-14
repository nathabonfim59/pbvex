package runtime

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/dop251/goja"
)

// Wire values are the JSON representation used by the HTTP protocol.  Keeping
// this conversion at the Goja boundary is important: JSON itself cannot carry
// either BigInt or ArrayBuffer without losing their type.
const maxWireDepth = 128

const (
	maxWireNodes   = 16 * 1024
	maxWireBytes   = 4 << 20
	maxWireEntries = 1024
)

// WireLimitError distinguishes adversarial resource-limit failures from a
// malformed ordinary value. Callers intentionally turn both into the same
// structured public error, but the type keeps tests and hosts from treating a
// rejected sparse array as an internal Go failure.
type WireLimitError struct{ Limit string }

func (e *WireLimitError) Error() string { return "wire " + e.Limit + " exceeds limit" }

type wireEncodeState struct {
	nodes int
	bytes int
	seen  map[*goja.Object]bool
}

func (s *wireEncodeState) node() error {
	s.nodes++
	if s.nodes > maxWireNodes {
		return &WireLimitError{Limit: "node count"}
	}
	return nil
}

func (s *wireEncodeState) addBytes(n int) error {
	if n < 0 || n > maxWireBytes-s.bytes {
		return &WireLimitError{Limit: "byte size"}
	}
	s.bytes += n
	return nil
}

func decodeWire(vm *goja.Runtime, value any) (goja.Value, error) {
	return decodeWireAt(vm, value, 0)
}

func decodeWireAt(vm *goja.Runtime, value any, depth int) (goja.Value, error) {
	if depth > maxWireDepth {
		return nil, fmt.Errorf("wire value depth exceeded")
	}
	switch v := value.(type) {
	case nil, bool, string:
		return vm.ToValue(v), nil
	case int:
		return vm.ToValue(float64(v)), nil
	case int64:
		return vm.ToValue(float64(v)), nil
	case int32:
		return vm.ToValue(float64(v)), nil
	case uint32:
		return vm.ToValue(float64(v)), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("non-finite wire number")
		}
		return vm.ToValue(v), nil
	case float32:
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("non-finite wire number")
		}
		return vm.ToValue(v), nil
	case int8:
		return vm.ToValue(int64(v)), nil
	case int16:
		return vm.ToValue(int64(v)), nil
	case uint:
		return vm.ToValue(int64(v)), nil
	case uint8:
		return vm.ToValue(int64(v)), nil
	case uint16:
		return vm.ToValue(int64(v)), nil
	case uint64:
		if v > 1<<63-1 {
			return nil, fmt.Errorf("uint64 too large")
		}
		return vm.ToValue(int64(v)), nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return vm.ToValue(i), nil
		}
		if f, err := v.Float64(); err == nil {
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, fmt.Errorf("non-finite wire number")
			}
			return vm.ToValue(f), nil
		}
		return nil, fmt.Errorf("invalid json.Number")
	case []any:
		a := vm.NewArray()
		for i, x := range v {
			item, err := decodeWireAt(vm, x, depth+1)
			if err != nil {
				return nil, err
			}
			a.Set(strconvI(i), item)
		}
		return a, nil
	case map[string]any:
		if len(v) == 1 {
			if raw, ok := v["$integer"]; ok {
				s, ok := raw.(string)
				if !ok {
					return nil, fmt.Errorf("malformed $integer")
				}
				b, err := base64.StdEncoding.DecodeString(s)
				if err != nil || len(b) != 8 {
					return nil, fmt.Errorf("malformed $integer")
				}
				u := new(big.Int).SetBytes(reverseCopy(b))
				if b[7]&0x80 != 0 {
					u.Sub(u, new(big.Int).Lsh(big.NewInt(1), 64))
				}
				return vm.ToValue(u), nil
			}
			if raw, ok := v["$bytes"]; ok {
				s, ok := raw.(string)
				if !ok {
					return nil, fmt.Errorf("malformed $bytes")
				}
				b, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return nil, fmt.Errorf("malformed $bytes")
				}
				return vm.ToValue(vm.NewArrayBuffer(b)), nil
			}
		}
		o := vm.NewObject()
		for k, x := range v {
			if !safeWireKey(k) {
				return nil, fmt.Errorf("invalid wire field %q", k)
			}
			item, err := decodeWireAt(vm, x, depth+1)
			if err != nil {
				return nil, err
			}
			if err := o.Set(k, item); err != nil {
				return nil, err
			}
		}
		return o, nil
	default:
		return nil, fmt.Errorf("unsupported wire value %T", value)
	}
}

func encodeWire(vm *goja.Runtime, value goja.Value) (any, error) {
	return encodeWireAt(vm, value, 0, true, &wireEncodeState{seen: make(map[*goja.Object]bool)})
}

func encodeWireAt(vm *goja.Runtime, value goja.Value, depth int, root bool, state *wireEncodeState) (any, error) {
	if depth > maxWireDepth {
		return nil, fmt.Errorf("wire value depth exceeded")
	}
	if state == nil {
		return nil, fmt.Errorf("invalid wire state")
	}
	if err := state.node(); err != nil {
		return nil, err
	}
	if value == nil || goja.IsNull(value) {
		return nil, nil
	}
	if goja.IsUndefined(value) {
		if root {
			return nil, nil
		}
		return nil, fmt.Errorf("undefined is not a wire value")
	}
	// Exporting a Goja Array eagerly materializes it as a Go slice. Check the
	// object class and array length first so `a.length = 1e9` cannot allocate
	// before the wire limits below have a chance to reject it.
	if o, ok := value.(*goja.Object); ok {
		return encodeWireObject(vm, o, depth, state)
	}
	switch v := value.Export().(type) {
	case bool:
		return v, nil
	case string:
		if err := state.addBytes(len(v)); err != nil {
			return nil, err
		}
		return v, nil
	case int64, int32, int, uint32:
		return v, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("non-finite wire number")
		}
		return v, nil
	case *big.Int:
		if v.Cmp(big.NewInt(-1<<63)) < 0 || v.Cmp(big.NewInt(1<<63-1)) > 0 {
			return nil, fmt.Errorf("bigint out of int64 range")
		}
		encoded := base64.StdEncoding.EncodeToString(int64LE(v.Int64()))
		if err := state.addBytes(len(encoded)); err != nil {
			return nil, err
		}
		return map[string]any{"$integer": encoded}, nil
	}
	return nil, fmt.Errorf("unsupported wire value %T", value.Export())
}

func encodeWireObject(vm *goja.Runtime, o *goja.Object, depth int, state *wireEncodeState) (any, error) {
	if state.seen[o] {
		return nil, fmt.Errorf("cyclic wire value")
	}
	state.seen[o] = true
	defer delete(state.seen, o)
	if o.ClassName() == "Array" {
		length := o.Get("length").ToInteger()
		if length < 0 || length > maxWireEntries {
			return nil, &WireLimitError{Limit: "array length"}
		}
		// Check sparsity before allocating an output slice. In particular a
		// JavaScript `a.length = 1e9` is rejected here without a Go allocation.
		names := o.GetOwnPropertyNames()
		if len(names) > maxWireEntries {
			return nil, &WireLimitError{Limit: "array property count"}
		}
		present := make(map[string]struct{}, len(names))
		for _, name := range names {
			present[name] = struct{}{}
		}
		for i := int64(0); i < length; i++ {
			if _, ok := present[strconvI(int(i))]; !ok {
				return nil, fmt.Errorf("sparse wire array")
			}
		}
		out := make([]any, int(length))
		for i := range out {
			key := strconvI(i)
			x, err := encodeWireAt(vm, o.Get(key), depth+1, false, state)
			if err != nil {
				return nil, err
			}
			out[i] = x
		}
		return out, nil
	}
	if o.ClassName() == "ArrayBuffer" {
		ab, ok := o.Export().(goja.ArrayBuffer)
		if !ok {
			return nil, fmt.Errorf("invalid ArrayBuffer")
		}
		bytes := ab.Bytes()
		if err := state.addBytes(base64.StdEncoding.EncodedLen(len(bytes))); err != nil {
			return nil, err
		}
		return map[string]any{"$bytes": base64.StdEncoding.EncodeToString(bytes)}, nil
	}
	// ArrayBuffer carries the generic "Object" class name in this runtime, so
	// resolve it from the exported value. Array is handled above and returns
	// eagerly, so exporting here never materializes an attacker-sized slice.
	if ab, ok := o.Export().(goja.ArrayBuffer); ok {
		bytes := ab.Bytes()
		if err := state.addBytes(base64.StdEncoding.EncodedLen(len(bytes))); err != nil {
			return nil, err
		}
		return map[string]any{"$bytes": base64.StdEncoding.EncodeToString(bytes)}, nil
	}
	proto := o.Prototype()
	objectPrototype := vm.Get("Object").ToObject(vm).Get("prototype").ToObject(vm)
	if proto != nil && proto != objectPrototype {
		return nil, fmt.Errorf("unsupported object prototype")
	}
	keys := o.Keys()
	if len(keys) > maxWireEntries {
		return nil, &WireLimitError{Limit: "object field count"}
	}
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		if !safeWireKey(k) {
			return nil, fmt.Errorf("invalid wire field %q", k)
		}
		if err := state.addBytes(len(k)); err != nil {
			return nil, err
		}
		x := o.Get(k)
		if goja.IsUndefined(x) {
			continue
		}
		encoded, err := encodeWireAt(vm, x, depth+1, false, state)
		if err != nil {
			return nil, err
		}
		out[k] = encoded
	}
	return out, nil
}

func safeWireKey(k string) bool {
	if k == "" || len(k) > 1024 || strings.HasPrefix(k, "$") || k == "__proto__" || k == "constructor" || k == "prototype" {
		return false
	}
	for _, r := range k {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func reverseCopy(b []byte) []byte {
	out := append([]byte(nil), b...)
	for i := 0; i < len(out)/2; i++ {
		out[i], out[len(out)-1-i] = out[len(out)-1-i], out[i]
	}
	return out
}
func int64LE(v int64) []byte {
	out := make([]byte, 8)
	u := uint64(v)
	for i := range out {
		out[i] = byte(u)
		u >>= 8
	}
	return out
}
func strconvI(i int) string { return fmt.Sprintf("%d", i) }
