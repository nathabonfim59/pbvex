package runtime

// This file deliberately keeps the JavaScript database surface small and
// request-scoped.  There is no package global database handle: every object is
// built for one handler invocation and holds the app/manifest snapshot passed
// by deploy.Service.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const documentDataField = "_pbvex_data"
const maxQueryItems = 1024
const maxCursorBytes = 16 << 10
const maxCursorTupleItems = 64

// PBVex record identifiers are opaque, table-bound capabilities.  The
// payload has a stable schema-state namespace and is authenticated by the
// persisted key ring, so a PocketBase raw id cannot be moved between tables
// (or forged after being observed in another response).
type opaqueIDPayload struct {
	V int    `json:"v"`
	K int    `json:"k"`
	N string `json:"n"`
	T string `json:"t"`
	R string `json:"r"`
}

type keyRing struct {
	namespace       string
	legacyNamespace string
	currentID       int
	current         []byte
	previous        []byte
	idKeyID         int
	id              []byte
	legacy          []byte
}

func parseOpaqueID(id string) (opaqueIDPayload, []byte, []byte, bool) {
	parts := strings.Split(id, ".")
	if len(parts) != 4 || (parts[0] != "pbv1" && parts[0] != "pbv2") || len(id) > 4096 {
		return opaqueIDPayload{}, nil, nil, false
	}
	kid, err := strconv.Atoi(parts[1])
	if err != nil || kid < 1 || strconv.Itoa(kid) != parts[1] {
		return opaqueIDPayload{}, nil, nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(payload) == 0 || len(payload) > 2048 || base64.RawURLEncoding.EncodeToString(payload) != parts[2] {
		return opaqueIDPayload{}, nil, nil, false
	}
	var p opaqueIDPayload
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	wantVersion := 1
	if parts[0] == "pbv2" {
		wantVersion = 2
	}
	if dec.Decode(&p) != nil || dec.Decode(&struct{}{}) != io.EOF || p.V != wantVersion || p.K != kid ||
		p.N == "" || p.T == "" || len(p.R) != 15 {
		return opaqueIDPayload{}, nil, nil, false
	}
	canonical, err := json.Marshal(p)
	if err != nil || !bytes.Equal(canonical, payload) {
		return opaqueIDPayload{}, nil, nil, false
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(mac) != sha256.Size || base64.RawURLEncoding.EncodeToString(mac) != parts[3] {
		return opaqueIDPayload{}, nil, nil, false
	}
	return p, payload, mac, true
}

type database struct {
	vm        *goja.Runtime
	ctx       context.Context
	app       core.App
	manifest  deploy.DeploymentManifest
	namespace string
	schema    any
	physical  map[string]string
	write     bool
	function  string
	args      string
	rawArgs   any
	keys      *keyRing
	argPaths  map[*goja.Object][]string
}
type expression struct {
	op, field string
	value     any
	args      []*expression
}

// expressionInfo is deliberately richer than a Go/SQLite storage type. It
// records the protocol kind and whether an expression may be missing or null,
// so validation never accidentally delegates three-valued SQLite semantics to
// JavaScript filters.
type expressionInfo struct {
	kind      string
	nullable  bool
	validator any
	literal   any
	field     string
}
type queryBuilder struct {
	db          *database
	table       string
	predicate   *expression
	descending  bool
	hasOrder    bool
	index       string
	indexFields []string
	indexBounds []indexBound
}
type indexBound struct {
	field string
	op    string
	key   string
}
type indexRange struct {
	d            *database
	fields       []string
	pos          int
	rangeField   string
	lower, upper *expression
	equal        []*expression
}

func databaseScope(ctx context.Context, app core.App, manifest deploy.DeploymentManifest, fd deploy.FunctionDescriptor) *database {
	db := &database{ctx: ctx, app: app, manifest: manifest, namespace: deploy.RootNamespace, schema: manifest.Schema}
	if namespace, ok := deploy.NamespaceForModule(manifest, fd.ModulePath); ok {
		db.namespace = namespace.ID
		db.schema = namespace.Schema
		db.physical = namespace.PhysicalByTable
	}
	return db
}

func newInvocationContext(vm *goja.Runtime, ctx context.Context, app core.App, manifest deploy.DeploymentManifest, fd deploy.FunctionDescriptor, function string, args any, jsArgs goja.Value, extenders []ContextExtender) (*goja.Object, error) {
	o := vm.NewObject()
	namespace, mounted := deploy.NamespaceForModule(manifest, fd.ModulePath)
	if mounted {
		var check schema.IDChecker
		if app != nil {
			check = databaseScope(ctx, app, manifest, fd).validIDForTable
		}
		mountArgs, present, err := resolveComponentArgs(namespace, check)
		if err != nil {
			return nil, err
		}
		if present {
			decoded, err := decodeWire(vm, mountArgs)
			if err != nil {
				return nil, fmt.Errorf("invalid mount args")
			}
			if err := o.Set("args", decoded); err != nil {
				return nil, err
			}
		} else if err := o.Set("args", goja.Undefined()); err != nil {
			return nil, err
		}
		env, err := resolveComponentEnv(namespace)
		if err != nil {
			return nil, err
		}
		if err := o.Set("env", env); err != nil {
			return nil, err
		}
	}
	if fd.Type != deploy.FunctionTypeAction && fd.Type != deploy.FunctionTypeHTTPAction && app != nil {
		canonicalArgs, err := deploy.CanonicalJSON(args)
		if err != nil {
			return nil, fmt.Errorf("invalid function arguments")
		}
		db := databaseScope(ctx, app, manifest, fd)
		db.vm, db.write, db.function, db.args, db.rawArgs, db.argPaths = vm, fd.Type == deploy.FunctionTypeMutation, function, canonicalArgs, args, map[*goja.Object][]string{}
		db.bindArgumentPaths(jsArgs, args, nil, 0)
		dbo := vm.NewObject()
		dbo.Set("get", db.get)
		dbo.Set("normalizeId", db.normalizeID)
		dbo.Set("query", db.query)
		if db.write {
			dbo.Set("insert", db.insert)
			dbo.Set("patch", db.patch)
			dbo.Set("replace", db.replace)
			dbo.Set("delete", db.delete)
		}
		o.Set("db", dbo)
	}
	for _, extender := range extenders {
		if extender != nil {
			if err := extender(vm, ctx, app, fd, o); err != nil {
				return nil, err
			}
		}
	}
	return o, nil
}

// bindArgumentPaths records identity, not a property-name heuristic. A cursor
// continuation can then redact only the precise pagination option consumed by
// paginate; unrelated application fields named "cursor" remain part of the
// authenticated invocation arguments.
func (d *database) bindArgumentPaths(value goja.Value, raw any, path []string, depth int) {
	if value == nil || depth > maxWireDepth {
		return
	}
	o, ok := value.(*goja.Object)
	if !ok || o == nil {
		return
	}
	switch source := raw.(type) {
	case map[string]any:
		if o.ClassName() != "Object" {
			return
		}
		d.argPaths[o] = append([]string(nil), path...)
		for key, item := range source {
			d.bindArgumentPaths(o.Get(key), item, appendPath(path, key), depth+1)
		}
	case []any:
		if o.ClassName() != "Array" {
			return
		}
		d.argPaths[o] = append([]string(nil), path...)
		for i, item := range source {
			key := strconv.Itoa(i)
			d.bindArgumentPaths(o.Get(key), item, appendPath(path, key), depth+1)
		}
	}
}

func appendPath(path []string, item string) []string {
	out := make([]string, len(path)+1)
	copy(out, path)
	out[len(path)] = item
	return out
}

func (d *database) tables() map[string]map[string]any {
	out := map[string]map[string]any{}
	rawSchema := d.schema
	if rawSchema == nil {
		rawSchema = d.manifest.Schema
	}
	s, ok := rawSchema.(map[string]any)
	if !ok {
		return out
	}
	items, _ := s["tables"].([]any)
	for _, raw := range items {
		if t, ok := raw.(map[string]any); ok {
			if n, ok := t["tableName"].(string); ok {
				out[n] = t
			}
		}
	}
	return out
}
func (d *database) physicalTable(table string) (string, error) {
	if _, err := d.table(table); err != nil {
		return "", err
	}
	if d.namespace == deploy.RootNamespace || d.namespace == "" {
		return table, nil
	}
	physical := d.physical[table]
	if physical == "" {
		return "", fmt.Errorf("invalid table")
	}
	return physical, nil
}
func (d *database) sqlTable(table string) string {
	if strings.HasPrefix(d.namespace, "cmp_") {
		if physical := d.physical[table]; physical != "" {
			return physical
		}
	}
	return table
}
func (d *database) table(name string) (map[string]any, error) {
	if !safeTableName(name) || schema.IsReservedCollection(name) || strings.HasPrefix(name, "_") {
		return nil, fmt.Errorf("invalid table")
	}
	t, ok := d.tables()[name]
	if !ok {
		return nil, fmt.Errorf("invalid table")
	}
	return t, nil
}
func (d *database) jsError(err error) {
	panic(d.vm.NewGoError(fmt.Errorf("database error: %s", err.Error())))
}
func (d *database) requestContext() context.Context {
	if d.ctx != nil {
		return d.ctx
	}
	return context.Background()
}
func (d *database) internalRequestContext() context.Context {
	// The public PocketBase record hooks deliberately deny direct API access to
	// generated backing collections, including for superusers. Runtime writes
	// remain an internal capability, but derive from the invocation context so
	// cancellation and deadlines continue to govern the transaction.
	return context.WithValue(d.requestContext(), schema.InternalContextKey, true)
}
func jsString(v goja.Value) (string, bool) {
	s, ok := v.Export().(string)
	return s, ok
}
func (d *database) requireString(v goja.Value, what string) string {
	s, ok := jsString(v)
	if !ok {
		d.jsError(fmt.Errorf("%s must be a string", what))
	}
	return s
}
func (d *database) wire(v goja.Value) (any, error) { return encodeWire(d.vm, v) }
func (d *database) value(v any) goja.Value {
	x, err := decodeWire(d.vm, v)
	if err != nil {
		d.jsError(err)
	}
	return x
}
func (d *database) get(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) != 1 {
		d.jsError(fmt.Errorf("get requires an id"))
	}
	id := d.requireString(call.Argument(0), "id")
	table, rec, err := d.record(id)
	if err != nil {
		d.jsError(err)
	}
	if table == "" || rec == nil {
		return goja.Null()
	}
	return d.value(d.document(table, rec))
}
func (d *database) normalizeID(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) != 2 {
		d.jsError(fmt.Errorf("normalizeId requires table and id"))
	}
	table, id := d.requireString(call.Argument(0), "table"), d.requireString(call.Argument(1), "id")
	if _, err := d.table(table); err != nil {
		d.jsError(err)
	}
	p, err := d.verifyID(id)
	if err != nil || p.T != table {
		return goja.Null()
	}
	// normalizeId validates the opaque capability, not record liveness.  A
	// deleted/dangling id remains a stable value and must not trigger a table
	// scan or turn into a different identifier.
	return d.vm.ToValue(id)
}
func (d *database) record(id string) (string, *core.Record, error) {
	p, err := d.verifyID(id)
	if err != nil {
		return "", nil, fmt.Errorf("invalid id")
	}
	physical, err := d.physicalTable(p.T)
	if err != nil {
		return "", nil, err
	}
	r := &core.Record{}
	err = d.app.RecordQuery(physical).WithContext(d.requestContext()).AndWhere(dbx.HashExp{physical + ".id": p.R}).Limit(1).One(r)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p.T, nil, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || d.requestContext().Err() != nil {
			return p.T, nil, d.requestContext().Err()
		}
		return p.T, nil, fmt.Errorf("database unavailable")
	}
	return p.T, r, nil
}

