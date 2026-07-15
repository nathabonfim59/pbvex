package schema

// The deployment and runtime packages both need to evaluate the manifest
// validator wire format. Keeping that evaluator here prevents activation from
// accepting documents that a request-time insert would reject (or vice versa).

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
)

const (
	MaxValidatorDepth = 128
	MaxValidatorNodes = 16 * 1024
	MaxValidatorBytes = 4 << 20
	maxValidatorArray = 1024
	maxValidatorMap   = 1024
)

// IDChecker authenticates a table-bound opaque id. Nil retains structural
// validation for deployment-time schema checks, while runtime callers provide
// the persisted-key checker.
type IDChecker func(id, table string) bool

// NormalizeValue validates one wire value and applies defaulted validators.
func NormalizeValue(validator, value any, check IDChecker) (any, error) {
	n := newNormalizer(check)
	out, present, err := n.normalize(validator, value, true, 0)
	if err != nil || !present {
		return nil, fmt.Errorf("invalid value")
	}
	return out, nil
}

// NormalizeDocument validates a document shape and applies defaults. When
// partial is true omitted fields are intentionally left untouched; otherwise
// required and defaulted fields are resolved. rejectSystem reserves document
// _id/_creationTime while allowing those ordinary names in nested objects.
func NormalizeDocument(fields map[string]any, doc map[string]any, partial, rejectSystem bool, check IDChecker) (map[string]any, error) {
	n := newNormalizer(check)
	return n.object(fields, doc, partial, rejectSystem, 0)
}

// ValidateValue is a convenience predicate used by callers that do not need
// normalized output.
func ValidateValue(validator, value any, check IDChecker) bool {
	_, err := NormalizeValue(validator, value, check)
	return err == nil
}

// ValidateComponentValue applies structural validation for mount arguments.
// Legacy pbv1 capabilities are root-only and can never be mounted into a
// component namespace.
func ValidateComponentValue(validator, value any) bool {
	n := newNormalizer(nil)
	n.allowLegacyID = false
	_, present, err := n.normalize(validator, value, true, 0)
	return err == nil && present
}

// ValidateDescriptor validates the serializable validator graph. It also
// validates default values against their child, rejects delayed descriptors
// (which cannot be serialized safely), bounds union width, and enforces the
// record-key contract.
func ValidateDescriptor(validator any) bool {
	n := newNormalizer(nil)
	return n.descriptor(validator, 0)
}

type normalizer struct {
	nodes         int
	bytes         int
	check         IDChecker
	allowLegacyID bool
	values        map[uintptr]bool
	validators    map[uintptr]bool
	// definitions maps a recursive name to its declared descriptor, scoped to
	// the `recursive` wrapper currently being evaluated. `ref` descriptors
	// resolve against this map so recursive types are genuinely executable.
	definitions map[string]any
}

func newNormalizer(check IDChecker) *normalizer {
	return &normalizer{
		check:         check,
		allowLegacyID: true,
		values:        map[uintptr]bool{},
		validators:    map[uintptr]bool{},
		definitions:   map[string]any{},
	}
}

func (n *normalizer) budget(amount int) error {
	n.nodes++
	n.bytes += amount
	if n.nodes > MaxValidatorNodes || n.bytes > MaxValidatorBytes {
		return fmt.Errorf("invalid value")
	}
	return nil
}

