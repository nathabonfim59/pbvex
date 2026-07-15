package schema

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestRecursiveDescriptorValidation(t *testing.T) {
	// A self-referential Tree: { name: string, children: Tree[] }.
	tree := map[string]any{
		"type": "recursive", "name": "Tree",
		"validator": map[string]any{
			"type": "object", "shape": map[string]any{
				"name":     map[string]any{"type": "string"},
				"children": map[string]any{"type": "array", "item": map[string]any{"type": "ref", "name": "Tree"}},
			},
		},
	}
	if !ValidateDescriptor(tree) {
		t.Fatal("expected recursive descriptor to be valid")
	}

	invalid := []map[string]any{
		// bare recursive marker with no name/target identity
		{"type": "recursive"},
		// recursive missing inner validator
		{"type": "recursive", "name": "Tree"},
		// recursive with a non-identifier name
		{"type": "recursive", "name": "1bad", "validator": map[string]any{"type": "string"}},
		// ref to an undeclared name
		{"type": "ref", "name": "Missing"},
		// ref carrying extra keys
		{"type": "ref", "name": "Tree", "extra": 1},
		// recursive whose inner references an undeclared name
		{"type": "recursive", "name": "Tree", "validator": map[string]any{
			"type": "object", "shape": map[string]any{"x": map[string]any{"type": "ref", "name": "Other"}},
		}},
	}
	for _, d := range invalid {
		if ValidateDescriptor(d) {
			t.Fatalf("expected invalid recursive descriptor %#v", d)
		}
	}
}

func TestRecursiveValueNormalization(t *testing.T) {
	tree := map[string]any{
		"type": "recursive", "name": "Tree",
		"validator": map[string]any{
			"type": "object", "shape": map[string]any{
				"name":     map[string]any{"type": "string"},
				"children": map[string]any{"type": "array", "item": map[string]any{"type": "ref", "name": "Tree"}},
			},
		},
	}

	// Valid nested data: a root with two children, one of which has its own child.
	valid := map[string]any{
		"name": "root",
		"children": []any{
			map[string]any{"name": "a", "children": []any{}},
			map[string]any{"name": "b", "children": []any{
				map[string]any{"name": "c", "children": []any{}},
			}},
		},
	}
	out, err := NormalizeValue(tree, valid, nil)
	if err != nil {
		t.Fatalf("expected valid recursive value to normalize: %v", err)
	}
	root := out.(map[string]any)
	if root["name"] != "root" {
		t.Fatalf("expected root name preserved, got %v", root["name"])
	}
	children := root["children"].([]any)
	if len(children) != 2 || children[1].(map[string]any)["name"] != "b" {
		t.Fatalf("expected nested children preserved, got %v", children)
	}

	// Invalid: a child missing the required `name` field.
	bad := map[string]any{
		"name":     "root",
		"children": []any{map[string]any{"children": []any{}}},
	}
	if _, err := NormalizeValue(tree, bad, nil); err == nil {
		t.Fatal("expected invalid recursive value to be rejected")
	}

	// Invalid: wrong field type at a nested level.
	badType := map[string]any{
		"name":     "root",
		"children": []any{map[string]any{"name": 123, "children": []any{}}},
	}
	if _, err := NormalizeValue(tree, badType, nil); err == nil {
		t.Fatal("expected wrong nested type to be rejected")
	}
}