func (d *database) loadKeyRing() (*keyRing, error) {
	if d.keys != nil {
		return d.keys, nil
	}
	if d.app == nil {
		return nil, fmt.Errorf("id unavailable")
	}
	state := &core.Record{}
	err := d.app.RecordQuery(schema.CollectionSchemaState).
		WithContext(d.requestContext()).
		AndWhere(dbx.HashExp{schema.CollectionSchemaState + "." + schema.FieldKey: schema.StateKeyActive}).
		Limit(1).
		One(state)
	if err != nil {
		if d.requestContext().Err() != nil {
			return nil, d.requestContext().Err()
		}
		return nil, fmt.Errorf("id unavailable")
	}
	decode := func(encoded string) ([]byte, error) {
		key, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(key) < 32 {
			return nil, fmt.Errorf("id unavailable")
		}
		return key, nil
	}
	current, err := decode(state.GetString(schema.FieldCursorSecret))
	if err != nil {
		return nil, err
	}
	var previous []byte
	if encoded := state.GetString(schema.FieldCursorPreviousSecret); encoded != "" {
		previous, err = decode(encoded)
		if err != nil {
			return nil, err
		}
	}
	keyID := state.GetInt(schema.FieldCursorKeyID)
	id, err := decode(state.GetString(schema.FieldIDSecret))
	if err != nil {
		return nil, err
	}
	legacy, err := decode(state.GetString(schema.FieldLegacyIDSecret))
	if err != nil {
		return nil, err
	}
	idKeyID := state.GetInt(schema.FieldIDKeyID)
	if keyID < 1 || idKeyID < 1 || state.Id == "" {
		return nil, fmt.Errorf("id unavailable")
	}
	namespace := d.namespace
	if namespace == "" {
		namespace = deploy.RootNamespace
	}
	d.keys = &keyRing{namespace: namespace, legacyNamespace: state.Id, currentID: keyID, current: current, previous: previous, idKeyID: idKeyID, id: id, legacy: legacy}
	return d.keys, nil
}

func idMAC(key, payload []byte) []byte {
	return schema.OpaqueIDMAC(key, payload)
}

func (d *database) encodeID(table, raw string) (string, error) {
	if len(raw) != 15 {
		return "", fmt.Errorf("invalid id")
	}
	keys, err := d.loadKeyRing()
	if err != nil {
		return "", err
	}
	version, prefix := 2, "pbv2"
	p := opaqueIDPayload{V: version, K: keys.idKeyID, N: keys.namespace, T: table, R: raw}
	payload, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("id unavailable")
	}
	return prefix + "." + strconv.Itoa(p.K) + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(schema.OpaqueIDVersionMAC(keys.id, p.K, payload)), nil
}

func (d *database) verifyID(id string) (opaqueIDPayload, error) {
	p, payload, supplied, ok := parseOpaqueID(id)
	if !ok {
		return opaqueIDPayload{}, fmt.Errorf("invalid id")
	}
	keys, err := d.loadKeyRing()
	if err != nil {
		return opaqueIDPayload{}, err
	}
	validNamespace := p.V == 2 && p.N == keys.namespace
	if p.V == 1 && keys.namespace == deploy.RootNamespace && p.N == keys.legacyNamespace {
		validNamespace = true
	}
	if !validNamespace {
		return opaqueIDPayload{}, fmt.Errorf("invalid id")
	}
	// New IDs derive their signing key from the permanent identity root and
	// embedded version. The root is retained indefinitely, so arbitrary future
	// version/cursor rotations cannot invalidate a document id or persisted
	// v.id reference. Accept the direct-root form only for IDs emitted before
	// version derivation was introduced.
	if hmac.Equal(supplied, schema.OpaqueIDVersionMAC(keys.id, p.K, payload)) || hmac.Equal(supplied, idMAC(keys.id, payload)) {
		return p, nil
	}
	if len(keys.legacy) >= 32 && hmac.Equal(supplied, idMAC(keys.legacy, payload)) {
		return p, nil
	}
	key := keys.current
	if p.K != keys.currentID {
		if p.K != keys.currentID-1 || len(keys.previous) == 0 {
			return opaqueIDPayload{}, fmt.Errorf("invalid id")
		}
		key = keys.previous
	}
	if !hmac.Equal(supplied, idMAC(key, payload)) {
		return opaqueIDPayload{}, fmt.Errorf("invalid id")
	}
	return p, nil
}
func (d *database) insert(call goja.FunctionCall) goja.Value {
	if !d.write {
		d.jsError(fmt.Errorf("read-only database"))
	}
	if len(call.Arguments) != 2 {
		d.jsError(fmt.Errorf("insert requires table and value"))
	}
	table := d.requireString(call.Argument(0), "table")
	t, err := d.table(table)
	if err != nil {
		d.jsError(err)
	}
	data, err := d.wire(call.Argument(1))
	if err != nil {
		d.jsError(err)
	}
	obj, ok := data.(map[string]any)
	if !ok {
		d.jsError(fmt.Errorf("document must be an object"))
	}
	obj, err = d.normalizeDocument(t, obj, false)
	if err != nil {
		d.jsError(err)
	}
	physical, err := d.physicalTable(table)
	if err != nil {
		d.jsError(err)
	}
	col, err := d.app.FindCollectionByNameOrId(physical)
	if err != nil || col == nil {
		d.jsError(fmt.Errorf("write failed"))
	}
	r := core.NewRecord(col)
	r.Set("created", types.NowDateTime())
	r.Set(documentDataField, obj)
	projection, err := d.orderProjection(t, obj)
	if err != nil {
		d.jsError(err)
	}
	r.Set(schema.DocumentOrderField, projection)
	if err := d.app.SaveWithContext(d.internalRequestContext(), r); err != nil {
		d.jsError(fmt.Errorf("write failed: %w", err))
	}
	id, err := d.encodeID(table, r.Id)
	if err != nil {
		d.jsError(err)
	}
	return d.vm.ToValue(id)
}
func (d *database) patch(call goja.FunctionCall) goja.Value   { return d.writeUpdate(call, false) }
func (d *database) replace(call goja.FunctionCall) goja.Value { return d.writeUpdate(call, true) }
func (d *database) writeUpdate(call goja.FunctionCall, replace bool) goja.Value {
	if !d.write {
		d.jsError(fmt.Errorf("read-only database"))
	}
	if len(call.Arguments) != 2 {
		d.jsError(fmt.Errorf("update requires id and value"))
	}
	id := d.requireString(call.Argument(0), "id")
	table, r, err := d.record(id)
	if err != nil {
		d.jsError(err)
	}
	if r == nil {
		d.jsError(fmt.Errorf("document not found"))
	}
	t, _ := d.table(table)
	data, err := d.wire(call.Argument(1))
	if err != nil {
		d.jsError(err)
	}
	patch, ok := data.(map[string]any)
	if !ok {
		d.jsError(fmt.Errorf("document must be an object"))
	}
	if _, ok := patch["_id"]; ok {
		d.jsError(fmt.Errorf("system fields are immutable"))
	}
	if _, ok := patch["_creationTime"]; ok {
		d.jsError(fmt.Errorf("system fields are immutable"))
	}
	base := map[string]any{}
	if !replace {
		for k, v := range recordData(r) {
			base[k] = v
		}
	}
	for k, v := range patch {
		base[k] = v
	}
	base, err = d.normalizeDocument(t, base, false)
	if err != nil {
		d.jsError(err)
	}
	r.Set(documentDataField, base)
	projection, err := d.orderProjection(t, base)
	if err != nil {
		d.jsError(err)
	}
	r.Set(schema.DocumentOrderField, projection)
	if err := d.app.SaveWithContext(d.internalRequestContext(), r); err != nil {
		d.jsError(fmt.Errorf("write failed: %w", err))
	}
	return goja.Undefined()
}
func (d *database) delete(call goja.FunctionCall) goja.Value {
	if !d.write {
		d.jsError(fmt.Errorf("read-only database"))
	}
	if len(call.Arguments) != 1 {
		d.jsError(fmt.Errorf("delete requires id"))
	}
	id := d.requireString(call.Argument(0), "id")
	_, r, err := d.record(id)
	if err != nil {
		d.jsError(err)
	}
	if r == nil {
		d.jsError(fmt.Errorf("document not found"))
	}
	if err := d.app.DeleteWithContext(d.internalRequestContext(), r); err != nil {
		d.jsError(fmt.Errorf("delete failed"))
	}
	return goja.Undefined()
}
func (d *database) document(table string, r *core.Record) map[string]any {
	id, err := d.encodeID(table, r.Id)
	if err != nil {
		d.jsError(err)
	}
	out := map[string]any{"_id": id, "_creationTime": float64(r.GetDateTime("created").Time().UnixMilli())}
	for k, v := range recordData(r) {
		out[k] = v
	}
	return out
}