func (n *normalizer) descriptor(validator any, depth int) bool {
	if depth > MaxValidatorDepth || n.nodes >= MaxValidatorNodes {
		return false
	}
	n.nodes++
	o, ok := validator.(map[string]any)
	if !ok || !n.enterValidator(o) {
		return false
	}
	defer n.leaveValidator(o)
	typ, ok := o["type"].(string)
	if !ok {
		return false
	}
	simple := map[string]bool{"string": true, "number": true, "float64": true, "boolean": true, "any": true, "null": true, "int64": true, "bytes": true}
	if simple[typ] {
		return len(o) == 1
	}
	switch typ {
	case "id":
		table, ok := o["tableName"].(string)
		return ok && len(o) == 2 && safeIdentifier(table)
	case "literal":
		value, ok := o["value"]
		return ok && len(o) == 2 && CanonicalWire(value)
	case "array":
		return len(o) == 2 && n.descriptor(o["item"], depth+1)
	case "object":
		// The accepted v1 fixture format includes a bare object validator as an
		// unconstrained object. Preserve that protocol spelling while routing it
		// through this evaluator rather than treating it as arbitrary JSON.
		if len(o) == 1 {
			return true
		}
		shape, ok := validatorShape(o)
		if !ok || len(o) != 2 || len(shape) > maxValidatorMap {
			return false
		}
		for key, child := range shape {
			if !safeWireKey(key) || !n.descriptor(child, depth+1) {
				return false
			}
		}
		return true
	case "record":
		return len(o) == 3 && recordKeyDescriptor(o["key"], 0, map[uintptr]bool{}) && n.descriptor(o["key"], depth+1) && n.descriptor(o["value"], depth+1)
	case "union":
		branches, ok := o["validators"].([]any)
		if !ok || len(o) != 2 || len(branches) == 0 || len(branches) > 64 {
			return false
		}
		for _, branch := range branches {
			if !n.descriptor(branch, depth+1) {
				return false
			}
		}
		return true
	case "optional":
		return len(o) == 2 && n.descriptor(o["validator"], depth+1)
	case "defaulted":
		child, childOK := o["validator"]
		defaultValue, defaultOK := o["defaultValue"]
		if len(o) != 3 || !childOK || !defaultOK || !n.descriptor(child, depth+1) {
			return false
		}
		// Normalize the default with the current definitions scope so a default
		// nested inside a recursive body can resolve refs (a fresh top-level
		// NormalizeValue would have an empty definitions map).
		_, _, err := n.normalize(child, defaultValue, true, depth+1)
		return err == nil
	case "recursive":
		// {type:'recursive', name, validator} declares a named recursive type.
		// The inner validator may reference the name via {type:'ref', name}.
		name, nameOK := o["name"].(string)
		child, childOK := o["validator"]
		if len(o) != 3 || !nameOK || !safeIdentifier(name) || !childOK {
			return false
		}
		prev, hadPrev := n.definitions[name]
		n.definitions[name] = child
		ok := n.descriptor(child, depth+1)
		if hadPrev {
			n.definitions[name] = prev
		} else {
			delete(n.definitions, name)
		}
		return ok
	case "ref":
		// {type:'ref', name} references a named recursive definition declared
		// by an enclosing `recursive` descriptor. The name must be declared.
		name, nameOK := o["name"].(string)
		if len(o) != 2 || !nameOK || !safeIdentifier(name) {
			return false
		}
		_, declared := n.definitions[name]
		return declared
	default:
		// delayed cannot be represented by an executable serializable graph.
		return false
	}
}

func (n *normalizer) object(fields map[string]any, doc map[string]any, partial, rejectSystem bool, depth int) (map[string]any, error) {
	if depth > MaxValidatorDepth || len(doc) > maxValidatorMap || !n.enterValue(doc) {
		return nil, fmt.Errorf("invalid value")
	}
	defer n.leaveValue(doc)
	if err := n.budget(valueSize(doc)); err != nil {
		return nil, err
	}
	for key := range doc {
		if !safeWireKey(key) {
			return nil, fmt.Errorf("invalid value")
		}
		if rejectSystem && (key == "_id" || key == "_creationTime") {
			return nil, fmt.Errorf("system fields are immutable")
		}
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("unknown field")
		}
	}
	out := make(map[string]any, len(doc))
	for key, validator := range fields {
		if !safeWireKey(key) {
			return nil, fmt.Errorf("invalid validator")
		}
		if rejectSystem && (key == "_id" || key == "_creationTime" || strings.HasPrefix(key, "_pbvex_")) {
			return nil, fmt.Errorf("invalid validator")
		}
		value, present := doc[key]
		if partial && !present {
			continue
		}
		normalized, normalizedPresent, err := n.normalize(validator, value, present, depth+1)
		if err != nil {
			return nil, err
		}
		if normalizedPresent {
			out[key] = normalized
		}
	}
	return out, nil
}