func TestRecursiveDocumentInsertPatch(t *testing.T) {
	// A table whose `tree` column is a recursive Tree descriptor.
	treeField := map[string]any{
		"type": "recursive", "name": "Tree",
		"validator": map[string]any{
			"type": "object", "shape": map[string]any{
				"name":     map[string]any{"type": "string"},
				"children": map[string]any{"type": "array", "item": map[string]any{"type": "ref", "name": "Tree"}},
			},
		},
	}
	fields := map[string]any{"tree": treeField}

	insert := map[string]any{
		"tree": map[string]any{
			"name": "root", "children": []any{
				map[string]any{"name": "leaf", "children": []any{}},
			},
		},
	}
	doc, err := NormalizeDocument(fields, insert, false, true, nil)
	if err != nil {
		t.Fatalf("expected recursive document insert to validate: %v", err)
	}
	if doc["tree"].(map[string]any)["name"] != "root" {
		t.Fatalf("expected nested tree preserved, got %v", doc["tree"])
	}

	// Patch mode: a partial recursive value must still validate against the schema.
	patch := map[string]any{
		"tree": map[string]any{"name": "patched", "children": []any{}},
	}
	if _, err := NormalizeDocument(fields, patch, true, true, nil); err != nil {
		t.Fatalf("expected recursive patch to validate: %v", err)
	}

	// Invalid insert: nested child missing required field.
	badInsert := map[string]any{
		"tree": map[string]any{"name": "root", "children": []any{map[string]any{"children": []any{}}}},
	}
	if _, err := NormalizeDocument(fields, badInsert, false, true, nil); err == nil {
		t.Fatal("expected invalid recursive insert to be rejected")
	}
}

// TestRecursiveDefaultResolvesRefs proves a default nested in a recursive body
// resolves refs against the enclosing recursive declaration. The default's
// child is {type:'ref', name:'Node'}; without the current definitions context
// (the old top-level NormalizeValue call had an empty map) the ref would be
// rejected as undeclared. optional(defaulted(...)) keeps the default finite.
func TestRecursiveDefaultResolvesRefs(t *testing.T) {
	node := map[string]any{
		"type": "recursive", "name": "Node",
		"validator": map[string]any{
			"type": "object", "shape": map[string]any{
				"name":     map[string]any{"type": "string"},
				"children": map[string]any{"type": "array", "item": map[string]any{"type": "ref", "name": "Node"}},
				"fallback": map[string]any{
					"type": "optional", "validator": map[string]any{
						"type":         "defaulted",
						"validator":    map[string]any{"type": "ref", "name": "Node"},
						"defaultValue": map[string]any{"name": "seed", "children": []any{}},
					},
				},
			},
		},
	}
	if !ValidateDescriptor(node) {
		t.Fatal("expected recursive descriptor with a ref-resolving default to be valid")
	}
	// A value that supplies `fallback` explicitly normalizes through the ref.
	valid := map[string]any{
		"name": "root", "children": []any{},
		"fallback": map[string]any{"name": "supplied", "children": []any{}},
	}
	out, err := NormalizeValue(node, valid, nil)
	if err != nil {
		t.Fatalf("expected recursive value with explicit fallback to normalize: %v", err)
	}
	if out.(map[string]any)["fallback"].(map[string]any)["name"] != "supplied" {
		t.Fatalf("expected fallback preserved, got %v", out)
	}
	// A default declared against an undeclared ref name is still rejected.
	bad := map[string]any{
		"type": "recursive", "name": "Node",
		"validator": map[string]any{
			"type": "object", "shape": map[string]any{
				"x": map[string]any{"type": "defaulted", "validator": map[string]any{"type": "ref", "name": "Missing"}, "defaultValue": map[string]any{}},
			},
		},
	}
	if ValidateDescriptor(bad) {
		t.Fatal("expected defaulted ref to an undeclared name to be rejected")
	}
}