func recordData(r *core.Record) map[string]any {
	if data, ok := r.Get(documentDataField).(map[string]any); ok {
		return canonicalRecordData(data)
	}
	if raw, ok := r.Get(documentDataField).(types.JSONRaw); ok {
		var data map[string]any
		if json.Unmarshal(raw, &data) == nil {
			return canonicalRecordData(data)
		}
	}
	if raw, ok := r.Get(documentDataField).(string); ok {
		var data map[string]any
		if json.Unmarshal([]byte(raw), &data) == nil {
			return canonicalRecordData(data)
		}
	}
	return map[string]any{}
}
func canonicalRecordData(data map[string]any) map[string]any {
	// PocketBase's JSON field may surface json.Number values. Re-roundtrip
	// through encoding/json so documents and return-size accounting use the
	// canonical wire representation rather than driver-specific Go types.
	b, err := json.Marshal(data)
	if err != nil {
		return map[string]any{}
	}
	var normalized map[string]any
	if json.Unmarshal(b, &normalized) != nil {
		return map[string]any{}
	}
	return normalized
}

func (d *database) query(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) != 1 {
		d.jsError(fmt.Errorf("query requires a table"))
	}
	table := d.requireString(call.Argument(0), "table")
	if _, err := d.table(table); err != nil {
		d.jsError(err)
	}
	return d.newQuery(&queryBuilder{db: d, table: table})
}
func (d *database) newQuery(q *queryBuilder) goja.Value {
	o := d.vm.NewObject()
	o.Set("filter", func(c goja.FunctionCall) goja.Value { return d.queryFilter(q, c) })
	o.Set("withIndex", func(c goja.FunctionCall) goja.Value { return d.queryIndex(q, c) })
	o.Set("order", func(c goja.FunctionCall) goja.Value { return d.queryOrder(q, c) })
	o.Set("collect", func(c goja.FunctionCall) goja.Value {
		if len(c.Arguments) != 0 {
			d.jsError(fmt.Errorf("collect takes no arguments"))
		}
		return d.documentsFromDocs(d.docs(q))
	})
	o.Set("take", func(c goja.FunctionCall) goja.Value {
		if len(c.Arguments) != 1 {
			d.jsError(fmt.Errorf("take requires a positive limit"))
		}
		n, ok := exactPositiveInteger(c.Argument(0).Export())
		if !ok || n > maxQueryItems {
			d.jsError(fmt.Errorf("invalid take limit"))
		}
		return d.documents(q, n)
	})
	o.Set("first", func(c goja.FunctionCall) goja.Value {
		if len(c.Arguments) != 0 {
			d.jsError(fmt.Errorf("first takes no arguments"))
		}
		v := d.docsLimit(q, 1)
		if len(v) == 0 {
			return goja.Null()
		}
		return d.value(v[0])
	})
	o.Set("unique", func(c goja.FunctionCall) goja.Value {
		if len(c.Arguments) != 0 {
			d.jsError(fmt.Errorf("unique takes no arguments"))
		}
		v := d.docsLimit(q, 2)
		if len(v) > 1 {
			d.jsError(fmt.Errorf("unique query returned more than one document"))
		}
		if len(v) == 0 {
			return goja.Null()
		}
		return d.value(v[0])
	})
	o.Set("paginate", func(c goja.FunctionCall) goja.Value { return d.paginate(q, c) })
	return o
}
func cloneQ(q *queryBuilder) *queryBuilder {
	n := *q
	n.indexFields = append([]string(nil), q.indexFields...)
	n.indexBounds = append([]indexBound(nil), q.indexBounds...)
	return &n
}
func (d *database) queryFilter(q *queryBuilder, c goja.FunctionCall) goja.Value {
	if len(c.Arguments) != 1 {
		d.jsError(fmt.Errorf("filter requires a function"))
	}
	fn, ok := goja.AssertFunction(c.Argument(0))
	if !ok {
		d.jsError(fmt.Errorf("filter requires a function"))
	}
	b := d.expressionBuilder()
	v, err := fn(goja.Undefined(), b)
	if err != nil {
		d.jsError(err)
	}
	e, ok := v.Export().(*expression)
	if !ok {
		d.jsError(fmt.Errorf("invalid filter expression"))
	}
	if err := d.validateExpression(q.table, e); err != nil {
		d.jsError(err)
	}
	n := cloneQ(q)
	if n.predicate != nil {
		n.predicate = &expression{op: "and", args: []*expression{n.predicate, e}}
	} else {
		n.predicate = e
	}
	return d.newQuery(n)
}
func (d *database) expressionBuilder() *goja.Object {
	o := d.vm.NewObject()
	for _, op := range []string{"eq", "neq", "lt", "lte", "gt", "gte", "add", "sub", "mul", "div", "mod", "and", "or"} {
		op := op
		o.Set(op, func(c goja.FunctionCall) goja.Value { return d.expr(op, c) })
	}
	for _, op := range []string{"neg", "not"} {
		op := op
		o.Set(op, func(c goja.FunctionCall) goja.Value { return d.expr(op, c) })
	}
	o.Set("field", func(c goja.FunctionCall) goja.Value {
		if len(c.Arguments) != 1 {
			d.jsError(fmt.Errorf("field requires a name"))
		}
		if _, ok := c.Argument(0).Export().(string); !ok {
			d.jsError(fmt.Errorf("field requires a name"))
		}
		return d.vm.ToValue(&expression{op: "field", field: c.Argument(0).String()})
	})
	o.Set("literal", func(c goja.FunctionCall) goja.Value {
		if len(c.Arguments) != 1 {
			d.jsError(fmt.Errorf("literal requires a value"))
		}
		if goja.IsUndefined(c.Argument(0)) {
			d.jsError(fmt.Errorf("literal requires a wire value"))
		}
		return d.vm.ToValue(&expression{op: "literal", value: mustWire(d, c.Argument(0))})
	})
	return o
}
func mustWire(d *database, v goja.Value) any {
	x, e := d.wire(v)
	if e != nil {
		d.jsError(e)
	}
	return x
}
func (d *database) expr(op string, c goja.FunctionCall) goja.Value {
	want := map[string]int{"eq": 2, "neq": 2, "lt": 2, "lte": 2, "gt": 2, "gte": 2, "add": 2, "sub": 2, "mul": 2, "div": 2, "mod": 2, "neg": 1, "not": 1}[op]
	if (op == "and" || op == "or") && len(c.Arguments) < 2 {
		d.jsError(fmt.Errorf("invalid expression arity"))
	}
	if op != "and" && op != "or" && len(c.Arguments) != want {
		d.jsError(fmt.Errorf("invalid expression arity"))
	}
	e := &expression{op: op}
	for _, v := range c.Arguments {
		x, ok := v.Export().(*expression)
		if (op == "and" || op == "or" || op == "not") && !ok {
			d.jsError(fmt.Errorf("boolean expression required"))
		}
		if !ok {
			x = &expression{op: "literal", value: mustWire(d, v)}
		}
		e.args = append(e.args, x)
	}
	return d.vm.ToValue(e)
}
func (d *database) queryIndex(q *queryBuilder, c goja.FunctionCall) goja.Value {
	if len(c.Arguments) != 1 && len(c.Arguments) != 2 {
		d.jsError(fmt.Errorf("withIndex requires a name and optional callback"))
	}
	if q.index != "" {
		d.jsError(fmt.Errorf("index already selected"))
	}
	name, nameOK := jsString(c.Argument(0))
	if !nameOK {
		d.jsError(fmt.Errorf("invalid index"))
	}
	t, _ := d.table(q.table)
	valid := false
	var fields []string
	for _, raw := range list(t["indexes"]) {
		if i, ok := raw.(map[string]any); ok && i["name"] == name {
			valid = true
			for _, f := range list(i["fields"]) {
				s, ok := f.(string)
				validator, declared, err := d.fieldValidator(q.table, s)
				if !ok || err != nil || !declared || !schema.IndexableValidator(validator) {
					d.jsError(fmt.Errorf("invalid index"))
				}
				fields = append(fields, s)
			}
		}
	}
	if !valid {
		d.jsError(fmt.Errorf("invalid index"))
	}
	n := cloneQ(q)
	n.index = name
	n.indexFields = append([]string(nil), fields...)
	if len(c.Arguments) == 2 {
		fn, ok := goja.AssertFunction(c.Argument(1))
		if !ok {
			d.jsError(fmt.Errorf("withIndex callback must be a function"))
		}
		r := &indexRange{d: d, fields: fields}
		b := r.builder()
		_, err := fn(goja.Undefined(), b)
		if err != nil {
			d.jsError(err)
		}
		if len(r.equal) == 0 && r.lower == nil && r.upper == nil {
			d.jsError(fmt.Errorf("index callback must specify a range"))
		}
		for _, e := range append(append([]*expression{}, r.equal...), r.lower, r.upper) {
			if e == nil {
				continue
			}
			key, err := d.indexKey(q.table, e.args[0].field, e.args[1].value)
			if err != nil {
				d.jsError(err)
			}
			n.indexBounds = append(n.indexBounds, indexBound{field: e.args[0].field, op: e.op, key: key})
		}
	}
	return d.newQuery(n)
}

