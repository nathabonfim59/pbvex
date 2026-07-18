package schema

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/dop251/goja"
)

// DocumentOrderField is an internal, hidden JSON projection of document
// values.  SQLite's JSON comparison rules deliberately conflate missing and
// null and do not order the protocol's little-endian int64 or base64 bytes
// values.  PBVex therefore indexes this canonical projection instead of the
// user document directly.  The projection is written transactionally with the
// document and is never exposed to JavaScript.
const DocumentOrderField = "_pbvex_order"

// equalityProjectionPrefix identifies a second, validator-independent order
// key kept alongside the validator-aware key.  A v.id, v.string, v.any, and a
// compatible union can all carry the same wire string.  Their *sort* ranks are
// intentionally distinct, but equality must compare the canonical wire value,
// not the field validator that happened to contain it.
const equalityProjectionPrefix = "$eq:"

// EqualityProjectionField returns the internal projection key for a declared
// q.field path. Document field names cannot begin with '$', so this cannot
// collide with a user-owned projection entry.
func EqualityProjectionField(path string) string { return equalityProjectionPrefix + path }

const (
	orderMissing = "00"
	orderNull    = "01"
	orderFalse   = "02"
	orderTrue    = "03"
	orderNumber  = "04"
	orderString  = "05"
	orderID      = "06"
	orderInt64   = "07"
	orderBytes   = "08"
	orderComplex = "09"
)

// OrderData produces a total-order key for every declared table field.  A
// field which is absent from the source document is deliberately different
// from a field whose value is null.
func OrderData(fields map[string]any, document map[string]any) (map[string]any, error) {
	return OrderDataWithID(fields, document, nil)
}