// TestRecursiveNonOptionalDefault proves a non-optional defaulted field inside
// a recursive object is applied when omitted — not bypassed through optional
// semantics. The `name` field is `defaulted(string, "anon")` (not wrapped in
// optional), and both root and a nested child omit it. The normalized result
// must contain "anon" at every level where the field was omitted.
func TestRecursiveNonOptionalDefault(t *testing.T) {
	node := map[string]any{
		"type": "recursive", "name": "Node",
		"validator": map[string]any{
			"type": "object", "shape": map[string]any{
				"name":     map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "anon"},
				"kind":     map[string]any{"type": "string"},
				"children": map[string]any{"type": "array", "item": map[string]any{"type": "ref", "name": "Node"}},
			},
		},
	}
	if !ValidateDescriptor(node) {
		t.Fatal("expected recursive descriptor with non-optional default to be valid")
	}
	// Both root and child omit `name` — the default must apply at every level.
	value := map[string]any{
		"kind": "root", "children": []any{
			map[string]any{"kind": "leaf", "children": []any{}},
		},
	}
	out, err := NormalizeValue(node, value, nil)
	if err != nil {
		t.Fatalf("expected recursive value with omitted defaults to normalize: %v", err)
	}
	root := out.(map[string]any)
	if root["name"] != "anon" {
		t.Fatalf("expected root defaulted name 'anon', got %#v", root["name"])
	}
	children := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	child := children[0].(map[string]any)
	if child["name"] != "anon" {
		t.Fatalf("expected child defaulted name 'anon', got %#v", child["name"])
	}
}

func TestDescriptorParityRecordKeys(t *testing.T) {
	valid := []map[string]any{
		{"type": "record", "key": map[string]any{"type": "string"}, "value": map[string]any{"type": "string"}},
		{"type": "record", "key": map[string]any{"type": "literal", "value": "foo"}, "value": map[string]any{"type": "string"}},
		{"type": "record", "key": map[string]any{"type": "union", "validators": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "literal", "value": "fixed"},
		}}, "value": map[string]any{"type": "number"}},
	}
	for _, d := range valid {
		if !ValidateDescriptor(d) {
			t.Fatalf("expected valid record key descriptor %#v", d)
		}
	}

	invalid := []map[string]any{
		{"type": "record", "key": map[string]any{"type": "number"}, "value": map[string]any{"type": "string"}},
		{"type": "record", "key": map[string]any{"type": "boolean"}, "value": map[string]any{"type": "string"}},
		{"type": "record", "key": map[string]any{"type": "literal", "value": 42}, "value": map[string]any{"type": "string"}},
		{"type": "record", "key": map[string]any{"type": "object", "shape": map[string]any{}}, "value": map[string]any{"type": "string"}},
	}
	for _, d := range invalid {
		if ValidateDescriptor(d) {
			t.Fatalf("expected invalid record key descriptor %#v", d)
		}
	}
}

func TestDescriptorParityObjectShapeFields(t *testing.T) {
	// Object with both shape and fields is rejected (len(o)!=2).
	bad := map[string]any{
		"type":   "object",
		"shape":  map[string]any{"a": map[string]any{"type": "string"}},
		"fields": map[string]any{"a": map[string]any{"type": "string"}},
	}
	if ValidateDescriptor(bad) {
		t.Fatal("expected object with both shape and fields to be rejected")
	}
}

func TestDescriptorParityUnionCap(t *testing.T) {
	branches := make([]any, 65)
	for i := range branches {
		branches[i] = map[string]any{"type": "string"}
	}
	bad := map[string]any{"type": "union", "validators": branches}
	if ValidateDescriptor(bad) {
		t.Fatal("expected union with 65 branches to be rejected")
	}

	branches64 := make([]any, 64)
	for i := range branches64 {
		branches64[i] = map[string]any{"type": "string"}
	}
	ok := map[string]any{"type": "union", "validators": branches64}
	if !ValidateDescriptor(ok) {
		t.Fatal("expected union with 64 branches to be valid")
	}
}