func (r *indexRange) builder() *goja.Object {
	d := r.d
	o := d.vm.NewObject()
	for _, op := range []string{"eq", "lt", "lte", "gt", "gte"} {
		op := op
		o.Set(op, func(c goja.FunctionCall) goja.Value {
			if len(c.Arguments) != 2 {
				d.jsError(fmt.Errorf("invalid index expression"))
			}
			field, ok := c.Argument(0).Export().(string)
			if !ok {
				d.jsError(fmt.Errorf("invalid index field"))
			}
			if r.pos >= len(r.fields) || field != r.fields[r.pos] {
				d.jsError(fmt.Errorf("invalid index field order"))
			}
			if goja.IsUndefined(c.Argument(1)) {
				d.jsError(fmt.Errorf("invalid index value"))
			}
			value := mustWire(d, c.Argument(1))
			if !orderable(value) {
				d.jsError(fmt.Errorf("invalid index value"))
			}
			e := &expression{op: op, args: []*expression{{op: "field", field: field}, {op: "literal", value: value}}}
			if op == "eq" {
				if r.rangeField != "" {
					d.jsError(fmt.Errorf("equality after range"))
				}
				r.equal = append(r.equal, e)
				r.pos++
			} else {
				if r.rangeField == "" {
					r.rangeField = field
				} else if r.rangeField != field {
					d.jsError(fmt.Errorf("invalid index range"))
				}
				if op == "gt" || op == "gte" {
					if r.lower != nil {
						d.jsError(fmt.Errorf("duplicate lower bound"))
					}
					r.lower = e
				} else {
					if r.upper != nil {
						d.jsError(fmt.Errorf("duplicate upper bound"))
					}
					r.upper = e
				}
			}
			return o
		})
	}
	return o
}
func orderable(v any) bool {
	_, err := schema.GenericOrderKey(v, true)
	return err == nil
}

func (d *database) validateExpression(table string, e *expression) error {
	nodes := 0
	var walk func(*expression, int) (expressionInfo, error)
	walk = func(x *expression, depth int) (expressionInfo, error) {
		nodes++
		if x == nil || depth > maxWireDepth || nodes > maxQueryItems {
			return expressionInfo{}, fmt.Errorf("invalid filter expression")
		}
		switch x.op {
		case "field":
			if len(x.args) != 0 || x.field == "" {
				return expressionInfo{}, fmt.Errorf("invalid filter expression")
			}
			switch x.field {
			case "_id":
				return expressionInfo{kind: "id", field: x.field}, nil
			case "_creationTime":
				return expressionInfo{kind: "number", field: x.field}, nil
			}
			validator, exists, err := d.fieldValidator(table, x.field)
			if err != nil {
				return expressionInfo{}, err
			}
			if !exists {
				return expressionInfo{}, fmt.Errorf("invalid field")
			}
			info := validatorExpressionInfo(validator)
			if info.kind == "unknown" {
				return expressionInfo{}, fmt.Errorf("unsupported field expression")
			}
			info.validator, info.field = validator, x.field
			return info, nil
		case "literal":
			if len(x.args) != 0 || !validateCanonicalWire(x.value) {
				return expressionInfo{}, fmt.Errorf("invalid filter expression")
			}
			info := literalExpressionInfo(x.value)
			if info.kind == "unknown" {
				return expressionInfo{}, fmt.Errorf("invalid filter literal")
			}
			return info, nil
		case "eq", "neq", "lt", "lte", "gt", "gte":
			if len(x.args) != 2 {
				return expressionInfo{}, fmt.Errorf("invalid filter expression")
			}
			left, err := walk(x.args[0], depth+1)
			if err != nil {
				return expressionInfo{}, err
			}
			right, err := walk(x.args[1], depth+1)
			if err != nil {
				return expressionInfo{}, err
			}
			if !compatibleComparison(left, right, x.op) {
				return expressionInfo{}, fmt.Errorf("incompatible comparison operands")
			}
			return expressionInfo{kind: "boolean"}, nil
		case "and", "or":
			if len(x.args) < 2 {
				return expressionInfo{}, fmt.Errorf("invalid filter expression")
			}
			for _, arg := range x.args {
				info, err := walk(arg, depth+1)
				if err != nil || info.kind != "boolean" || info.nullable {
					return expressionInfo{}, fmt.Errorf("non-null boolean expression required")
				}
			}
			return expressionInfo{kind: "boolean"}, nil
		case "not":
			if len(x.args) != 1 {
				return expressionInfo{}, fmt.Errorf("invalid filter expression")
			}
			info, err := walk(x.args[0], depth+1)
			if err != nil || info.kind != "boolean" || info.nullable {
				return expressionInfo{}, fmt.Errorf("non-null boolean expression required")
			}
			return expressionInfo{kind: "boolean"}, nil
		case "neg", "add", "sub", "mul", "div", "mod":
			need := 2
			if x.op == "neg" {
				need = 1
			}
			if len(x.args) != need {
				return expressionInfo{}, fmt.Errorf("invalid filter expression")
			}
			for _, arg := range x.args {
				info, err := walk(arg, depth+1)
				if err != nil || info.kind != "number" || info.nullable {
					return expressionInfo{}, fmt.Errorf("non-null numeric expression required")
				}
			}
			if x.op == "div" || x.op == "mod" {
				rhs := x.args[1]
				if rhs == nil || rhs.op != "literal" {
					return expressionInfo{}, fmt.Errorf("division requires a nonzero literal divisor")
				}
				n, ok := finiteNumber(rhs.value)
				if !ok || n == 0 {
					return expressionInfo{}, fmt.Errorf("division by zero")
				}
			}
			return expressionInfo{kind: "number"}, nil
		default:
			return expressionInfo{}, fmt.Errorf("invalid filter expression")
		}
	}
	info, err := walk(e, 0)
	if err != nil {
		return err
	}
	if info.kind != "boolean" || info.nullable {
		return fmt.Errorf("filter must return a boolean expression")
	}
	return nil
}

func validatorExpressionInfo(validator any) expressionInfo {
	return validatorExpressionInfoAt(validator, 0, map[uintptr]bool{})
}
func validatorExpressionInfoAt(validator any, depth int, stack map[uintptr]bool) expressionInfo {
	if depth > maxWireDepth {
		return expressionInfo{kind: "unknown", nullable: true}
	}
	o, ok := validator.(map[string]any)
	if !ok {
		return expressionInfo{kind: "unknown", nullable: true}
	}
	p := reflect.ValueOf(o).Pointer()
	if p != 0 && stack[p] {
		return expressionInfo{kind: "unknown", nullable: true}
	}
	if p != 0 {
		stack[p] = true
		defer delete(stack, p)
	}
	typ, _ := o["type"].(string)
	switch typ {
	case "optional":
		info := validatorExpressionInfoAt(o["validator"], depth+1, stack)
		info.nullable = true
		return info
	case "defaulted":
		return validatorExpressionInfoAt(o["validator"], depth+1, stack)
	case "number", "float64":
		return expressionInfo{kind: "number"}
	case "boolean":
		return expressionInfo{kind: "boolean"}
	case "string":
		return expressionInfo{kind: "string"}
	case "id":
		return expressionInfo{kind: "id"}
	case "int64":
		return expressionInfo{kind: "int64"}
	case "bytes":
		return expressionInfo{kind: "bytes"}
	case "null":
		return expressionInfo{kind: "null", nullable: true}
	case "any":
		// v.any can hold null, scalars and compound values. It is only
		// comparable for equality/inequality; arithmetic and range checks use
		// compatibleComparison below to reject it.
		return expressionInfo{kind: "any", nullable: true}
	case "object", "array", "record":
		return expressionInfo{kind: "complex"}
	case "literal":
		return literalExpressionInfo(o["value"])
	case "union":
		branches, ok := o["validators"].([]any)
		if !ok || len(branches) == 0 || len(branches) > 64 {
			return expressionInfo{kind: "unknown", nullable: true}
		}
		first := validatorExpressionInfoAt(branches[0], depth+1, stack)
		for _, branch := range branches[1:] {
			next := validatorExpressionInfoAt(branch, depth+1, stack)
			if next.kind != first.kind {
				first.kind = "union"
			}
			first.nullable = first.nullable || next.nullable
		}
		return first
	default:
		return expressionInfo{kind: "unknown", nullable: true}
	}
}
func literalExpressionInfo(value any) expressionInfo {
	switch value.(type) {
	case nil:
		return expressionInfo{kind: "null", nullable: true, literal: value}
	case bool:
		return expressionInfo{kind: "boolean", literal: value}
	case float64, float32, int, int32, int64, uint32:
		return expressionInfo{kind: "number", literal: value}
	case string:
		return expressionInfo{kind: "string", literal: value}
	case map[string]any:
		if schema.ValidateValue(map[string]any{"type": "int64"}, value, nil) {
			return expressionInfo{kind: "int64", literal: value}
		}
		if schema.ValidateValue(map[string]any{"type": "bytes"}, value, nil) {
			return expressionInfo{kind: "bytes", literal: value}
		}
		return expressionInfo{kind: "complex", literal: value}
	case []any:
		return expressionInfo{kind: "complex", literal: value}
	}
	return expressionInfo{kind: "unknown", nullable: true}
}
func compatibleComparison(left, right expressionInfo, op string) bool {
	if left.kind == "unknown" || right.kind == "unknown" {
		return false
	}
	rangeOp := op == "lt" || op == "lte" || op == "gt" || op == "gte"
	if rangeOp {
		// Compound, boolean, null and unconstrained-any values have no
		// protocol range operator. Unions are represented by a canonical total
		// key only for equality; accepting a range here would silently invent a
		// compound ordering contract.
		if left.kind == "any" || right.kind == "any" || left.kind == "complex" || right.kind == "complex" || left.kind == "boolean" || right.kind == "boolean" || left.kind == "null" || right.kind == "null" || left.kind == "union" || right.kind == "union" {
			return false
		}
		if left.kind == right.kind {
			return true
		}
		return (left.kind == "id" && right.kind == "string") || (left.kind == "string" && right.kind == "id")
	}
	// Equality is defined over canonical protocol order keys, including any,
	// objects, arrays, records and compound literals. A concrete id/string
	// comparison is authenticated by semanticComparison before SQL binding.
	if left.kind == "any" || right.kind == "any" || left.kind == "union" || right.kind == "union" {
		return true
	}
	if left.kind == right.kind {
		return true
	}
	return (left.kind == "id" && right.kind == "string") || (left.kind == "string" && right.kind == "id")
}
func finiteNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case float32:
		n := float64(v)
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint32:
		return float64(v), true
	}
	return 0, false
}
func (d *database) queryOrder(q *queryBuilder, c goja.FunctionCall) goja.Value {
	if len(c.Arguments) != 1 {
		d.jsError(fmt.Errorf("order requires a direction"))
	}
	if q.hasOrder {
		d.jsError(fmt.Errorf("order already selected"))
	}
	dir, ok := jsString(c.Argument(0))
	if !ok {
		d.jsError(fmt.Errorf("invalid order"))
	}
	if dir != "asc" && dir != "desc" {
		d.jsError(fmt.Errorf("invalid order"))
	}
	n := cloneQ(q)
	n.descending = dir == "desc"
	n.hasOrder = true
	return d.newQuery(n)
}