// OrderDataWithID is OrderData with the request/activation ID authenticator.
// Passing it keeps an id|string union from classifying a merely id-shaped
// string as an ID rank; runtime writes and activation both use this form.
func OrderDataWithID(fields map[string]any, document map[string]any, check IDChecker) (map[string]any, error) {
	out := make(map[string]any, len(fields)*2)
	for name, validator := range fields {
		value, present := document[name]
		if err := appendOrderData(out, name, validator, value, present, check); err != nil {
			return nil, err
		}
		if err := appendNestedOrderData(out, name, validator, value, present, check, 0); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func appendOrderData(out map[string]any, path string, validator, value any, present bool, check IDChecker) error {
	key, err := OrderKeyWithID(validator, value, present, check)
	if err != nil {
		return err
	}
	equality, err := GenericOrderKey(value, present)
	if err != nil {
		return err
	}
	out[path] = key
	out[EqualityProjectionField(path)] = equality
	return nil
}

// appendNestedOrderData materializes canonical keys for every addressable
// constrained-object path. q.field uses dot-separated declared object paths;
// keeping their keys in the same hidden projection as top-level fields gives
// nested equality/order the same missing/null/type semantics as an index key.
func appendNestedOrderData(out map[string]any, prefix string, validator, value any, present bool, check IDChecker, depth int) error {
	if depth > MaxValidatorDepth {
		return fmt.Errorf("invalid validator")
	}
	for {
		o, ok := validator.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid validator")
		}
		typ, _ := o["type"].(string)
		if typ == "optional" || typ == "defaulted" {
			validator = o["validator"]
			continue
		}
		if typ != "object" || len(o) == 1 {
			return nil
		}
		shape, ok := validatorShape(o)
		if !ok {
			return fmt.Errorf("invalid validator")
		}
		var object map[string]any
		if present {
			var objectOK bool
			object, objectOK = value.(map[string]any)
			if !objectOK {
				return fmt.Errorf("invalid value")
			}
		}
		for name, child := range shape {
			childValue, childPresent := object[name]
			path := prefix + "." + name
			if err := appendOrderData(out, path, child, childValue, childPresent, check); err != nil {
				return err
			}
			if err := appendNestedOrderData(out, path, child, childValue, childPresent, check, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
}

// FieldValidator resolves a canonical q.field path against a table field
// shape. Top-level document fields and constrained-object segments reject
// literal dots, so splitting never falls back to a user-provided SQL path.
// Manifest validation, physical-index materialization and runtime querying all
// call this one resolver.
func FieldValidator(fields map[string]any, path string) (any, bool) {
	if path == "" || strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") || strings.Contains(path, "..") {
		return nil, false
	}
	parts := strings.Split(path, ".")
	if !safePathSegment(parts[0]) {
		return nil, false
	}
	validator, ok := fields[parts[0]]
	if !ok {
		return nil, false
	}
	for _, part := range parts[1:] {
		validator, ok = ObjectFieldValidator(validator, part)
		if !ok {
			return nil, false
		}
	}
	return validator, true
}

// OrderKey produces a lexical key whose byte order is the PBVex wire-value
// order.  It intentionally supports only the scalar values that can be used
// by a declared SQLite index.  Activation rejects an index over a compound or
// otherwise unsupported validator rather than silently using SQLite's
// incompatible JSON ordering.
func OrderKey(validator any, value any, present bool) (string, error) {
	return OrderKeyWithID(validator, value, present, nil)
}

// OrderKeyWithID derives a protocol ordering key while preserving a
// validated id|string union distinction. IDChecker is optional for tooling
// that only has a serializable descriptor; request and activation paths must
// provide it.
func OrderKeyWithID(validator any, value any, present bool, check IDChecker) (string, error) {
	if !present {
		return orderMissing, nil
	}
	typ, ok := validatorType(validator)
	if !ok {
		return "", fmt.Errorf("invalid validator")
	}
	for typ == "optional" || typ == "defaulted" {
		o, _ := validator.(map[string]any)
		validator = o["validator"]
		typ, ok = validatorType(validator)
		if !ok {
			return "", fmt.Errorf("invalid validator")
		}
	}
	if value == nil {
		if validatorAllowsNull(validator) {
			return orderNull, nil
		}
		return "", fmt.Errorf("invalid null")
	}
	if typ == "union" {
		o, _ := validator.(map[string]any)
		branches, ok := o["validators"].([]any)
		if !ok || len(branches) == 0 {
			return "", fmt.Errorf("invalid validator")
		}
		// IDs are strings on the wire. Prefer a declared, authenticated id
		// branch for a valid capability so string/id unions retain their
		// distinct protocol ranks instead of depending on branch order. If a
		// token only looks like an ID, let a declared string branch handle it.
		if s, ok := value.(string); ok {
			target, _, structurallyValid := opaqueIDTarget(s)
			if structurallyValid {
				for _, branch := range branches {
					if validatorIsID(branch) {
						if table, tableOK := validatorIDTable(branch); tableOK && target == table && (check == nil || check(s, table)) {
							key, err := OrderKeyWithID(branch, value, true, check)
							if err == nil {
								return key, nil
							}
						}
					}
				}
			}
		}
		for _, branch := range branches {
			if key, err := OrderKeyWithID(branch, value, true, check); err == nil {
				return key, nil
			}
		}
		return "", fmt.Errorf("invalid union value")
	}

	switch typ {
	case "null":
		return "", fmt.Errorf("invalid null")
	case "any":
		return genericOrderKey(value, true)
	case "boolean":
		b, ok := value.(bool)
		if !ok {
			return "", fmt.Errorf("invalid boolean")
		}
		if b {
			return orderTrue, nil
		}
		return orderFalse, nil
	case "number", "float64":
		n, ok := finiteFloat(value)
		if !ok {
			return "", fmt.Errorf("invalid number")
		}
		// The wire/canonical JSON representation treats -0 as 0.
		if n == 0 {
			n = 0
		}
		bits := math.Float64bits(n)
		// IEEE-754's sign-magnitude encoding is not lexically sortable.  Flip
		// positive signs and invert negative values to obtain monotonically
		// increasing big-endian bytes, including negative and very large values.
		if bits&(uint64(1)<<63) != 0 {
			bits = ^bits
		} else {
			bits ^= uint64(1) << 63
		}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], bits)
		return orderNumber + hex.EncodeToString(b[:]), nil
	case "string", "image":
		s, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("invalid string")
		}
		return orderString + hex.EncodeToString([]byte(s)), nil
	case "id":
		s, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("invalid id")
		}
		o, _ := validator.(map[string]any)
		table, _ := o["tableName"].(string)
		target, raw, valid := opaqueIDTarget(s)
		if !valid || table == "" || target != table || (check != nil && !check(s, table)) {
			return "", fmt.Errorf("invalid id")
		}
		return OpaqueIDOrderKey(target, raw), nil
	case "int64":
		m, ok := value.(map[string]any)
		encoded, ok := m["$integer"].(string)
		if !ok || len(m) != 1 {
			return "", fmt.Errorf("invalid int64")
		}
		b, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(b) != 8 || base64.StdEncoding.EncodeToString(b) != encoded {
			return "", fmt.Errorf("invalid int64")
		}
		// The wire representation is little-endian two's complement.  Convert
		// it to a sortable signed big-endian representation.
		u := binary.LittleEndian.Uint64(b)
		u ^= uint64(1) << 63
		var ordered [8]byte
		binary.BigEndian.PutUint64(ordered[:], u)
		return orderInt64 + hex.EncodeToString(ordered[:]), nil
	case "bytes":
		m, ok := value.(map[string]any)
		encoded, ok := m["$bytes"].(string)
		if !ok || len(m) != 1 {
			return "", fmt.Errorf("invalid bytes")
		}
		b, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || base64.StdEncoding.EncodeToString(b) != encoded {
			return "", fmt.Errorf("invalid bytes")
		}
		return orderBytes + hex.EncodeToString(b), nil
	case "literal":
		o, _ := validator.(map[string]any)
		if !wireEqual(o["value"], value) {
			return "", fmt.Errorf("invalid literal")
		}
		return genericOrderKey(o["value"], true)
	default:
		// Compound values are never accepted for a physical manifest index,
		// but retaining a canonical projection lets equality filters over an
		// otherwise valid object/array field stay deterministic.
		return complexOrderKey(value)
	}
}

