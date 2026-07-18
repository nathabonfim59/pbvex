package deploy

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestSchemaValidatorDescriptorSurface(t *testing.T) {
	valid := []any{
		map[string]any{"type": "int64"}, map[string]any{"type": "float64"}, map[string]any{"type": "bytes"},
		map[string]any{"type": "record", "key": map[string]any{"type": "string"}, "value": map[string]any{"type": "int64"}},
		map[string]any{"type": "literal", "value": map[string]any{"$integer": "AQAAAAAAAAA="}},
		map[string]any{"type": "optional", "validator": map[string]any{"type": "bytes"}},
		map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": "x"},
	}
	for _, value := range valid {
		if !validateValidatorDescriptor(value) {
			t.Fatalf("expected valid descriptor %#v", value)
		}
	}
	invalid := []any{
		map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}},
		map[string]any{"type": "defaulted", "validator": map[string]any{"type": "string"}, "defaultValue": float64(1)},
		map[string]any{"type": "delayed"},
		map[string]any{"type": "record", "value": map[string]any{"type": "string"}},
		map[string]any{"type": "record", "key": map[string]any{"type": "string"}, "value": map[string]any{"type": "wat"}},
	}
	for _, value := range invalid {
		if validateValidatorDescriptor(value) {
			t.Fatalf("expected invalid descriptor %#v", value)
		}
	}
}

func TestMigrationManifestContract(t *testing.T) {
	from := map[string]any{"type": "object", "shape": map[string]any{"name": map[string]any{"type": "string"}}}
	to := map[string]any{"type": "object", "shape": map[string]any{"name": map[string]any{"type": "string"}, "active": map[string]any{"type": "boolean"}}}
	fromHash, _ := CanonicalHash(from)
	toHash, _ := CanonicalHash(to)
	descriptor := map[string]any{
		"id": "20260718_add_active", "table": "users", "mode": "transactional",
		"from": from, "to": to, "sourceSchemaHash": fromHash, "targetSchemaHash": toHash,
		"checksum": strings.Repeat("a", 64), "modulePath": "pbvex/migrations/add_active.ts",
		"exportName": "addActive", "reversibility": "reversible",
	}
	manifest := func(entries []any) map[string]any {
		return map[string]any{
			"protocolVersion": "v1", "deploymentId": "migration_manifest", "migrations": entries,
			"schema": map[string]any{"tables": []any{map[string]any{"tableName": "users", "fields": to["shape"]}}},
		}
	}
	parsed, err := ValidateManifest(manifest([]any{descriptor}))
	if err != nil || len(parsed.Migrations) != 1 || parsed.Migrations[0].ID != descriptor["id"] {
		t.Fatalf("valid migration rejected: %#v, %v", parsed.Migrations, err)
	}
	for _, mutate := range []func(map[string]any){
		func(m map[string]any) { m["unexpected"] = true },
		func(m map[string]any) { m["modulePath"] = "pbvex/ordinary.ts" },
		func(m map[string]any) { m["sourceSchemaHash"] = strings.Repeat("0", 64) },
		func(m map[string]any) { m["from"] = map[string]any{"type": "unknown"} },
		func(m map[string]any) { m["from"] = map[string]any{"type": "string"} },
		func(m map[string]any) { m["table"] = "missing" },
	} {
		copy := map[string]any{}
		for key, value := range descriptor {
			copy[key] = value
		}
		mutate(copy)
		if _, err := ValidateManifest(manifest([]any{copy})); err == nil {
			t.Fatalf("invalid migration accepted: %#v", copy)
		}
	}
	duplicate := []any{descriptor, descriptor}
	if _, err := ValidateManifest(manifest(duplicate)); err == nil {
		t.Fatal("duplicate migration accepted")
	}
	second := map[string]any{}
	for key, value := range descriptor {
		second[key] = value
	}
	second["id"] = "100_before"
	if _, err := ValidateManifest(manifest([]any{descriptor, second})); err == nil {
		t.Fatal("unsorted migrations accepted")
	}
}