func TestDescriptorParityDefaultedNormalization(t *testing.T) {
	// Construct a structurally valid opaque ID for table "trees".
	payload := `{"v":1,"k":1,"n":"test","t":"trees","r":"abcdefghijklmno"}`
	payloadB64 := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := make([]byte, 32)
	macB64 := base64.RawURLEncoding.EncodeToString(mac)
	validID := "pbv1.1." + payloadB64 + "." + macB64

	// Construct an array with 1025 elements (above the cap).
	bigArray := make([]any, 1025)
	for i := range bigArray {
		bigArray[i] = float64(0)
	}

	// Construct a record with 1025 keys (above the cap).
	bigRecord := make(map[string]any, 1025)
	for i := 0; i < 1025; i++ {
		bigRecord[base64.RawURLEncoding.EncodeToString([]byte{byte(i), byte(i >> 8)})] = float64(0)
	}

	valid := []map[string]any{
		{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "x"},
		{"type": "defaulted", "validator": map[string]any{"type": "number"}, "defaultValue": float64(42)},
		{"type": "defaulted", "validator": map[string]any{"type": "boolean"}, "defaultValue": true},
		{"type": "defaulted", "validator": map[string]any{"type": "null"}, "defaultValue": nil},
		{"type": "defaulted", "validator": map[string]any{"type": "array", "item": map[string]any{"type": "number"}}, "defaultValue": []any{float64(1), float64(2)}},
		{"type": "defaulted", "validator": map[string]any{"type": "object", "shape": map[string]any{"a": map[string]any{"type": "string"}}}, "defaultValue": map[string]any{"a": "x"}},
		{"type": "defaulted", "validator": map[string]any{"type": "id", "tableName": "trees"}, "defaultValue": validID},
	}
	for _, d := range valid {
		if !ValidateDescriptor(d) {
			t.Fatalf("expected valid defaulted descriptor %#v", d)
		}
	}

	invalid := []map[string]any{
		{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": float64(42)},
		{"type": "defaulted", "validator": map[string]any{"type": "number"}, "defaultValue": "not-a-number"},
		{"type": "defaulted", "validator": map[string]any{"type": "boolean"}, "defaultValue": float64(1)},
		{"type": "defaulted", "validator": map[string]any{"type": "array", "item": map[string]any{"type": "number"}}, "defaultValue": []any{float64(1), "x"}},
		{"type": "defaulted", "validator": map[string]any{"type": "object", "shape": map[string]any{"a": map[string]any{"type": "string"}}}, "defaultValue": map[string]any{"a": float64(123)}},
		{"type": "defaulted", "validator": map[string]any{"type": "id", "tableName": "trees"}, "defaultValue": "not-an-id"},
		{"type": "defaulted", "validator": map[string]any{"type": "id", "tableName": "trees"}, "defaultValue": "pbv1.1.bad.bad"},
		{"type": "defaulted", "validator": map[string]any{"type": "array", "item": map[string]any{"type": "number"}}, "defaultValue": bigArray},
		{"type": "defaulted", "validator": map[string]any{"type": "record", "key": map[string]any{"type": "string"}, "value": map[string]any{"type": "number"}}, "defaultValue": bigRecord},
		{"type": "defaulted", "validator": map[string]any{"type": "record", "key": map[string]any{"type": "string"}, "value": map[string]any{"type": "number"}}, "defaultValue": map[string]any{"$bad": float64(0)}},
	}
	for _, d := range invalid {
		if ValidateDescriptor(d) {
			t.Fatalf("expected invalid defaulted descriptor %#v", d)
		}
	}
}

func TestOpaqueIDCanonicality(t *testing.T) {
	table := "trees"
	validPayload := `{"v":1,"k":1,"n":"test","t":"` + table + `","r":"abcdefghijklmno"}`
	validPayloadB64 := base64.RawURLEncoding.EncodeToString([]byte(validPayload))
	mac := make([]byte, 32)
	macB64 := base64.RawURLEncoding.EncodeToString(mac)
	validID := "pbv1.1." + validPayloadB64 + "." + macB64

	if !ValidateDescriptor(map[string]any{
		"type":         "defaulted",
		"validator":    map[string]any{"type": "id", "tableName": table},
		"defaultValue": validID,
	}) {
		t.Fatal("expected valid Go-issued opaque ID to be accepted")
	}

	makeID := func(payload string) string {
		return "pbv1.1." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + macB64
	}

	invalidCases := []struct {
		label string
		id    string
	}{
		{"unknown field", makeID(`{"v":1,"k":1,"n":"test","t":"` + table + `","r":"abcdefghijklmno","extra":1}`)},
		{"reordered keys", makeID(`{"r":"abcdefghijklmno","t":"` + table + `","n":"test","k":1,"v":1}`)},
		{"padded payload", "pbv1.1." + base64.URLEncoding.EncodeToString([]byte(validPayload)) + "." + macB64},
		{"extra JSON values", makeID(validPayload + `{"extra":1}`)},
		{"wrong table target", makeID(`{"v":1,"k":1,"n":"test","t":"other","r":"abcdefghijklmno"}`)},
	}
	for _, tc := range invalidCases {
		if ValidateDescriptor(map[string]any{
			"type":         "defaulted",
			"validator":    map[string]any{"type": "id", "tableName": table},
			"defaultValue": tc.id,
		}) {
			t.Fatalf("expected invalid opaque ID (%s) to be rejected: %s", tc.label, tc.id)
		}
	}
}

func TestDescriptorParityAnyBareObjectDefaults(t *testing.T) {
	bigArray := make([]any, 1025)
	for i := range bigArray {
		bigArray[i] = float64(0)
	}
	bigRecord := make(map[string]any, 1025)
	for i := 0; i < 1025; i++ {
		bigRecord[base64.RawURLEncoding.EncodeToString([]byte{byte(i), byte(i >> 8)})] = float64(0)
	}

	valid := []map[string]any{
		{"type": "defaulted", "validator": map[string]any{"type": "any"}, "defaultValue": map[string]any{"ok": float64(1)}},
		{"type": "defaulted", "validator": map[string]any{"type": "any"}, "defaultValue": []any{float64(1), float64(2)}},
		{"type": "defaulted", "validator": map[string]any{"type": "any"}, "defaultValue": "str"},
		{"type": "defaulted", "validator": map[string]any{"type": "object"}, "defaultValue": map[string]any{"ok": float64(1)}},
	}
	for _, d := range valid {
		if !ValidateDescriptor(d) {
			t.Fatalf("expected valid descriptor %#v", d)
		}
	}

	invalid := []map[string]any{
		{"type": "defaulted", "validator": map[string]any{"type": "any"}, "defaultValue": map[string]any{"$bad": float64(0)}},
		{"type": "defaulted", "validator": map[string]any{"type": "any"}, "defaultValue": bigArray},
		{"type": "defaulted", "validator": map[string]any{"type": "any"}, "defaultValue": bigRecord},
		{"type": "defaulted", "validator": map[string]any{"type": "any"}, "defaultValue": map[string]any{"$unknown": "x"}},
		{"type": "defaulted", "validator": map[string]any{"type": "any"}, "defaultValue": math.Inf(1)},
		{"type": "defaulted", "validator": map[string]any{"type": "object"}, "defaultValue": map[string]any{"$bad": float64(0)}},
		{"type": "defaulted", "validator": map[string]any{"type": "object"}, "defaultValue": bigArray},
	}
	for _, d := range invalid {
		if ValidateDescriptor(d) {
			t.Fatalf("expected invalid descriptor %#v", d)
		}
	}
}

type idEnvelope struct {
	V int    `json:"v"`
	K int    `json:"k"`
	N string `json:"n"`
	T string `json:"t"`
	R string `json:"r"`
}

func makeOpaqueIDWithN(n, table string) string {
	payload, _ := json.Marshal(idEnvelope{V: 1, K: 1, N: n, T: table, R: "abcdefghijklmno"})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	mac := make([]byte, 32)
	macB64 := base64.RawURLEncoding.EncodeToString(mac)
	return "pbv1.1." + payloadB64 + "." + macB64
}

func makeOpaqueIDRawPayload(payload string) string {
	payloadB64 := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := make([]byte, 32)
	macB64 := base64.RawURLEncoding.EncodeToString(mac)
	return "pbv1.1." + payloadB64 + "." + macB64
}

func TestOpaqueIDUnicodeCanonicality(t *testing.T) {
	table := "trees"
	validNs := []string{"test", "café", "日本語", "🎉", "a<b>c", "a&b", "a\u2028b", "a\u2029b", "a\bb", "a\fb"}
	for _, n := range validNs {
		id := makeOpaqueIDWithN(n, table)
		if !ValidateDescriptor(map[string]any{
			"type":         "defaulted",
			"validator":    map[string]any{"type": "id", "tableName": table},
			"defaultValue": id,
		}) {
			t.Fatalf("expected valid opaque ID with n=%q to be accepted: %s", n, id)
		}
	}

	invalidCases := []struct {
		label string
		id    string
	}{
		{"raw < in n", makeOpaqueIDRawPayload(`{"v":1,"k":1,"n":"a<b","t":"` + table + `","r":"abcdefghijklmno"}`)},
		{"raw U+2028 in n", makeOpaqueIDRawPayload("{\"v\":1,\"k\":1,\"n\":\"a\u2028b\",\"t\":\"" + table + "\",\"r\":\"abcdefghijklmno\"}")},
		{"raw U+2029 in n", makeOpaqueIDRawPayload("{\"v\":1,\"k\":1,\"n\":\"a\u2029b\",\"t\":\"" + table + "\",\"r\":\"abcdefghijklmno\"}")},
		{"invalid UTF-8", "pbv1.1." + base64.RawURLEncoding.EncodeToString([]byte{0x80}) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))},
		{"null payload", makeOpaqueIDRawPayload("null")},
		{"array payload", makeOpaqueIDRawPayload("[1,2,3]")},
		{"string payload", makeOpaqueIDRawPayload(`"hello"`)},
		{"number payload", makeOpaqueIDRawPayload("42")},
	}
	for _, tc := range invalidCases {
		if ValidateDescriptor(map[string]any{
			"type":         "defaulted",
			"validator":    map[string]any{"type": "id", "tableName": table},
			"defaultValue": tc.id,
		}) {
			t.Fatalf("expected invalid opaque ID (%s) to be rejected: %s", tc.label, tc.id)
		}
	}
}