func (n *normalizer) normalize(validator, value any, present bool, depth int) (any, bool, error) {
	if depth > MaxValidatorDepth {
		return nil, false, fmt.Errorf("invalid value")
	}
	o, ok := validator.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("invalid validator")
	}
	// Note: enterValidator is intentionally not used here. Recursive types
	// legitimately re-enter the same descriptor map at each level of the value
	// tree; data cycles are caught by enterValue and depth is bounded by
	// MaxValidatorDepth. enterValidator remains in use for descriptor-graph
	// validation (n.descriptor).
	typ, ok := o["type"].(string)
	if !ok || typ == "" {
		return nil, false, fmt.Errorf("invalid validator")
	}
	switch typ {
	case "optional":
		child, exists := o["validator"]
		if len(o) != 2 || !exists {
			return nil, false, fmt.Errorf("invalid validator")
		}
		if !present {
			return nil, false, nil
		}
		return n.normalize(child, value, true, depth+1)
	case "defaulted":
		child, childOK := o["validator"]
		defaultValue, defaultOK := o["defaultValue"]
		if len(o) != 3 || !childOK || !defaultOK {
			return nil, false, fmt.Errorf("invalid validator")
		}
		if !present {
			return n.normalize(child, defaultValue, true, depth+1)
		}
		return n.normalize(child, value, true, depth+1)
	case "union":
		branches, ok := o["validators"].([]any)
		if len(o) != 2 || !ok || len(branches) == 0 || len(branches) > 64 {
			return nil, false, fmt.Errorf("invalid validator")
		}
		for _, branch := range branches {
			candidate := newNormalizer(n.check)
			candidate.allowLegacyID = n.allowLegacyID
			candidate.definitions = n.definitions
			normalized, exists, err := candidate.normalize(branch, value, present, depth+1)
			n.nodes += candidate.nodes
			n.bytes += candidate.bytes
			if n.nodes > MaxValidatorNodes || n.bytes > MaxValidatorBytes {
				return nil, false, fmt.Errorf("invalid value")
			}
			if err == nil {
				return normalized, exists, nil
			}
		}
		return nil, false, fmt.Errorf("invalid value")
	}
	if !present {
		return nil, false, fmt.Errorf("missing required field")
	}
	if err := n.budget(valueSize(value)); err != nil {
		return nil, false, err
	}
	switch typ {
	case "any":
		if len(o) != 1 || !n.wire(value, depth+1, false) {
			return nil, false, fmt.Errorf("invalid value")
		}
		return value, true, nil
	case "string":
		if len(o) != 1 {
			return nil, false, fmt.Errorf("invalid validator")
		}
		if _, ok := value.(string); !ok {
			return nil, false, fmt.Errorf("invalid value")
		}
	case "number", "float64":
		if len(o) != 1 || !finiteNumber(value) {
			return nil, false, fmt.Errorf("invalid value")
		}
	case "boolean":
		if len(o) != 1 {
			return nil, false, fmt.Errorf("invalid validator")
		}
		if _, ok := value.(bool); !ok {
			return nil, false, fmt.Errorf("invalid value")
		}
	case "null":
		if len(o) != 1 || value != nil {
			return nil, false, fmt.Errorf("invalid value")
		}
	case "int64":
		if len(o) != 1 || !special(value, "$integer", 8) {
			return nil, false, fmt.Errorf("invalid value")
		}
	case "bytes":
		if len(o) != 1 || !special(value, "$bytes", -1) {
			return nil, false, fmt.Errorf("invalid value")
		}
	case "literal":
		literal, exists := o["value"]
		if len(o) != 2 || !exists || !CanonicalWire(literal) || !wireEqual(literal, value) {
			return nil, false, fmt.Errorf("invalid value")
		}
	case "id":
		table, ok := o["tableName"].(string)
		id, isString := value.(string)
		parsed, _, _, structurallyValid := parseOpaqueID(id)
		target := parsed.T
		if len(o) != 2 || !ok || !safeIdentifier(table) || !isString || !structurallyValid || target != table || (n.check != nil && !n.check(id, table)) {
			return nil, false, fmt.Errorf("invalid value")
		}
		if !n.allowLegacyID && parsed.V != 2 {
			return nil, false, fmt.Errorf("invalid value")
		}
	case "array":
		if len(o) != 2 {
			return nil, false, fmt.Errorf("invalid validator")
		}
		values, ok := value.([]any)
		if !ok || len(values) > maxValidatorArray || !n.enterValue(values) {
			return nil, false, fmt.Errorf("invalid value")
		}
		defer n.leaveValue(values)
		out := make([]any, len(values))
		for i, item := range values {
			normalized, exists, err := n.normalize(o["item"], item, true, depth+1)
			if err != nil || !exists {
				return nil, false, fmt.Errorf("invalid value")
			}
			out[i] = normalized
		}
		return out, true, nil
	case "object":
		object, valueOK := value.(map[string]any)
		if !valueOK {
			return nil, false, fmt.Errorf("invalid value")
		}
		if len(o) == 1 {
			if !n.wire(object, depth+1, false) {
				return nil, false, fmt.Errorf("invalid value")
			}
			return object, true, nil
		}
		shape, shapeOK := validatorShape(o)
		if len(o) != 2 || !shapeOK {
			return nil, false, fmt.Errorf("invalid value")
		}
		out, err := n.object(shape, object, false, false, depth+1)
		if err != nil {
			return nil, false, err
		}
		return out, true, nil
	case "record":
		if len(o) != 3 || !recordKeyDescriptor(o["key"], 0, map[uintptr]bool{}) {
			return nil, false, fmt.Errorf("invalid validator")
		}
		object, ok := value.(map[string]any)
		if !ok || len(object) > maxValidatorMap || !n.enterValue(object) {
			return nil, false, fmt.Errorf("invalid value")
		}
		defer n.leaveValue(object)
		out := make(map[string]any, len(object))
		for key, item := range object {
			normalizedKey, presentKey, err := n.normalize(o["key"], key, true, depth+1)
			if err != nil || !presentKey {
				return nil, false, fmt.Errorf("invalid value")
			}
			key, ok := normalizedKey.(string)
			if !ok || !safeWireKey(key) {
				return nil, false, fmt.Errorf("invalid value")
			}
			normalized, presentValue, err := n.normalize(o["value"], item, true, depth+1)
			if err != nil || !presentValue {
				return nil, false, fmt.Errorf("invalid value")
			}
			out[key] = normalized
		}
		return out, true, nil
	case "recursive":
		name, nameOK := o["name"].(string)
		child, childOK := o["validator"]
		if len(o) != 3 || !nameOK || !safeIdentifier(name) || !childOK {
			return nil, false, fmt.Errorf("invalid validator")
		}
		prev, hadPrev := n.definitions[name]
		n.definitions[name] = child
		out, p, err := n.normalize(child, value, present, depth+1)
		if hadPrev {
			n.definitions[name] = prev
		} else {
			delete(n.definitions, name)
		}
		return out, p, err
	case "ref":
		name, nameOK := o["name"].(string)
		target, declared := n.definitions[name]
		if len(o) != 2 || !nameOK || !declared {
			return nil, false, fmt.Errorf("invalid validator")
		}
		return n.normalize(target, value, present, depth+1)
	default:
		return nil, false, fmt.Errorf("invalid validator")
	}
	return value, true, nil
}