func TestDeploymentActivateResponseMigrationWarningJSONParity(t *testing.T) {
	response := DeploymentActivateResponse{
		DeploymentID: "dep_warning", ActivatedAt: "2026-07-18T12:00:00Z",
		Warnings: []MigrationWarning{{
			Code: "transactional_migration_utilization", Rows: 8000, RowLimit: 10000,
			EstimatedBytes: 1024, ByteLimit: 64 << 20, UtilizationPercent: 80,
		}},
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	warnings, ok := got["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("warnings JSON = %#v", got["warnings"])
	}
	warning := warnings[0].(map[string]any)
	for _, key := range []string{"code", "rows", "rowLimit", "estimatedBytes", "byteLimit", "utilizationPercent"} {
		if _, ok := warning[key]; !ok {
			t.Fatalf("warning JSON missing %q: %#v", key, warning)
		}
	}
}

func TestSchemaValidationRejectsUnsupportedIndexesAndUnknownIDTargets(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"protocolVersion": "v1", "deploymentId": "schema_limits", "functions": []any{},
			"schema": map[string]any{"tables": []any{map[string]any{
				"tableName": "items", "fields": map[string]any{"value": map[string]any{"type": "any"}},
				"indexes": []any{map[string]any{"name": "by_value", "fields": []any{"value"}}},
			}}},
		}
	}
	if _, err := ValidateManifest(base()); err == nil {
		t.Fatal("any validator was accepted for a physical scalar index")
	}
	unknownID := map[string]any{
		"protocolVersion": "v1", "deploymentId": "unknown_id", "functions": []any{map[string]any{
			"name": "read", "type": "query", "visibility": "public", "modulePath": "read", "exportName": "default",
			"args": map[string]any{"type": "id", "tableName": "missing"},
		}},
		"schema": map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{}}}},
	}
	if _, err := ValidateManifest(unknownID); err == nil {
		t.Fatal("function id target outside active schema was accepted")
	}
	tables := make([]any, maxSchemaTables+1)
	for i := range tables {
		tables[i] = map[string]any{"tableName": fmt.Sprintf("t%d", i), "fields": map[string]any{}}
	}
	tooMany := map[string]any{"protocolVersion": "v1", "deploymentId": "many_tables", "functions": []any{}, "schema": map[string]any{"tables": tables}}
	if _, err := ValidateManifest(tooMany); err == nil {
		t.Fatal("table aggregate limit was accepted")
	}
	reserved := map[string]any{"protocolVersion": "v1", "deploymentId": "reserved_field", "functions": []any{}, "schema": map[string]any{"tables": []any{map[string]any{
		"tableName": "items", "fields": map[string]any{"_id": map[string]any{"type": "string"}},
	}}}}
	if _, err := ValidateManifest(reserved); err == nil {
		t.Fatal("reserved document system field was accepted")
	}
}

func TestManifestRejectsReservedTablesAmbiguousFieldsAndDuplicateFunctions(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"protocolVersion": "v1", "deploymentId": "manifest_negative", "functions": []any{},
			"schema": map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{}}}},
		}
	}
	reserved := base()
	reserved["schema"] = map[string]any{"tables": []any{map[string]any{"tableName": "pbvex_deployments", "fields": map[string]any{}}}}
	if _, err := ValidateManifest(reserved); err == nil {
		t.Fatal("legacy reserved collection name was accepted at upload")
	}
	physicalPrefix := base()
	physicalPrefix["schema"] = map[string]any{"tables": []any{map[string]any{"tableName": "PbVeX_CmP_attacker", "fields": map[string]any{}}}}
	if _, err := ValidateManifest(physicalPrefix); err == nil {
		t.Fatal("generated component physical prefix was accepted as a logical table")
	}
	dotted := base()
	dotted["schema"] = map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{
		"profile.name": map[string]any{"type": "string"},
	}}}}
	if _, err := ValidateManifest(dotted); err == nil {
		t.Fatal("ambiguous dotted top-level field was accepted")
	}
	nestedDotted := base()
	nestedDotted["schema"] = map[string]any{"tables": []any{map[string]any{"tableName": "items", "fields": map[string]any{
		"profile": map[string]any{"type": "object", "shape": map[string]any{"first.last": map[string]any{"type": "string"}}},
	}}}}
	if _, err := ValidateManifest(nestedDotted); err == nil {
		t.Fatal("ambiguous dotted object path was accepted")
	}
	duplicate := base()
	duplicate["functions"] = []any{
		map[string]any{"name": "read", "type": "query", "visibility": "public", "modulePath": "read", "exportName": "default"},
		map[string]any{"name": "read", "type": "query", "visibility": "public", "modulePath": "other", "exportName": "default"},
	}
	if _, err := ValidateManifest(duplicate); err == nil {
		t.Fatal("duplicate canonical function name was accepted")
	}
}