type cursorState struct {
	Tuple []any  `json:"t"`
	ID    string `json:"i"`
}

func (d *database) docs(q *queryBuilder) []map[string]any {
	// collect is deliberately bounded. The row stream applies LIMIT max+1 and
	// charges canonical document bytes before anything is materialized in JS.
	docs, _, extra, err := d.boundedDocuments(q, maxQueryItems+1, maxQueryItems, nil)
	if err != nil || extra {
		d.jsError(fmt.Errorf("collect result exceeds limit"))
	}
	return docs
}
func (d *database) docsLimit(q *queryBuilder, limit int) []map[string]any {
	if limit < 1 || limit > maxQueryItems {
		d.jsError(fmt.Errorf("invalid query limit"))
	}
	docs, _, _, err := d.boundedDocuments(q, limit, limit, nil)
	if err != nil {
		d.jsError(fmt.Errorf("query result exceeds limit"))
	}
	return docs
}
func (d *database) recordQuery(q *queryBuilder, limit int, cursor *cursorState) (*dbx.SelectQuery, error) {
	if limit < 1 || limit > maxQueryItems+1 {
		return nil, fmt.Errorf("invalid query limit")
	}
	if err := d.requestContext().Err(); err != nil {
		return nil, err
	}
	physical, err := d.physicalTable(q.table)
	if err != nil {
		return nil, err
	}
	query := d.app.RecordQuery(physical)
	query.WithContext(d.requestContext())
	if len(q.indexBounds) > 0 {
		fragment, params, err := d.indexExpression(q)
		if err != nil {
			return nil, err
		}
		query.AndWhere(dbx.NewExp(fragment, params))
	}
	if q.predicate != nil {
		fragment, params, err := d.sqlExpression(q.table, q.predicate)
		if err != nil {
			return nil, err
		}
		query.AndWhere(dbx.NewExp(fragment, params))
	}
	if cursor != nil {
		fragment, params, err := d.keysetExpression(q, cursor)
		if err != nil {
			return nil, err
		}
		query.AndWhere(dbx.NewExp(fragment, params))
	}
	for _, column := range d.orderColumns(q) {
		direction := "ASC"
		if q.descending {
			direction = "DESC"
		}
		query.AndOrderBy(column + " " + direction)
	}
	query.Limit(int64(limit))
	return query, nil
}

type queryRow struct {
	id      string
	created string
	data    map[string]any
	order   map[string]any
}

// boundedDocuments consumes dbx rows one at a time. It never calls All for a
// terminal operation, and it accounts for canonical document bytes before a
// document is retained for Goja conversion.
func (d *database) boundedDocuments(q *queryBuilder, sqlLimit, documentLimit int, cursor *cursorState) ([]map[string]any, queryRow, bool, error) {
	query, err := d.recordQuery(q, sqlLimit, cursor)
	if err != nil {
		return nil, queryRow{}, false, err
	}
	rows, err := query.Rows()
	if err != nil {
		return nil, queryRow{}, false, err
	}
	defer rows.Close()
	maxBytes := deploy.NormalizeConfig(d.manifest.Config).MaxReturnValueBytes
	// Reserve the array delimiters and commas; the enclosing terminal wrapper
	// can only add bytes, so this is intentionally conservative.
	usedBytes := int64(2)
	docs := make([]map[string]any, 0, documentLimit)
	var last queryRow
	extra := false
	for rows.Next() {
		if err := d.requestContext().Err(); err != nil {
			return nil, queryRow{}, false, err
		}
		values := dbx.NullStringMap{}
		if err := rows.ScanMap(values); err != nil {
			return nil, queryRow{}, false, err
		}
		row, err := decodeQueryRow(values)
		if err != nil {
			return nil, queryRow{}, false, err
		}
		if len(docs) >= documentLimit {
			extra = true
			continue
		}
		doc, err := d.documentFromQueryRow(q.table, row)
		if err != nil {
			return nil, queryRow{}, false, err
		}
		encoded, err := deploy.CanonicalJSON(doc)
		separator := int64(0)
		if len(docs) > 0 {
			separator = 1
		}
		if err != nil || int64(len(encoded))+separator > maxBytes-usedBytes {
			return nil, queryRow{}, false, fmt.Errorf("query result exceeds limit")
		}
		usedBytes += separator
		usedBytes += int64(len(encoded))
		docs = append(docs, doc)
		last = row
	}
	if err := rows.Err(); err != nil {
		return nil, queryRow{}, false, err
	}
	return docs, last, extra, nil
}

func decodeQueryRow(values dbx.NullStringMap) (queryRow, error) {
	get := func(name string) (string, error) {
		value, ok := values[name]
		if !ok || !value.Valid {
			return "", fmt.Errorf("query failed")
		}
		return value.String, nil
	}
	id, err := get("id")
	if err != nil || len(id) != 15 {
		return queryRow{}, fmt.Errorf("query failed")
	}
	created, err := get("created")
	if err != nil {
		return queryRow{}, err
	}
	rawData, err := get(documentDataField)
	if err != nil {
		return queryRow{}, err
	}
	data := map[string]any{}
	if json.Unmarshal([]byte(rawData), &data) != nil {
		return queryRow{}, fmt.Errorf("query failed")
	}
	rawOrder, err := get(schema.DocumentOrderField)
	if err != nil {
		return queryRow{}, err
	}
	order := map[string]any{}
	if json.Unmarshal([]byte(rawOrder), &order) != nil {
		return queryRow{}, fmt.Errorf("query failed")
	}
	return queryRow{id: id, created: created, data: data, order: order}, nil
}

func (d *database) documentFromQueryRow(table string, row queryRow) (map[string]any, error) {
	created, err := types.ParseDateTime(row.created)
	if err != nil {
		return nil, fmt.Errorf("query failed")
	}
	id, err := d.encodeID(table, row.id)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"_id": id, "_creationTime": float64(created.Time().UnixMilli())}
	for key, value := range row.data {
		out[key] = value
	}
	return out, nil
}

func (d *database) orderColumns(q *queryBuilder) []string {
	physical := d.sqlTable(q.table)
	if len(q.indexFields) == 0 {
		return []string{"[[" + physical + ".created]]", "[[" + physical + ".id]]"}
	}
	out := make([]string, 0, len(q.indexFields)+2)
	for _, f := range q.indexFields {
		out = append(out, d.orderColumn(q.table, f))
	}
	// Convex's implicit order after a declared index is _creationTime, not
	// PocketBase's record id. id remains the final deterministic tie-breaker
	// for records created in the same timestamp bucket.
	out = append(out, "[["+physical+".created]]")
	return append(out, "[["+physical+".id]]")
}
func (d *database) fieldColumn(table, field string) string {
	return "json_extract([[" + d.sqlTable(table) + "." + documentDataField + "]], " + schema.SQLiteJSONPathLiteralPath(strings.Split(field, ".")) + ")"
}
func (d *database) orderColumn(table, field string) string {
	return "json_extract([[" + d.sqlTable(table) + "." + schema.DocumentOrderField + "]], " + schema.SQLiteJSONPathLiteral(field) + ")"
}
func (d *database) equalityColumn(table, field string) string {
	return "json_extract([[" + d.sqlTable(table) + "." + schema.DocumentOrderField + "]], " + schema.SQLiteJSONPathLiteral(schema.EqualityProjectionField(field)) + ")"
}
func (d *database) recordTuple(q *queryBuilder, r *core.Record) []any {
	if len(q.indexFields) == 0 {
		return []any{r.GetString("created")}
	}
	projection := recordOrderData(r)
	if projection == nil {
		// An older collection can only reach this fallback while activation is
		// reconciling it.  It remains deterministic and does not scan records.
		t, err := d.table(q.table)
		if err != nil {
			d.jsError(err)
		}
		var errProjection error
		projection, errProjection = d.orderProjection(t, recordData(r))
		if errProjection != nil {
			d.jsError(errProjection)
		}
	}
	out := make([]any, 0, len(q.indexFields)+1)
	for _, f := range q.indexFields {
		key, ok := projection[f].(string)
		if !ok {
			d.jsError(fmt.Errorf("invalid cursor tuple"))
		}
		out = append(out, key)
	}
	out = append(out, r.GetString("created"))
	return out
}
func (d *database) queryRowTuple(q *queryBuilder, row queryRow) []any {
	if len(q.indexFields) == 0 {
		return []any{row.created}
	}
	projection := row.order
	if projection == nil {
		t, err := d.table(q.table)
		if err != nil {
			d.jsError(err)
		}
		projection, err = d.orderProjection(t, row.data)
		if err != nil {
			d.jsError(err)
		}
	}
	out := make([]any, 0, len(q.indexFields)+1)
	for _, field := range q.indexFields {
		key, ok := projection[field].(string)
		if !ok {
			d.jsError(fmt.Errorf("invalid cursor tuple"))
		}
		out = append(out, key)
	}
	out = append(out, row.created)
	return out
}
func recordOrderData(r *core.Record) map[string]any {
	if data, ok := r.Get(schema.DocumentOrderField).(map[string]any); ok {
		return data
	}
	if raw, ok := r.Get(schema.DocumentOrderField).(types.JSONRaw); ok {
		var data map[string]any
		if json.Unmarshal(raw, &data) == nil {
			return data
		}
	}
	if raw, ok := r.Get(schema.DocumentOrderField).(string); ok {
		var data map[string]any
		if json.Unmarshal([]byte(raw), &data) == nil {
			return data
		}
	}
	return nil
}
func (d *database) tableFields(table string) (map[string]any, error) {
	t, err := d.table(table)
	if err != nil {
		return nil, err
	}
	fields, ok := t["fields"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid schema")
	}
	return fields, nil
}