// CanonicalWire validates the protocol wire-value subset independently of a
// particular validator.
func CanonicalWire(value any) bool {
	n := newNormalizer(nil)
	return n.wire(value, 0, true)
}

func (n *normalizer) wire(value any, depth int, account bool) bool {
	if depth > MaxValidatorDepth {
		return false
	}
	if account && n.budget(valueSize(value)) != nil {
		return false
	}
	switch v := value.(type) {
	case nil, bool, string:
		return true
	case float64, float32, int, int32, int64, uint32:
		return finiteNumber(v)
	case []any:
		if len(v) > maxValidatorArray || !n.enterValue(v) {
			return false
		}
		defer n.leaveValue(v)
		for _, item := range v {
			if !n.wire(item, depth+1, true) {
				return false
			}
		}
		return true
	case map[string]any:
		if len(v) > maxValidatorMap || !n.enterValue(v) {
			return false
		}
		defer n.leaveValue(v)
		if len(v) == 1 && (special(v, "$integer", 8) || special(v, "$bytes", -1)) {
			return true
		}
		for key, item := range v {
			if !safeWireKey(key) || !n.wire(item, depth+1, true) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func validatorShape(o map[string]any) (map[string]any, bool) {
	if shape, ok := o["shape"].(map[string]any); ok {
		return shape, true
	}
	shape, ok := o["fields"].(map[string]any)
	return shape, ok
}

// ObjectFieldValidator returns a direct child validator of a constrained
// object. It intentionally does not traverse an unconstrained object or a
// union: q.field paths are schema-addressable only when every requested
// segment is unambiguous and declared. Other wire-object keys remain valid
// application data but are not query-path syntax.
func ObjectFieldValidator(validator any, name string) (any, bool) {
	if !safePathSegment(name) {
		return nil, false
	}
	for {
		o, ok := validator.(map[string]any)
		if !ok {
			return nil, false
		}
		typ, _ := o["type"].(string)
		if typ == "optional" || typ == "defaulted" {
			validator = o["validator"]
			continue
		}
		if typ != "object" || len(o) == 1 {
			return nil, false
		}
		shape, ok := validatorShape(o)
		if !ok {
			return nil, false
		}
		child, ok := shape[name]
		return child, ok
	}
}

func recordKeyDescriptor(validator any, depth int, stack map[uintptr]bool) bool {
	if depth > MaxValidatorDepth {
		return false
	}
	o, ok := validator.(map[string]any)
	if !ok {
		return false
	}
	p := reflect.ValueOf(o).Pointer()
	if p != 0 && stack[p] {
		return false
	}
	if p != 0 {
		stack[p] = true
		defer delete(stack, p)
	}
	switch o["type"] {
	case "string":
		return len(o) == 1
	case "literal":
		value, ok := o["value"].(string)
		return len(o) == 2 && ok && safeWireKey(value)
	case "union":
		branches, ok := o["validators"].([]any)
		if len(o) != 2 || !ok || len(branches) == 0 || len(branches) > 64 {
			return false
		}
		for _, branch := range branches {
			if !recordKeyDescriptor(branch, depth+1, stack) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func special(value any, name string, wantLen int) bool {
	m, ok := value.(map[string]any)
	if !ok || len(m) != 1 {
		return false
	}
	encoded, ok := m[name].(string)
	if !ok {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	return err == nil && (wantLen < 0 || len(b) == wantLen) && base64.StdEncoding.EncodeToString(b) == encoded
}

func finiteNumber(value any) bool {
	switch v := value.(type) {
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	case float32:
		return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
	case int, int32, int64, uint32:
		return true
	default:
		return false
	}
}

func safeIdentifier(value string) bool {
	if value == "" || len(value) > 1024 {
		return false
	}
	for i := range value {
		c := value[i]
		if i == 0 {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
				return false
			}
			continue
		}
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func safeWireKey(value string) bool {
	if value == "" || len(value) > 1024 || strings.HasPrefix(value, "$") || value == "__proto__" || value == "constructor" || value == "prototype" {
		return false
	}
	for _, c := range value {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

func safePathSegment(value string) bool {
	return safeWireKey(value) && !strings.Contains(value, ".")
}

func opaqueIDShape(value string) bool {
	_, _, ok := opaqueIDTarget(value)
	return ok
}

type opaqueIDEnvelope struct {
	V int    `json:"v"`
	K int    `json:"k"`
	N string `json:"n"`
	T string `json:"t"`
	R string `json:"r"`
}

func parseOpaqueID(value string) (opaqueIDEnvelope, []byte, []byte, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 4 || (parts[0] != "pbv1" && parts[0] != "pbv2") || len(value) > 4096 {
		return opaqueIDEnvelope{}, nil, nil, false
	}
	keyID, err := strconv.Atoi(parts[1])
	if err != nil || keyID < 1 || strconv.Itoa(keyID) != parts[1] {
		return opaqueIDEnvelope{}, nil, nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(payload) == 0 || base64.RawURLEncoding.EncodeToString(payload) != parts[2] {
		return opaqueIDEnvelope{}, nil, nil, false
	}
	var p opaqueIDEnvelope
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.DisallowUnknownFields()
	wantVersion := 1
	if parts[0] == "pbv2" {
		wantVersion = 2
	}
	if dec.Decode(&p) != nil || dec.Decode(&struct{}{}) != io.EOF || p.V != wantVersion || p.K != keyID || !safeIdentifier(p.T) || p.N == "" || len(p.R) != 15 {
		return opaqueIDEnvelope{}, nil, nil, false
	}
	canonical, err := json.Marshal(p)
	if err != nil || string(canonical) != string(payload) {
		return opaqueIDEnvelope{}, nil, nil, false
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(mac) != 32 || base64.RawURLEncoding.EncodeToString(mac) != parts[3] {
		return opaqueIDEnvelope{}, nil, nil, false
	}
	return p, payload, mac, true
}

func opaqueIDTarget(value string) (string, string, bool) {
	p, _, _, ok := parseOpaqueID(value)
	if !ok {
		return "", "", false
	}
	return p.T, p.R, true
}

func opaqueIDMAC(key, payload []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("pbvex:id:v1:"))
	m.Write(payload)
	return m.Sum(nil)
}

// OpaqueIDMAC is shared by the runtime encoder and the activation verifier.
// Keeping the domain-separated primitive here prevents an ID written by one
// request path from using subtly different signing bytes than a v.id checked
// during migration.
func OpaqueIDMAC(key, payload []byte) []byte {
	return opaqueIDMAC(key, payload)
}

func opaqueIDVersionKey(root []byte, keyID int) []byte {
	m := hmac.New(sha256.New, root)
	m.Write([]byte("pbvex:id:key:v1:"))
	m.Write([]byte(strconv.Itoa(keyID)))
	return m.Sum(nil)
}

// OpaqueIDVersionMAC derives a stable signing key for the embedded version
// from the persistent ID root. The root is retained indefinitely, so changing
// the active version never makes a live document id or v.id reference expire.
func OpaqueIDVersionMAC(root []byte, keyID int, payload []byte) []byte {
	if len(root) < 32 || keyID < 1 {
		return nil
	}
	return opaqueIDMAC(opaqueIDVersionKey(root, keyID), payload)
}

// VerifyOpaqueID authenticates a PBVex capability against the persisted
// namespace identity root. legacyRoot is a durable, one-time migration
// verifier for ids written by the historical cursor-key scheme. It is kept
// independently of cursor current/previous so those old ids survive arbitrary
// future cursor rotations and restarts. current/previous remain a short
// compatibility path for a rolling upgrade that has not yet persisted the
// anchor.
func VerifyOpaqueID(value, namespace string, identityRoot, legacyRoot []byte, cursorKeyID int, cursorCurrent, cursorPrevious []byte) (string, string, bool) {
	p, payload, supplied, ok := parseOpaqueID(value)
	if !ok || p.N != namespace || len(identityRoot) < 32 {
		return "", "", false
	}
	// v2 of the signing policy derives a key from the permanent root and the
	// payload's version label. The direct-root form remains accepted only for
	// IDs emitted by the immediately preceding implementation.
	if hmac.Equal(supplied, OpaqueIDVersionMAC(identityRoot, p.K, payload)) || hmac.Equal(supplied, opaqueIDMAC(identityRoot, payload)) {
		return p.T, p.R, true
	}
	if len(legacyRoot) >= 32 && hmac.Equal(supplied, opaqueIDMAC(legacyRoot, payload)) {
		return p.T, p.R, true
	}
	if len(cursorCurrent) < 32 || cursorKeyID < 1 {
		return "", "", false
	}
	key := cursorCurrent
	if p.K != cursorKeyID {
		if p.K != cursorKeyID-1 || len(cursorPrevious) < 32 {
			return "", "", false
		}
		key = cursorPrevious
	}
	if !hmac.Equal(supplied, opaqueIDMAC(key, payload)) {
		return "", "", false
	}
	return p.T, p.R, true
}

func valueSize(value any) int {
	switch v := value.(type) {
	case string:
		return len(v)
	case map[string]any:
		n := 1
		for key, item := range v {
			n += len(key)
			switch x := item.(type) {
			case string:
				n += len(x)
			case []any:
				n += len(x)
			case map[string]any:
				n += len(x)
			default:
				n++
			}
		}
		return n
	case []any:
		return len(v) + 1
	default:
		return 1
	}
}

func (n *normalizer) enterValue(value any) bool {
	v := reflect.ValueOf(value)
	if !v.IsValid() || (v.Kind() != reflect.Map && v.Kind() != reflect.Slice) {
		return true
	}
	p := v.Pointer()
	if p != 0 && n.values[p] {
		return false
	}
	if p != 0 {
		n.values[p] = true
	}
	return true
}
func (n *normalizer) leaveValue(value any) {
	v := reflect.ValueOf(value)
	if v.IsValid() && (v.Kind() == reflect.Map || v.Kind() == reflect.Slice) && v.Pointer() != 0 {
		delete(n.values, v.Pointer())
	}
}
func (n *normalizer) enterValidator(value map[string]any) bool {
	p := reflect.ValueOf(value).Pointer()
	if p != 0 && n.validators[p] {
		return false
	}
	if p != 0 {
		n.validators[p] = true
	}
	return true
}
func (n *normalizer) leaveValidator(value map[string]any) {
	if p := reflect.ValueOf(value).Pointer(); p != 0 {
		delete(n.validators, p)
	}
}
