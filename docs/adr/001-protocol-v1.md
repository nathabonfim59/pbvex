# ADR 001: PBVex Deployment Protocol v1

## Status
Accepted - protocol v1 compatibility contract.

## Context
PBVex exposes a Convex-shaped TypeScript developer experience on a generic PocketBase-based Go binary. The backend, CLI, and client SDKs share this versioned contract: JSON types, validator descriptors, wire values, canonical hashing, and HTTP/realtime endpoints.

## Decisions

### 1. Protocol version
- The deployment protocol is fixed at `protocolVersion: "v1"` for this phase.
- All agents must reject manifests with a `protocolVersion` other than `v1`.

### 2. Deployment manifest
A valid v1 manifest is a JSON object:

```json
{
  "protocolVersion": "v1",
  "deploymentId": "<identifier>",
  "functions": [
    {
      "name": "<identifier>",
      "type": "query" | "mutation" | "action" | "httpAction",
      "visibility": "public" | "internal",
      "modulePath": "<bundle-path>",
      "exportName": "default" | "<identifier>",
      "args": <optional-validator-descriptor>,
      "returns": <optional-validator-descriptor>,
      "route": <optional-http-action-route>
    }
  ],
  "schema": { "tables": [] },
  "components": { "definitions": [], "mounts": [] },
  "emailTemplates": { "sha256": "<lowercase-sha256>", "entries": [] },
  "cronJobs": [],
  "migrations": [],
  "config": {
    "httpPathPrefix": "/api/pbvex",
    "realtimePath": "/api/pbvex/realtime",
    "maxUploadBytes": 67108864,
    "maxFunctionArgsBytes": 1048576,
    "maxReturnValueBytes": 1048576,
    "defaultRequestTimeoutMs": 30000
  }
}
```

- `deploymentId` must be a valid identifier.
- `functions` is optional and may be empty to allow schema-only deployments.
- `schema`, `components`, `emailTemplates`, `cronJobs`, and `migrations` are optional first-class deployment definitions with strict bounded validators.
- `functions` entries must be valid `FunctionDescriptor` objects.
- `modulePath` is a relative POSIX-like bundle path (no leading `/`, no `..`, `[a-zA-Z0-9._/\-]+`).
- `exportName` is `"default"` or a valid identifier.
- `args` and `returns` are optional validator descriptors; when present they must satisfy the bounded descriptor grammar implemented by both TypeScript and Go.
- `route` is only valid for `httpAction` functions and declares an HTTP method with either an exact path or a path prefix.

### 3. Naming grammar
- **Identifiers**: `^[a-zA-Z][a-zA-Z0-9_]*$`, max length 1024.
- **Module paths**: relative POSIX-like paths, no leading `/`, no `..`, only `[a-zA-Z0-9._/\-]+`, max length 4096.
- **Object field names**: non-empty, max 1024, ASCII printable (`0x20-0x7E`), cannot start with `$`, cannot be `__proto__`, `constructor`, or `prototype`.

### 4. Canonical JSON and content hashing
- Canonical JSON is a deterministic string with:
  - No whitespace.
  - Object keys sorted lexicographically.
  - Numbers as finite JSON numbers (`-0` normalizes to `0` by `JSON.stringify`).
  - Strings escaped with `JSON.stringify()` behavior.
- Canonical JSON rejects non-finite numbers, `undefined` (including object properties), cyclic references, `bigint`, `symbol`, `function`, and objects with unsafe prototypes.
- Content hash is `SHA-256(UTF-8 bytes of canonical JSON)` returned as lowercase hex.
- Bundle hash is `SHA-256(raw bundle bytes)` of the deterministic executable JS bundle.

### 5. Wire value codec
The codec supports the following values:
- `null`
- booleans
- finite `number` (NaN/Infinity are rejected on both encode and decode)
- `string`
- 64-bit signed `bigint` (int64), encoded as `{ "$integer": "<base64-little-endian-8-bytes>" }`
- `ArrayBuffer` bytes, encoded as `{ "$bytes": "<base64>" }`
- arrays
- plain objects (no unsafe prototypes, no cyclic data, no `$`-prefixed or reserved keys; properties whose values are `undefined` are omitted by the wire encoder)
- `Id` (a branded string; encoded as a plain string for Convex wire compatibility)

New document IDs use a canonical authenticated `pbv2` envelope containing the
key version, exact namespace (`root` or a deterministic component namespace),
logical table, and raw record ID. Clients may validate the structural envelope
but cannot mint IDs. The backend authenticates every `v.id` at database,
function, component-mount, and schema-migration boundaries. Authenticated
legacy `pbv1` IDs are accepted only for the root namespace migration path.

Top-level and array-element `undefined` values are not valid in the codec. Plain-object properties whose values are `undefined` are omitted before encoding, and a function's top-level `undefined` return is normalized to `null`.

### 6. Deployment endpoints
- `POST /api/pbvex/deployments` — upload a new deployment.
  - Request body: `DeploymentUploadRequest` JSON envelope:
    ```json
    {
      "manifest": <DeploymentManifest>,
      "bundle": "<base64-encoded deterministic executable JS bundle>",
      "sha256": "<lowercase-hex SHA-256 of decoded bundle bytes>",
      "size": <non-negative-integer>,
      "modules": <optional-array-of-authenticated-module-sources>
    }
    ```
  - The backend decodes the base64 bundle, verifies byte length and SHA-256, and stores the manifest and bundle as a single deployment. When the manifest declares components, `modules` is required and contains bounded `{ path, bytes }` entries whose base64 source bytes authenticate every declared component module hash.
  - Response: `{ deploymentId, bundleHash, acceptedAt }`.