// fieldValidator resolves a canonical q.field path. Top-level manifest keys
// and every constrained-object segment reject dots, so splitting is
// unambiguous and never falls back to a raw JSON path supplied by JavaScript.
func (d *database) fieldValidator(table, path string) (any, bool, error) {
	fields, err := d.tableFields(table)
	if err != nil {
		return nil, false, err
	}
	validator, ok := schema.FieldValidator(fields, path)
	return validator, ok, nil
}
func (d *database) orderProjection(table map[string]any, document map[string]any) (map[string]any, error) {
	fields, ok := table["fields"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid schema")
	}
	projection, err := schema.OrderDataWithID(fields, document, schema.IDChecker(d.validIDForTable))
	if err != nil {
		return nil, fmt.Errorf("invalid indexed value")
	}
	return projection, nil
}
func (d *database) indexKey(table, field string, value any) (string, error) {
	validator, ok, err := d.fieldValidator(table, field)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("invalid field")
	}
	key, err := schema.OrderKeyWithID(validator, value, true, schema.IDChecker(d.validIDForTable))
	if err != nil {
		return "", fmt.Errorf("invalid index value")
	}
	return key, nil
}
func idValidatorTargets(validator any, depth int, stack map[uintptr]bool) []string {
	if depth > maxWireDepth {
		return nil
	}
	o, ok := validator.(map[string]any)
	if !ok {
		return nil
	}
	p := reflect.ValueOf(o).Pointer()
	if p != 0 && stack[p] {
		return nil
	}
	if p != 0 {
		stack[p] = true
		defer delete(stack, p)
	}
	typ, _ := o["type"].(string)
	switch typ {
	case "id":
		if target, ok := o["tableName"].(string); ok && safeTableName(target) {
			return []string{target}
		}
	case "optional", "defaulted":
		return idValidatorTargets(o["validator"], depth+1, stack)
	case "union":
		branches, _ := o["validators"].([]any)
		var out []string
		for _, branch := range branches {
			for _, target := range idValidatorTargets(branch, depth+1, stack) {
				if !containsString(out, target) {
					out = append(out, target)
				}
			}
		}
		return out
	}
	return nil
}
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func (d *database) indexExpression(q *queryBuilder) (string, dbx.Params, error) {
	params := dbx.Params{}
	terms := make([]string, 0, len(q.indexBounds))
	pos := 0
	rangeField := ""
	for i, bound := range q.indexBounds {
		if pos >= len(q.indexFields) || bound.field != q.indexFields[pos] {
			// The index range builder should make this unreachable. Keep the
			// compiler defensive so malformed exported builder state cannot alter
			// an arbitrary SQL plan.
			return "", nil, fmt.Errorf("invalid index range")
		}
		op := map[string]string{"eq": "=", "lt": "<", "lte": "<=", "gt": ">", "gte": ">="}[bound.op]
		if op == "" {
			return "", nil, fmt.Errorf("invalid index range")
		}
		if bound.op == "eq" {
			if rangeField != "" {
				return "", nil, fmt.Errorf("invalid index range")
			}
			pos++
		} else if rangeField == "" {
			rangeField = bound.field
		} else if rangeField != bound.field {
			return "", nil, fmt.Errorf("invalid index range")
		}
		name := fmt.Sprintf("ix%d", i)
		params[name] = bound.key
		terms = append(terms, "("+d.orderColumn(q.table, bound.field)+" "+op+" {:"+name+"})")
	}
	return strings.Join(terms, " AND "), params, nil
}
func (d *database) keysetExpression(q *queryBuilder, cursor *cursorState) (string, dbx.Params, error) {
	columns := d.orderColumns(q)
	// The final id is stored separately; all prior values are the actual index
	// tuple (or created for the default ordering).
	if len(cursor.Tuple) != len(columns)-1 || cursor.ID == "" || len(cursor.ID) != 15 {
		return "", nil, fmt.Errorf("invalid cursor")
	}
	params := dbx.Params{}
	cmp := ">"
	if q.descending {
		cmp = "<"
	}
	var branches []string
	for i := range columns {
		var terms []string
		for j := 0; j < i; j++ {
			key := fmt.Sprintf("k%d", j)
			value, ok := cursor.Tuple[j].(string)
			if !ok || value == "" || len(value) > 4096 {
				return "", nil, fmt.Errorf("invalid cursor")
			}
			params[key] = value
			terms = append(terms, "("+columns[j]+" = {:"+key+"})")
		}
		key := fmt.Sprintf("k%d", i)
		if i == len(columns)-1 {
			params[key] = cursor.ID
		} else {
			params[key] = cursor.Tuple[i]
		}
		terms = append(terms, "("+columns[i]+" "+cmp+" {:"+key+"})")
		branches = append(branches, "("+strings.Join(terms, " AND ")+")")
	}
	return "(" + strings.Join(branches, " OR ") + ")", params, nil
}

// sqlExpression only emits SQL templates owned by this package. Values are
// always dbx parameters; names have already passed manifest validation.
func (d *database) sqlExpression(table string, e *expression) (string, dbx.Params, error) {
	params := dbx.Params{}
	next := 0
	var build func(*expression) (string, error)
	build = func(x *expression) (string, error) {
		if x == nil {
			return "", fmt.Errorf("invalid filter expression")
		}
		if x.op == "eq" || x.op == "neq" || x.op == "lt" || x.op == "lte" || x.op == "gt" || x.op == "gte" {
			if len(x.args) != 2 {
				return "", fmt.Errorf("invalid filter expression")
			}
			if x.args[0] != nil && x.args[0].op == "literal" && x.args[1] != nil && x.args[1].op == "literal" {
				matched, err := protocolLiteralComparison(x.args[0].value, x.args[1].value, x.op)
				if err != nil {
					return "", err
				}
				if matched {
					return "(1 = 1)", nil
				}
				return "(1 = 0)", nil
			}
			if comparison, ok, err := d.semanticComparison(table, x); err != nil {
				return "", err
			} else if ok {
				for name, value := range comparison.params {
					boundName := fmt.Sprintf("v%d", next)
					next++
					if _, exists := params[boundName]; exists {
						return "", fmt.Errorf("invalid filter expression")
					}
					comparison.left = strings.ReplaceAll(comparison.left, "{:"+name+"}", "{:"+boundName+"}")
					comparison.right = strings.ReplaceAll(comparison.right, "{:"+name+"}", "{:"+boundName+"}")
					params[boundName] = value
				}
				return "((" + comparison.left + ") " + comparison.op + " (" + comparison.right + "))", nil
			}
		}
		if x.op == "field" {
			if x.field == "_id" {
				return "[[" + d.sqlTable(table) + ".id]]", nil
			}
			if x.field == "_creationTime" {
				return d.creationTimeColumn(table), nil
			}
			return d.fieldColumn(table, x.field), nil
		}
		if x.op == "literal" {
			key := fmt.Sprintf("v%d", next)
			next++
			params[key] = x.value
			return "{:" + key + "}", nil
		}
		operators := map[string]string{"eq": "=", "neq": "!=", "lt": "<", "lte": "<=", "gt": ">", "gte": ">=", "add": "+", "sub": "-", "mul": "*", "div": "/", "and": "AND", "or": "OR"}
		if x.op == "not" || x.op == "neg" {
			a, err := build(x.args[0])
			if err != nil {
				return "", err
			}
			if x.op == "not" {
				return "(NOT (" + a + "))", nil
			}
			return "(-(" + a + "))", nil
		}
		op, ok := operators[x.op]
		if !ok {
			if x.op == "mod" && len(x.args) == 2 {
				a, err := build(x.args[0])
				if err != nil {
					return "", err
				}
				b, err := build(x.args[1])
				if err != nil {
					return "", err
				}
				// SQLite's % truncates to integers. The protocol's remainder is
				// floating-point, so compile the mathematical definition instead.
				return "((" + a + ") - ((" + b + ") * floor((" + a + ") / (" + b + "))))", nil
			}
			return "", fmt.Errorf("invalid filter expression")
		}
		if x.op == "and" || x.op == "or" {
			if len(x.args) < 2 {
				return "", fmt.Errorf("invalid filter expression")
			}
			parts := make([]string, 0, len(x.args))
			for _, arg := range x.args {
				part, err := build(arg)
				if err != nil {
					return "", err
				}
				parts = append(parts, "("+part+")")
			}
			return "(" + strings.Join(parts, " "+op+" ") + ")", nil
		}
		a, err := build(x.args[0])
		if err != nil {
			return "", err
		}
		b, err := build(x.args[1])
		if err != nil {
			return "", err
		}
		return "((" + a + ") " + op + " (" + b + "))", nil
	}
	s, err := build(e)
	return s, params, err
}

type comparisonPlan struct {
	left, right string
	op          string
	params      dbx.Params
}

