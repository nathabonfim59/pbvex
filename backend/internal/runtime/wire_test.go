package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
)

func TestWireCodecFixtureVectorsAtRuntimeBoundary(t *testing.T) {
	d := deploy.FunctionDescriptor{Name: "roundtrip", Type: deploy.FunctionTypeQuery, Visibility: deploy.FunctionVisibilityPublic, ModulePath: "x.js", ExportName: "roundtrip"}
	bundle := `(function(){__pbvex.registerFunction({name:"roundtrip",type:"query",visibility:"public",modulePath:"x.js",exportName:"roundtrip"}, async function(ctx,args) { if (arguments.length !== 2) throw new Error("wrong arity"); return {integer:args.integer, bytes:args.bytes}; }); })();`
	m := NewManager(DefaultConfig())
	if err := m.Compile("test", bundle, []deploy.FunctionDescriptor{d}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Invoke(context.Background(), "test", "roundtrip", map[string]any{"integer": map[string]any{"$integer": "AQAAAAAAAAA="}, "bytes": map[string]any{"$bytes": "AP9C"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"integer": map[string]any{"$integer": "AQAAAAAAAAA="}, "bytes": map[string]any{"$bytes": "AP9C"}}
	if !descriptorsValueEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestWireCodecGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile("../../../fixtures/codec/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Roundtrip []struct {
			Label string `json:"label"`
			Value any    `json:"value"`
		} `json:"roundtrip"`
		InvalidDecode []struct {
			Label string `json:"label"`
			Value any    `json:"value"`
		} `json:"invalidDecode"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, vector := range fixture.Roundtrip {
		t.Run(vector.Label, func(t *testing.T) {
			vm := newEntry(nil, nil, nil, "", nil).vm
			decoded, err := decodeWire(vm, vector.Value)
			if err != nil {
				t.Fatal(err)
			}
			got, err := encodeWire(vm, decoded)
			if err != nil {
				t.Fatal(err)
			}
			if !descriptorsValueEqual(got, vector.Value) {
				t.Fatalf("got %#v want %#v", got, vector.Value)
			}
		})
	}
	for _, vector := range fixture.InvalidDecode {
		t.Run("invalid_"+vector.Label, func(t *testing.T) {
			if _, err := decodeWire(newEntry(nil, nil, nil, "", nil).vm, vector.Value); err == nil {
				t.Fatal("expected invalid vector error")
			}
		})
	}
}

func TestWireCodecRejectsUnsafeAndCyclicReturns(t *testing.T) {
	vm := newEntry(nil, nil, nil, "", nil).vm
	for _, source := range []string{`({"$bad":1})`, `(()=>{const x={};x.self=x;return x})()`} {
		v, err := vm.RunString(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := encodeWire(vm, v); err == nil {
			t.Fatalf("%s: expected error", source)
		}
	}
}

func TestWireCodecLimitsBeforeArrayAllocation(t *testing.T) {
	vm := newEntry(nil, nil, nil, "", nil).vm
	for name, source := range map[string]string{
		"sparse billion":         `(()=>{const a=[]; a.length=1000000000; return a})()`,
		"too many object fields": `(()=>{const o={}; for(let i=0;i<1025;i++) o["k"+i]=i; return o})()`,
		"oversized bytes":        `new Uint8Array(4 * 1024 * 1024).buffer`,
	} {
		t.Run(name, func(t *testing.T) {
			value, err := vm.RunString(source)
			if err != nil {
				t.Fatal(err)
			}
			_, err = encodeWire(vm, value)
			var limit *WireLimitError
			if !errors.As(err, &limit) {
				t.Fatalf("expected typed wire limit error, got %v", err)
			}
		})
	}
}

// TestWireCodecEncodesEmptyAndByteBackedValues guards two regressions: an empty
// JS array must encode to an empty Go slice (not [0]), and ArrayBuffer values
// reached through typed arrays resolve through the exported value because their
// class name is the generic "Object" in this runtime.
func TestWireCodecEncodesEmptyAndByteBackedValues(t *testing.T) {
	vm := newEntry(nil, nil, nil, "", nil).vm
	for name, tc := range map[string]struct {
		source string
		want   any
	}{
		"empty array":          {`[]`, []any{}},
		"empty nested array":   {`[[]]`, []any{[]any{}}},
		"uint8 array buffer":   {`new Uint8Array([0, 255]).buffer`, map[string]any{"$bytes": "AP8="}},
		"array buffer literal": {`new ArrayBuffer(2)`, map[string]any{"$bytes": "AAA="}},
	} {
		t.Run(name, func(t *testing.T) {
			value, err := vm.RunString(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := encodeWire(vm, value)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if !descriptorsValueEqual(got, tc.want) {
				t.Fatalf("got %#v want %#v", got, tc.want)
			}
		})
	}
}

func descriptorsValueEqual(a, b any) bool {
	// JSON-shaped fixture values only; this keeps the assertion independent of
	// map iteration order.
	return canonical(a) == canonical(b)
}

func canonical(v any) string {
	s, err := deploy.CanonicalJSON(v)
	if err != nil {
		panic(err)
	}
	return s
}