- `GET /api/pbvex/deployments` — list stored deployments.
  - Response: `DeploymentListResponse`.
- `POST /api/pbvex/deployments/:id/activate` — activate a previously uploaded deployment.
  - Body: `{ atomic: true }`.
  - The backend verifies and compiles the candidate runtime before opening the database transaction. It then applies migrations and schema/component changes and swaps the active deployment in one transaction. Any failure leaves the previous deployment active.
  - Response: `DeploymentActivateResponse` with `deploymentId`, `activatedAt`, optional `previousDeploymentId`, and optional migration-utilization `warnings`.
- `POST /api/pbvex/deployments/:id/rollback` — rollback to the previous active deployment.
  - Response: `DeploymentRollbackResponse` with `deploymentId`, `rolledBackAt`, and optional `restoredDeploymentId`.
- `POST /api/pbvex/call` - HTTP envelope for public query/mutation/action calls.
- `POST /api/pbvex/realtime` - primary SSE transport for one public query subscription per request.
  - Request JSON is `{ id, path, args }`, where `id` is derived from the protocol version, function path, and canonical encoded arguments.
  - `GET /api/pbvex/realtime?id=...&path=...&args=...` is a strictly bounded compatibility fallback.
  - Clients must request `Accept: text/event-stream`; POST also requires `Content-Type: application/json`.
  - Events are SSE `data: <RealtimeEnvelope>` records with `subscribe`, `message`, and `ping` operations.
  - Record mutations invalidate active subscriptions. Deployment activation closes connections so clients reconnect against the new immutable snapshot and limits.

### 7. Error envelope
All structured errors are JSON objects:

```json
{
  "error": true,
  "code": "<error-code>",
  "message": "<string>",
  "details": [],
  "requestId": "<optional>"
}
```

Core error codes are `bad_request`, `invalid_manifest`, `invalid_function`, `bundle_not_found`, `bundle_hash_mismatch`, `activation_failed`, `not_found`, `unauthorized`, `forbidden`, and `internal`. Service-specific protocol errors include storage admission states such as `upload_expired`, `upload_consumed`, `upload_pending`, and `storage_full`. Clients must preserve unknown string codes for forward compatibility.

### 8. Atomic activation/rollback semantics
- Activation is all-or-nothing for a single deployment from the caller's perspective.
- The backend validates descriptors and verifies/compiles the candidate bundle before the database transaction. The transaction applies migrations and schema/component materialization, records history, and swaps the active deployment pointer atomically.
- A failed activation does not affect the currently active deployment.
- Rollback restores the previous active deployment atomically.

### 9. Components

The optional `components` manifest field contains content-addressed component
definitions and a bounded mount graph. Definitions bind their module hashes,
schema, argument validator, environment declarations, dependencies, and bundle
hash into the component identity. Each mount path derives a stable namespace;
mounting the same definition twice isolates its data, while upgrading a
definition at the same path preserves that namespace.

Component tables are materialized as deterministic PocketBase collections and
recorded in the component ownership catalog. Activation authenticates mount
arguments and stored/defaulted IDs against the exact namespace, migrates schema
and indexes transactionally, and never silently adopts an unowned physical
collection. Removed mounts and tables remain dormant for rollback or a later
remount at the same canonical path.

### 10. Optional schema descriptor
A valid v1 manifest may optionally include a `schema` field describing the shape and indexes of data tables:

```json
{
  "schema": {
    "tables": [
      {
        "tableName": "messages",
        "fields": {
          "body": { "type": "string" },
          "author": { "type": "id", "tableName": "users" }
        },
        "indexes": [
          { "name": "by_author", "fields": ["author"] }
        ]
      }
    ]
  }
}
```

- `schema` is optional. When present, `schema.tables` is an array of `TableDescriptor` objects.
- Each table descriptor has an `tableName` (identifier), `fields` (a JSON object mapping field names to validator JSON values), and an optional `indexes` array.
- Each index descriptor has a `name` (identifier) and `fields` (array of strings).

### 11. Runtime host bridge
The CLI emits a single deterministic executable JS bundle as an immediately-invoked function expression (IIFE). The bundle has no Node globals, `require`, or `import` statements at runtime. It may be evaluated by the Go runtime (e.g., Goja) with a minimal host bridge exposed on `globalThis.__pbvex`:

```js
globalThis.__pbvex.registerFunction(descriptor, handler);
globalThis.__pbvex.registerMigration(descriptor, up, down);
```

- `descriptor` is a valid `FunctionDescriptor` (see Decision 2).
- `handler` is the function body that the runtime will invoke for calls matching `descriptor`.
- The IIFE is responsible for enumerating its exported function objects and registering each one with the host bridge.
- Bundled migration modules register their descriptor and pure `up`/`down` handlers through `registerMigration`.
- The host bridge is the only runtime API available to the bundle; all approved globals (e.g., `console`, `Object`, `Array`) are explicitly provided by the host.

### 12. Compatibility boundaries
- Single-node strong consistency: this protocol assumes a single PocketBase process and does not define multi-node consensus.
- No vector search in v1.
- No arbitrary npm/Node runtime: functions are bundled ahead of time by the CLI into a single deterministic executable JS bundle and the runtime executes the bundle within the Go process.
- HTTP/realtime envelope payloads are opaque to the transport; the wire value codec applies to `body`, `args`, and `payload`.
- The deploy endpoint stores the manifest and bundle as a single deployment; bundles are not tar/zip archives.
- Scheduler jobs pin the immutable deployment snapshot they target. Realtime subscriptions also invoke the snapshot captured at admission.
- Reserved PBVex routes (deployment, call, realtime, jobs, and storage) take precedence over deployed HTTP action catch-all routes.