// semanticComparison routes direct field/literal and field/field comparisons
// through protocol representations rather than SQLite's JSON/null coercions.
// All user values remain bound dbx parameters.
func (d *database) semanticComparison(table string, x *expression) (comparisonPlan, bool, error) {
	left, right := x.args[0], x.args[1]
	if left == nil || right == nil {
		return comparisonPlan{}, false, fmt.Errorf("invalid filter expression")
	}
	op := comparisonOperator(x.op)
	if op == "" {
		return comparisonPlan{}, false, fmt.Errorf("invalid filter expression")
	}
	if left.op == "field" && right.op == "literal" {
		plan, err := d.fieldLiteralComparison(table, left.field, right.value, op)
		return plan, true, err
	}
	if left.op == "literal" && right.op == "field" {
		plan, err := d.fieldLiteralComparison(table, right.field, left.value, reverseComparisonOperator(op))
		return plan, true, err
	}
	if left.op != "field" || right.op != "field" {
		return comparisonPlan{}, false, nil
	}
	return d.fieldFieldComparison(table, left.field, right.field, op)
}
func comparisonOperator(op string) string {
	return map[string]string{"eq": "=", "neq": "!=", "lt": "<", "lte": "<=", "gt": ">", "gte": ">="}[op]
}
func reverseComparisonOperator(op string) string {
	return map[string]string{"<": ">", "<=": ">=", ">": "<", ">=": "<=", "=": "=", "!=": "!="}[op]
}
func (d *database) fieldLiteralComparison(table, field string, value any, op string) (comparisonPlan, error) {
	params := dbx.Params{"v0": nil}
	switch field {
	case "_id":
		id, ok := value.(string)
		if !ok {
			return comparisonPlan{}, fmt.Errorf("invalid id comparison")
		}
		payload, err := d.verifyID(id)
		if err != nil || payload.T != table {
			return comparisonPlan{}, fmt.Errorf("invalid id comparison")
		}
		params["v0"] = schema.OpaqueIDOrderKey(payload.T, payload.R)
		return comparisonPlan{left: d.systemIDOrderColumn(table), right: "{:v0}", op: op, params: params}, nil
	case "_creationTime":
		ms, ok := finiteNumber(value)
		if !ok {
			return comparisonPlan{}, fmt.Errorf("invalid creation time comparison")
		}
		params["v0"] = ms
		return comparisonPlan{left: d.creationTimeColumn(table), right: "{:v0}", op: op, params: params}, nil
	default:
		key, err := d.indexKey(table, field, value)
		if err != nil {
			return comparisonPlan{}, err
		}
		params["v0"] = key
		return comparisonPlan{left: d.orderColumn(table, field), right: "{:v0}", op: op, params: params}, nil
	}
}
func (d *database) fieldFieldComparison(table, left, right, op string) (comparisonPlan, bool, error) {
	if left == "_id" && right == "_id" {
		column := d.systemIDOrderColumn(table)
		return comparisonPlan{left: column, right: column, op: op}, true, nil
	}
	if left == "_creationTime" && right == "_creationTime" {
		column := d.creationTimeColumn(table)
		return comparisonPlan{left: column, right: column, op: op}, true, nil
	}
	if left == "_id" || right == "_id" {
		other := right
		if right == "_id" {
			other = left
		}
		validator, ok, err := d.fieldValidator(table, other)
		if err != nil {
			return comparisonPlan{}, true, err
		}
		if !ok || !containsString(idValidatorTargets(validator, 0, map[uintptr]bool{}), table) {
			return comparisonPlan{}, true, fmt.Errorf("incompatible system field comparison")
		}
		if left == "_id" {
			return comparisonPlan{left: d.systemIDOrderColumn(table), right: d.orderColumn(table, right), op: op}, true, nil
		}
		return comparisonPlan{left: d.orderColumn(table, left), right: d.systemIDOrderColumn(table), op: op}, true, nil
	}
	if left == "_creationTime" || right == "_creationTime" {
		return comparisonPlan{}, true, fmt.Errorf("incompatible system field comparison")
	}
	if _, ok, err := d.fieldValidator(table, left); err != nil {
		return comparisonPlan{}, true, err
	} else if !ok {
		return comparisonPlan{}, true, fmt.Errorf("invalid field")
	}
	if _, ok, err := d.fieldValidator(table, right); err != nil {
		return comparisonPlan{}, true, err
	} else if !ok {
		return comparisonPlan{}, true, fmt.Errorf("invalid field")
	}
	// Equality is defined over canonical wire values, not over the
	// validator-specific sort rank. In particular the same opaque ID token is
	// equal when stored through v.id, v.string, v.any, or a compatible union.
	if op == "=" || op == "!=" {
		return comparisonPlan{left: d.equalityColumn(table, left), right: d.equalityColumn(table, right), op: op}, true, nil
	}
	return comparisonPlan{left: d.orderColumn(table, left), right: d.orderColumn(table, right), op: op}, true, nil
}
func (d *database) creationTimeColumn(table string) string {
	// PocketBase persists created values in its fixed UTC millisecond layout.
	// Extracting the integer millisecond protocol value avoids comparing a JS
	// Unix timestamp with a datetime string and keeps range boundaries exact.
	physical := d.sqlTable(table)
	return "(CAST(strftime('%s', [[" + physical + ".created]]) AS INTEGER) * 1000 + CAST(substr([[" + physical + ".created]], 21, 3) AS INTEGER))"
}
func (d *database) systemIDOrderColumn(table string) string {
	// The opaque transport text contains a MAC/key version and is deliberately
	// not SQL-comparable. Project the immutable table/raw-id tuple into the same
	// canonical ID key stored for declared v.id fields instead.
	prefix := schema.OpaqueIDOrderKey(table, "")
	return "('" + prefix + "' || lower(hex([[" + d.sqlTable(table) + ".id]])))"
}
func protocolLiteralComparison(left, right any, op string) (bool, error) {
	if op == "eq" {
		return equal(left, right), nil
	}
	if op == "neq" {
		return !equal(left, right), nil
	}
	leftKey, err := schema.GenericOrderKey(left, true)
	if err != nil {
		return false, fmt.Errorf("invalid comparison literal")
	}
	rightKey, err := schema.GenericOrderKey(right, true)
	if err != nil {
		return false, fmt.Errorf("invalid comparison literal")
	}
	cmp := strings.Compare(leftKey, rightKey)
	switch op {
	case "lt":
		return cmp < 0, nil
	case "lte":
		return cmp <= 0, nil
	case "gt":
		return cmp > 0, nil
	case "gte":
		return cmp >= 0, nil
	default:
		return false, fmt.Errorf("invalid filter expression")
	}
}
func (d *database) documents(q *queryBuilder, n int) goja.Value {
	v := d.docsLimit(q, n)
	if n > maxQueryItems {
		n = maxQueryItems
	}
	if n > 0 && len(v) > n {
		v = v[:n]
	}
	// goja's NewArray is variadic in elements, not length; pass nothing and
	// populate via index Set so an empty result is [] rather than [0].
	a := d.vm.NewArray()
	for i, x := range v {
		a.Set(fmt.Sprint(i), d.value(x))
	}
	return a
}
func (d *database) documentsFromDocs(v []map[string]any) goja.Value {
	a := d.vm.NewArray()
	for i, x := range v {
		a.Set(fmt.Sprint(i), d.value(x))
	}
	return a
}
func (d *database) paginate(q *queryBuilder, c goja.FunctionCall) goja.Value {
	if len(c.Arguments) != 1 {
		d.jsError(fmt.Errorf("paginate requires options"))
	}
	options, ok := c.Argument(0).(*goja.Object)
	if !ok || options == nil || options.ClassName() != "Object" {
		d.jsError(fmt.Errorf("invalid pagination options"))
	}
	objectPrototype := d.vm.Get("Object").ToObject(d.vm).Get("prototype").ToObject(d.vm)
	if options.Prototype() != objectPrototype {
		d.jsError(fmt.Errorf("invalid pagination options"))
	}
	// Keys() only reports enumerable properties. Pagination accepts an exact
	// data envelope, so a hidden own property must not be able to smuggle an
	// alternate option shape past validation.
	keys := options.GetOwnPropertyNames()
	if len(options.Symbols()) != 0 {
		d.jsError(fmt.Errorf("invalid pagination options"))
	}
	if len(keys) < 1 || len(keys) > 2 {
		d.jsError(fmt.Errorf("invalid pagination options"))
	}
	hasNumItems := false
	for _, key := range keys {
		if key != "numItems" && key != "cursor" {
			d.jsError(fmt.Errorf("invalid pagination options"))
		}
		if key == "numItems" {
			hasNumItems = true
		}
	}
	nRaw := options.Get("numItems")
	n, integer := exactPositiveInteger(nRaw.Export())
	if !hasNumItems || goja.IsUndefined(nRaw) || !integer || n > maxQueryItems {
		d.jsError(fmt.Errorf("invalid page size"))
	}
	hash := queryHash(q, n)
	var state *cursorState
	if contains(keys, "cursor") {
		raw := options.Get("cursor")
		if goja.IsUndefined(raw) {
			d.jsError(fmt.Errorf("invalid cursor"))
		}
		if !goja.IsNull(raw) {
			s, ok := jsString(raw)
			if !ok || s == "" || len(s) > base64.RawURLEncoding.EncodedLen(maxCursorBytes) {
				d.jsError(fmt.Errorf("invalid cursor"))
			}
			bindings := d.cursorArgumentBindings(options, s)
			hashes := make([]string, 0, len(bindings))
			for _, binding := range bindings {
				hashes = append(hashes, queryHashWithArgs(q, n, binding))
			}
			state, hash = d.decodeCursorCandidates(s, hashes...)
		}
	}
	page, last, extra, err := d.boundedDocuments(q, n+1, n, state)
	if err != nil {
		d.jsError(fmt.Errorf("query result exceeds limit"))
	}
	o := d.vm.NewObject()
	a := d.vm.NewArray()
	for i, x := range page {
		a.Set(fmt.Sprint(i), d.value(x))
	}
	o.Set("page", a)
	o.Set("isDone", !extra)
	if extra {
		o.Set("continueCursor", d.encodeCursor(hash, &cursorState{Tuple: d.queryRowTuple(q, last), ID: last.id}))
	} else {
		o.Set("continueCursor", "")
	}
	return o
}
func exactPositiveInteger(value any) (int, bool) {
	var n int64
	switch v := value.(type) {
	case int:
		n = int64(v)
	case int32:
		n = int64(v)
	case int64:
		n = v
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v || v > float64(math.MaxInt) || v < 1 {
			return 0, false
		}
		n = int64(v)
	default:
		return 0, false
	}
	if n < 1 || n > int64(math.MaxInt) {
		return 0, false
	}
	return int(n), true
}