func TestFunctionIDTargetsRequireDeclaredSchemaAndNestedIndexPathsResolve(t *testing.T) {
	function := map[string]any{
		"name": "read", "type": "query", "visibility": "public", "modulePath": "read", "exportName": "default",
		"args": map[string]any{"type": "id", "tableName": "notes"},
	}
	withoutSchema := map[string]any{"protocolVersion": "v1", "deploymentId": "id_without_schema", "functions": []any{function}}
	if _, err := ValidateManifest(withoutSchema); err == nil {
		t.Fatal("function v.id target was accepted without declared tables")
	}
	withoutID := map[string]any{"protocolVersion": "v1", "deploymentId": "schema_free", "functions": []any{map[string]any{
		"name": "read", "type": "query", "visibility": "public", "modulePath": "read", "exportName": "default",
		"args": map[string]any{"type": "string"},
	}}}
	if _, err := ValidateManifest(withoutID); err != nil {
		t.Fatalf("schema-free function without v.id rejected: %v", err)
	}

	nested := map[string]any{
		"protocolVersion": "v1", "deploymentId": "nested_index", "functions": []any{},
		"schema": map[string]any{"tables": []any{map[string]any{
			"tableName": "profiles",
			"fields":    map[string]any{"profile": map[string]any{"type": "object", "shape": map[string]any{"name": map[string]any{"type": "string"}}}},
			"indexes":   []any{map[string]any{"name": "by_name", "fields": []any{"profile.name"}}},
		}}},
	}
	if _, err := ValidateManifest(nested); err != nil {
		t.Fatalf("declared nested index path rejected: %v", err)
	}
	nested["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)["indexes"] = []any{map[string]any{"name": "by_missing", "fields": []any{"profile.missing"}}}
	if _, err := ValidateManifest(nested); err == nil {
		t.Fatal("undeclared nested index path accepted")
	}
}

func TestUploadValidationEnforcesAcceptedContractBoundary(t *testing.T) {
	makeRequest := func(bundle []byte, size int64) map[string]any {
		sum := sha256.Sum256(bundle)
		return map[string]any{
			"manifest": map[string]any{"protocolVersion": "v1", "deploymentId": "bounded_upload", "functions": []any{}},
			"bundle":   base64.StdEncoding.EncodeToString(bundle),
			"sha256":   hex.EncodeToString(sum[:]),
			"size":     size,
		}
	}
	// The accepted v1 contract (ADR 001) is a 64 MiB decoded bundle. A real
	// maximal bundle must round-trip through ValidateUploadRequest so the
	// boundary is proven at the actual contract size, not a scaled proxy.
	exact := make([]byte, MaxDeploymentUploadBytes)
	if _, decoded, err := ValidateUploadRequest(makeRequest(exact, int64(len(exact)))); err != nil || len(decoded) != len(exact) {
		t.Fatalf("accepted max (%d) upload rejected: bytes=%d err=%v", MaxDeploymentUploadBytes, len(decoded), err)
	}
	// max+1 is rejected at the size gate before any base64 allocation, so the
	// over-limit case does not need a second 64 MiB bundle to be meaningful.
	overSize := makeRequest([]byte{}, MaxDeploymentUploadBytes+1)
	if _, _, err := ValidateUploadRequest(overSize); err == nil {
		t.Fatal("max+1 decoded upload accepted")
	}
	// config.maxUploadBytes caps operator overrides at the same contract ceiling.
	if _, err := ValidateManifest(map[string]any{"protocolVersion": "v1", "deploymentId": "bounded_config", "functions": []any{}, "config": map[string]any{"maxUploadBytes": MaxDeploymentUploadBytes}}); err != nil {
		t.Fatalf("config.maxUploadBytes at accepted max rejected: %v", err)
	}
	if _, err := ValidateManifest(map[string]any{"protocolVersion": "v1", "deploymentId": "bounded_config", "functions": []any{}, "config": map[string]any{"maxUploadBytes": MaxDeploymentUploadBytes + 1}}); err == nil {
		t.Fatal("unreachable maxUploadBytes configuration accepted")
	}
}

func TestDeployAcceptsSharedTypeScriptValidatorArtifact(t *testing.T) {
	raw, err := os.ReadFile("../../../fixtures/validators/deployable-record-defaulted.json")
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Descriptor any            `json:"descriptor"`
		Manifest   map[string]any `json:"manifest"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatal(err)
	}
	if !validateValidatorDescriptor(artifact.Descriptor) {
		t.Fatal("shared TypeScript descriptor no longer matches Go validator contract")
	}
	if _, err := ValidateManifest(artifact.Manifest); err != nil {
		t.Fatalf("shared TypeScript artifact is not deployable: %v", err)
	}
}

// TestUploadAcceptsExecutableRecursiveSchema is the end-to-end upload/activation
// gate for recursive types: a full upload (manifest + bundle + sha256 + size)
// carrying a recursive schema must pass deploy validation, while a bare
// {type:'recursive'} marker (no target identity) is rejected. Runtime
// insert/patch validation against the same descriptor is covered by the schema
// package's TestRecursiveDocumentInsertPatch.
func TestUploadAcceptsExecutableRecursiveSchema(t *testing.T) {
	bundle := []byte(`(function(){globalThis.__pbvex_modules=globalThis.__pbvex_modules||{};})();`)
	sum := sha256.Sum256(bundle)
	recursiveSchema := map[string]any{
		"tables": []any{
			map[string]any{
				"tableName": "nodes",
				"fields": map[string]any{
					"tree": map[string]any{
						"type": "recursive", "name": "Node",
						"validator": map[string]any{
							"type": "object", "shape": map[string]any{
								"name":     map[string]any{"type": "string"},
								"children": map[string]any{"type": "array", "item": map[string]any{"type": "ref", "name": "Node"}},
							},
						},
					},
				},
			},
		},
	}
	upload := map[string]any{
		"manifest": map[string]any{
			"protocolVersion": "v1", "deploymentId": "recursive_dep", "functions": []any{}, "schema": recursiveSchema,
		},
		"bundle": base64.StdEncoding.EncodeToString(bundle),
		"sha256": hex.EncodeToString(sum[:]),
		"size":   int64(len(bundle)),
	}
	if _, _, err := ValidateUploadRequest(upload); err != nil {
		t.Fatalf("expected upload with executable recursive schema to be accepted: %v", err)
	}

	// Bare {type:'recursive'} has no target identity and must be rejected.
	badSchema := map[string]any{"tables": []any{map[string]any{
		"tableName": "nodes", "fields": map[string]any{"tree": map[string]any{"type": "recursive"}},
	}}}
	badUpload := map[string]any{
		"manifest": map[string]any{
			"protocolVersion": "v1", "deploymentId": "recursive_bad", "functions": []any{}, "schema": badSchema,
		},
		"bundle": base64.StdEncoding.EncodeToString(bundle),
		"sha256": hex.EncodeToString(sum[:]),
		"size":   int64(len(bundle)),
	}
	if _, _, err := ValidateUploadRequest(badUpload); err == nil {
		t.Fatal("expected bare recursive marker to be rejected at upload")
	}
}

func TestHTTPActionRouteContract(t *testing.T) {
	function := func(kind string, route any) map[string]any {
		fn := map[string]any{"name": "handler", "type": kind, "visibility": "public", "modulePath": "handler", "exportName": "default"}
		if route != nil {
			fn["route"] = route
		}
		return fn
	}
	manifest := func(fn map[string]any) map[string]any {
		return map[string]any{"protocolVersion": "v1", "deploymentId": "route_contract", "functions": []any{fn}}
	}
	for _, route := range []any{map[string]any{"method": "GET", "path": "health"}, map[string]any{"method": "DELETE", "pathPrefix": "hooks/"}} {
		if _, err := ValidateManifest(manifest(function("httpAction", route))); err != nil {
			t.Fatalf("valid route %#v rejected: %v", route, err)
		}
	}
	internal := function("httpAction", map[string]any{"method": "GET", "path": "internal"})
	internal["visibility"] = "internal"
	if _, err := ValidateManifest(manifest(internal)); err == nil {
		t.Fatal("internal httpAction accepted")
	}
	invalid := []any{nil, map[string]any{"method": "get", "path": "health"}, map[string]any{"method": "OPTIONS", "path": "health"}, map[string]any{"method": "GET", "path": ""}, map[string]any{"method": "GET", "path": "health", "pathPrefix": "hooks/"}, map[string]any{"method": "GET", "pathPrefix": "hooks"}, map[string]any{"method": "GET", "path": "/health"}, map[string]any{"method": "GET", "path": 12}, map[string]any{"method": "GET", "path": "health", "unknown": true}}
	for _, route := range invalid {
		if _, err := ValidateManifest(manifest(function("httpAction", route))); err == nil {
			t.Fatalf("invalid route %#v accepted", route)
		}
	}
	for _, segment := range []string{"call", "realtime", "deployments", "jobs", "storage", "admin"} {
		for _, route := range []any{map[string]any{"method": "POST", "path": segment}, map[string]any{"method": "POST", "pathPrefix": segment + "/"}} {
			if _, err := ValidateManifest(manifest(function("httpAction", route))); err == nil {
				t.Fatalf("reserved route %#v accepted", route)
			}
		}
	}
	if _, err := ValidateManifest(manifest(function("query", map[string]any{"method": "GET", "path": "health"}))); err == nil {
		t.Fatal("route on non-httpAction accepted")
	}
	nullRoute := function("query", nil)
	nullRoute["route"] = nil
	if _, err := ValidateManifest(manifest(nullRoute)); err == nil {
		t.Fatal("explicit null route accepted")
	}
}

func TestUploadEnvelopeBytesArithmetic(t *testing.T) {
	if got := UploadEnvelopeBytes(MaxDeploymentUploadBytes); got != MaxUploadEnvelopeBytes {
		t.Fatalf("absolute envelope=%d want %d", got, MaxUploadEnvelopeBytes)
	}
	if got := UploadEnvelopeBytes(0); got != maxManifestEnvelopeBytes {
		t.Fatalf("zero envelope=%d want %d", got, maxManifestEnvelopeBytes)
	}
	if got := UploadEnvelopeBytes(1 << 20); got >= MaxUploadEnvelopeBytes {
		t.Fatalf("tight envelope=%d", got)
	}
	if got := UploadEnvelopeBytes(3); got != 4+maxManifestEnvelopeBytes {
		t.Fatalf("three-byte envelope=%d", got)
	}
	if got := UploadEnvelopeBytes(4); got != 8+maxManifestEnvelopeBytes {
		t.Fatalf("four-byte envelope=%d", got)
	}
	if UploadEnvelopeBytes(MaxDeploymentUploadBytes+3) <= MaxUploadEnvelopeBytes {
		t.Fatal("max+3 should exceed global envelope")
	}
}

func TestCronJobManifestContract(t *testing.T) {
	functions := []any{
		map[string]any{
			"name": "cleanup", "type": "mutation", "visibility": "internal",
			"modulePath": "pbvex/tasks.ts", "exportName": "cleanup",
			"args":    map[string]any{"type": "object", "shape": map[string]any{"scope": map[string]any{"type": "string"}}},
			"returns": map[string]any{"type": "null"},
		},
		map[string]any{
			"name": "read", "type": "query", "visibility": "internal",
			"modulePath": "pbvex/tasks.ts", "exportName": "read",
			"args":    map[string]any{"type": "object", "shape": map[string]any{}},
			"returns": map[string]any{"type": "null"},
		},
	}
	manifest := func(cronJobs []any) map[string]any {
		return map[string]any{
			"protocolVersion": "v1", "deploymentId": "cron_contract",
			"functions": functions, "cronJobs": cronJobs,
		}
	}
	valid, err := ValidateManifest(manifest([]any{map[string]any{
		"name": "hourly-cleanup", "schedule": "@hourly", "functionName": "cleanup",
		"args": map[string]any{"scope": "expired"},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(valid.CronJobs) != 1 || valid.CronJobs[0].Schedule != "@hourly" {
		t.Fatalf("cron jobs = %#v", valid.CronJobs)
	}

	invalid := [][]any{
		{map[string]any{"name": "bad", "schedule": "@reboot", "functionName": "cleanup", "args": map[string]any{"scope": "expired"}}},
		{map[string]any{"name": "bad", "schedule": "@hourly", "functionName": "read", "args": map[string]any{}}},
		{map[string]any{"name": "bad", "schedule": "@hourly", "functionName": "cleanup", "args": map[string]any{"scope": float64(1)}}},
		{
			map[string]any{"name": "z-last", "schedule": "@daily", "functionName": "cleanup", "args": map[string]any{"scope": "z"}},
			map[string]any{"name": "a-first", "schedule": "@hourly", "functionName": "cleanup", "args": map[string]any{"scope": "a"}},
		},
	}
	for _, jobs := range invalid {
		if _, err := ValidateManifest(manifest(jobs)); err == nil {
			t.Fatalf("invalid cron jobs accepted: %#v", jobs)
		}
	}
}