// OpaqueIDOrderKey is the canonical ordering representation of an ID value.
// The signed transport encoding intentionally changes when key versions or
// MACs change; ordering the capability text would make the same record move
// between pages after rotation. The table/raw-record tuple is immutable and is
// also what the system _id SQL expression projects.
func OpaqueIDOrderKey(table, raw string) string {
	return orderID + hex.EncodeToString([]byte(table+"\x00"+raw))
}

// IndexableValidator reports whether a manifest validator has a stable,
// materializable scalar sort key.  This is deliberately conservative: a
// SQLite index over an arbitrary union/object/array would have semantics that
// differ from the protocol evaluator.
func IndexableValidator(validator any) bool {
	typ, ok := validatorType(validator)
	if !ok {
		return false
	}
	if typ == "optional" || typ == "defaulted" {
		o, _ := validator.(map[string]any)
		return IndexableValidator(o["validator"])
	}
	switch typ {
	case "string", "number", "float64", "boolean", "null", "id", "image", "int64", "bytes":
		return true
	case "literal":
		o, _ := validator.(map[string]any)
		return literalIndexable(o["value"])
	case "union":
		o, _ := validator.(map[string]any)
		branches, ok := o["validators"].([]any)
		if !ok || len(branches) == 0 || len(branches) > 64 {
			return false
		}
		for _, branch := range branches {
			if !IndexableValidator(branch) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func validatorAllowsNull(validator any) bool {
	typ, ok := validatorType(validator)
	if !ok {
		return false
	}
	if typ == "optional" || typ == "defaulted" {
		o, _ := validator.(map[string]any)
		return validatorAllowsNull(o["validator"])
	}
	if typ == "null" || typ == "any" {
		return true
	}
	if typ != "union" {
		return false
	}
	o, _ := validator.(map[string]any)
	branches, ok := o["validators"].([]any)
	if !ok {
		return false
	}
	for _, branch := range branches {
		if validatorAllowsNull(branch) {
			return true
		}
	}
	return false
}

func validatorIsID(validator any) bool {
	typ, ok := validatorType(validator)
	if !ok {
		return false
	}
	if typ == "optional" || typ == "defaulted" {
		o, _ := validator.(map[string]any)
		return validatorIsID(o["validator"])
	}
	return typ == "id"
}

func validatorIDTable(validator any) (string, bool) {
	typ, ok := validatorType(validator)
	if !ok {
		return "", false
	}
	for typ == "optional" || typ == "defaulted" {
		o, _ := validator.(map[string]any)
		validator = o["validator"]
		typ, ok = validatorType(validator)
		if !ok {
			return "", false
		}
	}
	if typ != "id" {
		return "", false
	}
	o, _ := validator.(map[string]any)
	table, ok := o["tableName"].(string)
	return table, ok && table != ""
}

func literalIndexable(value any) bool {
	switch v := value.(type) {
	case nil, bool, string, float64, float32, int, int32, int64, uint32:
		return true
	case map[string]any:
		if canonicalSpecialLiteral(v, "$integer", 8) || canonicalSpecialLiteral(v, "$bytes", -1) {
			return true
		}
	}
	return false
}

func canonicalSpecialLiteral(value map[string]any, name string, wantLen int) bool {
	encoded, ok := value[name].(string)
	if !ok || len(value) != 1 {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	return err == nil && (wantLen < 0 || len(b) == wantLen) && base64.StdEncoding.EncodeToString(b) == encoded
}

// GenericOrderKey is used for index bounds.  The validator determines the
// id-vs-string distinction where applicable; all other scalar literals retain
// their protocol type rank.
func GenericOrderKey(value any, present bool) (string, error) {
	return genericOrderKey(value, present)
}

func genericOrderKey(value any, present bool) (string, error) {
	if !present {
		return orderMissing, nil
	}
	if value == nil {
		return orderNull, nil
	}
	switch v := value.(type) {
	case bool:
		if v {
			return orderTrue, nil
		}
		return orderFalse, nil
	case string:
		return orderString + hex.EncodeToString([]byte(v)), nil
	case float64, float32, int, int32, int64, uint32:
		return OrderKey(map[string]any{"type": "number"}, v, true)
	case map[string]any:
		if _, ok := v["$integer"]; ok {
			return OrderKey(map[string]any{"type": "int64"}, v, true)
		}
		if _, ok := v["$bytes"]; ok {
			return OrderKey(map[string]any{"type": "bytes"}, v, true)
		}
		return complexOrderKey(v)
	case []any:
		return complexOrderKey(v)
	}
	return "", fmt.Errorf("unsupported indexed value")
}

func complexOrderKey(value any) (string, error) {
	b, err := canonicalOrderJSON(value)
	if err != nil {
		return "", fmt.Errorf("invalid compound value")
	}
	return orderComplex + hex.EncodeToString([]byte(b)), nil
}

func wireEqual(left, right any) bool {
	a, errA := canonicalOrderJSON(left)
	b, errB := canonicalOrderJSON(right)
	return errA == nil && errB == nil && a == b
}

// canonicalOrderJSON mirrors the protocol's canonical JSON spelling for
// compound wire values. JSON object keys are sorted, -0 normalizes to 0 and
// finite floating numbers use JSON.stringify's spelling, so an equality key
// computed at write time is identical to one computed from q.literal later.
func canonicalOrderJSON(value any) (string, error) {
	var out bytes.Buffer
	if err := writeCanonicalOrderJSON(&out, value, 0, map[uintptr]bool{}); err != nil {
		return "", err
	}
	return out.String(), nil
}

func writeCanonicalOrderJSON(out *bytes.Buffer, value any, depth int, seen map[uintptr]bool) error {
	if depth > MaxValidatorDepth {
		return fmt.Errorf("invalid compound value")
	}
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
		return nil
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
		return nil
	case string:
		encoded, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out.Write(encoded)
		return nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("invalid compound value")
		}
		if v == 0 {
			out.WriteByte('0')
			return nil
		}
		vm := goja.New()
		if err := vm.Set("n", v); err != nil {
			return err
		}
		encoded, err := vm.RunString("JSON.stringify(n)")
		if err != nil {
			return err
		}
		out.WriteString(encoded.String())
		return nil
	case float32:
		return writeCanonicalOrderJSON(out, float64(v), depth, seen)
	case int:
		out.WriteString(strconv.Itoa(v))
		return nil
	case int32:
		out.WriteString(strconv.FormatInt(int64(v), 10))
		return nil
	case int64:
		out.WriteString(strconv.FormatInt(v, 10))
		return nil
	case uint32:
		out.WriteString(strconv.FormatUint(uint64(v), 10))
		return nil
	case []any:
		ptr := reflect.ValueOf(v).Pointer()
		if ptr != 0 && seen[ptr] {
			return fmt.Errorf("invalid compound value")
		}
		if ptr != 0 {
			seen[ptr] = true
			defer delete(seen, ptr)
		}
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonicalOrderJSON(out, item, depth+1, seen); err != nil {
				return err
			}
		}
		out.WriteByte(']')
		return nil
	case map[string]any:
		ptr := reflect.ValueOf(v).Pointer()
		if ptr != 0 && seen[ptr] {
			return fmt.Errorf("invalid compound value")
		}
		if ptr != 0 {
			seen[ptr] = true
			defer delete(seen, ptr)
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonicalOrderJSON(out, key, depth+1, seen); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := writeCanonicalOrderJSON(out, v[key], depth+1, seen); err != nil {
				return err
			}
		}
		out.WriteByte('}')
		return nil
	default:
		return fmt.Errorf("invalid compound value")
	}
}

func validatorType(validator any) (string, bool) {
	o, ok := validator.(map[string]any)
	if !ok {
		return "", false
	}
	typ, ok := o["type"].(string)
	return typ, ok && typ != ""
}

func finiteFloat(value any) (float64, bool) {
	var n float64
	switch v := value.(type) {
	case float64:
		n = v
	case float32:
		n = float64(v)
	case int:
		n = float64(v)
	case int32:
		n = float64(v)
	case int64:
		n = float64(v)
	case uint32:
		n = float64(v)
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
}