// cursorArgumentBindings binds a continuation to the exact options object
// consumed by paginate. It intentionally does not search values recursively:
// a nested application field named cursor (or merely holding the same string)
// is not pagination state and must remain authenticated.
func (d *database) cursorArgumentBindings(options *goja.Object, token string) []string {
	bindings := []string{d.args}
	path, ok := d.argPaths[options]
	if !ok {
		var found bool
		path, found = uniqueArgumentCursorPath(d.rawArgs, token, nil, 0)
		if !found {
			return bindings
		}
	}
	for _, omit := range []bool{false, true} {
		redacted, changed := redactArgumentCursor(d.rawArgs, path, token, omit)
		if !changed {
			continue
		}
		canonical, err := deploy.CanonicalJSON(redacted)
		if err == nil && !containsString(bindings, canonical) {
			bindings = append(bindings, canonical)
		}
	}
	return bindings
}

// uniqueArgumentCursorPath is the safe fallback for a handler that creates a
// fresh options object (for example `{cursor: args.cursor}`). It only considers
// actual properties named cursor and refuses ambiguity instead of deleting all
// equal strings throughout application data.
func uniqueArgumentCursorPath(value any, token string, path []string, depth int) ([]string, bool) {
	if depth > maxWireDepth {
		return nil, false
	}
	var candidate []string
	var count int
	var walk func(any, []string, int)
	walk = func(current any, currentPath []string, currentDepth int) {
		if currentDepth > maxWireDepth || count > 1 {
			return
		}
		switch source := current.(type) {
		case map[string]any:
			if cursor, ok := source["cursor"].(string); ok && cursor == token {
				candidate = append([]string(nil), currentPath...)
				count++
			}
			for key, child := range source {
				if key == "cursor" {
					continue
				}
				walk(child, appendPath(currentPath, key), currentDepth+1)
			}
		case []any:
			for i, child := range source {
				walk(child, appendPath(currentPath, strconv.Itoa(i)), currentDepth+1)
			}
		}
	}
	walk(value, path, depth)
	return candidate, count == 1
}

func redactArgumentCursor(value any, path []string, token string, omit bool) (any, bool) {
	if len(path) == 0 {
		object, ok := value.(map[string]any)
		if !ok {
			return value, false
		}
		cursor, present := object["cursor"]
		if !present || cursor != token {
			return value, false
		}
		out := make(map[string]any, len(object))
		for key, item := range object {
			if key != "cursor" {
				out[key] = item
			}
		}
		if !omit {
			out["cursor"] = nil
		}
		return out, true
	}
	key := path[0]
	switch source := value.(type) {
	case map[string]any:
		child, exists := source[key]
		if !exists {
			return value, false
		}
		normalized, changed := redactArgumentCursor(child, path[1:], token, omit)
		if !changed {
			return value, false
		}
		out := make(map[string]any, len(source))
		for name, item := range source {
			out[name] = item
		}
		out[key] = normalized
		return out, true
	case []any:
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 || index >= len(source) {
			return value, false
		}
		normalized, changed := redactArgumentCursor(source[index], path[1:], token, omit)
		if !changed {
			return value, false
		}
		out := append([]any(nil), source...)
		out[index] = normalized
		return out, true
	default:
		return value, false
	}
}
func queryHash(q *queryBuilder, pageSize int) string {
	return queryHashWithArgs(q, pageSize, q.db.args)
}
func queryHashWithArgs(q *queryBuilder, pageSize int, args string) string {
	fields := make([]any, len(q.indexFields))
	for i, f := range q.indexFields {
		fields[i] = f
	}
	bounds := make([]any, len(q.indexBounds))
	for i, bound := range q.indexBounds {
		bounds[i] = map[string]any{"field": bound.field, "op": bound.op, "key": bound.key}
	}
	plan := map[string]any{"deployment": q.db.manifest.DeploymentID, "namespace": q.db.namespace, "function": q.db.function, "args": args, "table": q.table, "index": q.index, "indexFields": fields, "bounds": bounds, "desc": q.descending, "pageSize": pageSize, "predicate": expressionPlan(q.predicate)}
	b, err := deploy.CanonicalJSON(plan)
	if err != nil {
		return "invalid-plan"
	}
	h := sha256.Sum256([]byte(b))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func expressionPlan(e *expression) any {
	if e == nil {
		return nil
	}
	out := map[string]any{"op": e.op}
	if e.field != "" {
		out["field"] = e.field
	}
	if e.op == "literal" {
		out["value"] = e.value
	}
	if len(e.args) > 0 {
		a := make([]any, len(e.args))
		for i, x := range e.args {
			a[i] = expressionPlan(x)
		}
		out["args"] = a
	}
	return out
}

type cursorPayload struct {
	V int    `json:"v"`
	K int    `json:"k"`
	H string `json:"h"`
	T []any  `json:"t"`
	I string `json:"i"`
	M string `json:"m"`
}

func (d *database) cursorKey(keyID int) []byte {
	keys, err := d.loadKeyRing()
	if err != nil {
		d.jsError(fmt.Errorf("cursor unavailable"))
	}
	if keyID == 0 || keyID == keys.currentID {
		return keys.current
	}
	if keyID == keys.currentID-1 && len(keys.previous) >= 32 {
		return keys.previous
	}
	d.jsError(fmt.Errorf("invalid cursor"))
	return nil
}
func (d *database) cursorMAC(p cursorPayload) []byte {
	p.M = ""
	b, _ := json.Marshal(p)
	m := hmac.New(sha256.New, d.cursorKey(p.K))
	m.Write([]byte("pbvex:cursor:v1:"))
	m.Write(b)
	return m.Sum(nil)
}
func (d *database) encodeCursor(hash string, state *cursorState) string {
	p := cursorPayload{V: 1, K: d.cursorKeyID(), H: hash, T: state.Tuple, I: state.ID}
	p.M = base64.RawURLEncoding.EncodeToString(d.cursorMAC(p))
	b, _ := json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(b)
}
func (d *database) decodeCursor(raw, hash string) *cursorState {
	state, _ := d.decodeCursorCandidates(raw, hash)
	return state
}
func (d *database) decodeCursorCandidates(raw string, hashes ...string) (*cursorState, string) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) == 0 || len(b) > maxCursorBytes || base64.RawURLEncoding.EncodeToString(b) != raw {
		d.jsError(fmt.Errorf("invalid cursor"))
	}
	var p cursorPayload
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&p) != nil || dec.Decode(&struct{}{}) != io.EOF || p.V != 1 || p.K < 1 || !containsString(hashes, p.H) || len(p.H) != base64.RawURLEncoding.EncodedLen(sha256.Size) || len(p.I) != 15 || len(p.T) > maxCursorTupleItems {
		d.jsError(fmt.Errorf("invalid cursor"))
	}
	for _, value := range p.T {
		s, ok := value.(string)
		if !ok || s == "" || len(s) > 4096 {
			d.jsError(fmt.Errorf("invalid cursor"))
		}
	}
	canonical, _ := json.Marshal(p)
	if !bytes.Equal(b, canonical) {
		d.jsError(fmt.Errorf("invalid cursor"))
	}
	supplied, err := base64.RawURLEncoding.DecodeString(p.M)
	if err != nil || len(supplied) != sha256.Size || base64.RawURLEncoding.EncodeToString(supplied) != p.M || !hmac.Equal(supplied, d.cursorMAC(p)) {
		d.jsError(fmt.Errorf("invalid cursor"))
	}
	return &cursorState{Tuple: p.T, ID: p.I}, p.H
}
func (d *database) cursorKeyID() int {
	keys, err := d.loadKeyRing()
	if err != nil {
		d.jsError(fmt.Errorf("cursor unavailable"))
	}
	return keys.currentID
}
func list(v any) []any { a, _ := v.([]any); return a }
func equal(a, b any) bool {
	x, errX := deploy.CanonicalJSON(a)
	y, errY := deploy.CanonicalJSON(b)
	return errX == nil && errY == nil && x == y
}

func validateDocument(table map[string]any, doc map[string]any, partial bool) error {
	return validateDocumentWithID(table, doc, partial, nil)
}
func (d *database) validateDocument(table map[string]any, doc map[string]any, partial bool) error {
	return validateDocumentWithID(table, doc, partial, d.validIDForTable)
}
func (d *database) normalizeDocument(table map[string]any, doc map[string]any, partial bool) (map[string]any, error) {
	return normalizeDocumentWithID(table, doc, partial, d.validIDForTable)
}
func validateDocumentWithID(table map[string]any, doc map[string]any, partial bool, check func(string, string) bool) error {
	_, err := normalizeDocumentWithID(table, doc, partial, check)
	return err
}
func normalizeDocumentWithID(table map[string]any, doc map[string]any, partial bool, check func(string, string) bool) (map[string]any, error) {
	fields, ok := table["fields"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid schema")
	}
	return schema.NormalizeDocument(fields, doc, partial, true, schema.IDChecker(check))
}
func (d *database) validIDForTable(id, target string) bool {
	if _, err := d.table(target); err != nil {
		return false
	}
	p, err := d.verifyID(id)
	return err == nil && p.T == target
}
func (d *database) validateReference(v, value any, depth int) error {
	if depth > schema.MaxValidatorDepth || !schema.ValidateValue(v, value, schema.IDChecker(d.validIDForTable)) {
		return fmt.Errorf("invalid value")
	}
	return nil
}
func validateValue(v, value any) bool { return validateValueWithID(v, value, nil) }
func validateValueWithID(v, value any, check func(string, string) bool) bool {
	_, err := normalizeValueWithID(v, value, check)
	return err == nil
}
func normalizeValueWithID(v, value any, check func(string, string) bool) (any, error) {
	return schema.NormalizeValue(v, value, schema.IDChecker(check))
}
func validateValueAt(v, value any, depth int) bool {
	return depth <= schema.MaxValidatorDepth && schema.ValidateValue(v, value, nil)
}

func validateCanonicalWire(value any) bool {
	return schema.CanonicalWire(value)
}
func safeTableName(name string) bool {
	if name == "" || len(name) > deploy.MaxIdentifierLength {
		return false
	}
	for i, c := range []byte(name) {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || (i > 0 && c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}