func makeOpaqueIDWithKeyID(keyID int64, table string) string {
	payload, _ := json.Marshal(idEnvelope{V: 1, K: int(keyID), N: "test", T: table, R: "abcdefghijklmno"})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	mac := make([]byte, 32)
	macB64 := base64.RawURLEncoding.EncodeToString(mac)
	return "pbv1." + strconv.FormatInt(keyID, 10) + "." + payloadB64 + "." + macB64
}

func TestOpaqueIDKeyIDPrecision(t *testing.T) {
	table := "trees"
	validKeyIDs := []int64{
		1,
		9007199254740991,    // JS MAX_SAFE_INTEGER
		9007199254740993,    // above MAX_SAFE_INTEGER
		9223372036854775807, // Go int64 max
	}
	for _, keyID := range validKeyIDs {
		id := makeOpaqueIDWithKeyID(keyID, table)
		if !ValidateDescriptor(map[string]any{
			"type":         "defaulted",
			"validator":    map[string]any{"type": "id", "tableName": table},
			"defaultValue": id,
		}) {
			t.Fatalf("expected valid opaque ID with keyID=%d to be accepted: %s", keyID, id)
		}
	}
	invalidIDs := []struct {
		label string
		id    string
	}{
		{"above int64 max", makeOpaqueIDRawPayload(`{"v":1,"k":9223372036854775808,"n":"test","t":"` + table + `","r":"abcdefghijklmno"}`)},
		{"zero key ID", "pbv1.1.0.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"leading zero", "pbv1.1.01.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"negative", "pbv1.1.-1.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}
	for _, tc := range invalidIDs {
		if ValidateDescriptor(map[string]any{
			"type":         "defaulted",
			"validator":    map[string]any{"type": "id", "tableName": table},
			"defaultValue": tc.id,
		}) {
			t.Fatalf("expected invalid opaque ID (%s) to be rejected: %s", tc.label, tc.id)
		}
	}
}

func TestComponentValueRejectsLegacyOpaqueID(t *testing.T) {
	descriptor := map[string]any{"type": "id", "tableName": "items"}
	legacy := makeOpaqueIDWithN("legacyRootState", "items")
	payload, _ := json.Marshal(opaqueIDEnvelope{V: 2, K: 1, N: "cmp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", T: "items", R: "abcdefghijklmno"})
	v2 := "pbv2.1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if ValidateComponentValue(descriptor, legacy) {
		t.Fatal("component mount accepted legacy root id")
	}
	if !ValidateComponentValue(descriptor, v2) {
		t.Fatal("component mount rejected structurally valid pbv2 id")
	}
	wrongPayload, _ := json.Marshal(opaqueIDEnvelope{V: 2, K: 1, N: "cmp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", T: "other", R: "abcdefghijklmno"})
	wrong := "pbv2.1." + base64.RawURLEncoding.EncodeToString(wrongPayload) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if ValidateComponentValue(descriptor, wrong) {
		t.Fatal("component mount accepted wrong-table id")
	}
}

func TestOpaqueIDCrossLanguagePBV2Golden(t *testing.T) {
	const golden = "pbv2.7.eyJ2IjoyLCJrIjo3LCJuIjoicm9vdCIsInQiOiJtZXNzYWdlcyIsInIiOiJhYmNkZWZnaGlqa2xtbm8ifQ.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	payload, _, _, ok := parseOpaqueID(golden)
	if !ok || payload.V != 2 || payload.K != 7 || payload.N != "root" || payload.T != "messages" || payload.R != "abcdefghijklmno" {
		t.Fatalf("cross-language golden rejected: %#v", payload)
	}
	if _, _, _, ok := parseOpaqueID(strings.Replace(golden, ".AAAAAAAA", ".AAAAAAAA=", 1)); ok {
		t.Fatal("noncanonical base64 accepted")
	}
}

func TestDescriptorDepthBoundary(t *testing.T) {
	var d any = map[string]any{"type": "string"}
	for i := 0; i < 128; i++ {
		d = map[string]any{"type": "optional", "validator": d}
	}
	if !ValidateDescriptor(d) {
		t.Fatal("expected 128 optional wrappers (depth 0..128) to be valid")
	}
	d = map[string]any{"type": "string"}
	for i := 0; i < 129; i++ {
		d = map[string]any{"type": "optional", "validator": d}
	}
	if ValidateDescriptor(d) {
		t.Fatal("expected 129 optional wrappers (depth 129) to be invalid")
	}
}

func TestDescriptorNodeBudgetBoundary(t *testing.T) {
	makeWide := func(outer, inner int) map[string]any {
		shape := map[string]any{}
		for i := 0; i < outer; i++ {
			innerShape := map[string]any{}
			for j := 0; j < inner; j++ {
				innerShape[fmt.Sprintf("f%d", j)] = map[string]any{"type": "string"}
			}
			shape[fmt.Sprintf("g%d", i)] = map[string]any{"type": "object", "shape": innerShape}
		}
		return map[string]any{"type": "object", "shape": shape}
	}
	if !ValidateDescriptor(makeWide(127, 128)) {
		t.Fatal("expected 16384 descriptor nodes to be valid")
	}
	if ValidateDescriptor(makeWide(128, 128)) {
		t.Fatal("expected 16513 descriptor nodes to be invalid")
	}
}

func TestDescriptorByteBudgetBoundary(t *testing.T) {
	ok := strings.Repeat("x", 4<<20)
	if !ValidateDescriptor(map[string]any{
		"type":         "defaulted",
		"validator":    map[string]any{"type": "string"},
		"defaultValue": ok,
	}) {
		t.Fatal("expected 4MiB string default to be valid")
	}
	bad := strings.Repeat("x", (4<<20)+1)
	if ValidateDescriptor(map[string]any{
		"type":         "defaulted",
		"validator":    map[string]any{"type": "string"},
		"defaultValue": bad,
	}) {
		t.Fatal("expected 4MiB+1 string default to be invalid")
	}
}
