# Local policy protocol v1 (experimental)

PBVex exposes a provider-neutral Go package at
`github.com/nathabonfim59/pbvex/backend/hosting`. A self-hoster can implement
the service without a cloud account or private SDK. This is an experimental
foundation, **not a complete hosted isolation or billing boundary**; see the
implementation matrix below.

## Bootstrap and trust

`--hostingEnabled` / `PBVEX_HOSTING_ENABLED` explicitly enables integration.
`--hostingSocket` / `PBVEX_HOSTING_SOCKET` specifies an absolute Unix socket path
(maximum 100 bytes). `--hostingTimeout` / `PBVEX_HOSTING_TIMEOUT` defaults to 2s,
with a 30s maximum. Invalid booleans, durations and enabled configuration fail
startup. Zero Go timeout selects the default. Disabled integration does not
connect or install restrictions. Environment contains bootstrap, never dynamic
allowance balances. `hosting.Config.MaxInFlight` defaults to 32 (maximum 1024).

The service caches dynamic capability status; the client checks each protected
operation, without a local allow cache. Enabled startup requires a successful
handshake, including for administrative CLI commands. No provider means no
enabled startup. Existing process health/static routes do not consult policy.

Use one service endpoint per tenant, mapped by the host from trusted deployment
identity. There is intentionally no caller-supplied tenant ID. Put the socket in
a host-controlled private directory, restrict ownership/mode, and mount only
the intended tenant endpoint. The narrow endpoint must not expose orchestration
commands or its management credentials. Socket access alone does not make
arbitrary tenant JS trustworthy: it could forge protocol messages if allowed
to open sockets. Host scripting is therefore disabled in this foundation.

## Transport and compatibility

HTTP/1.1 POST with `Content-Type: application/json` over a persistent Unix domain
socket. No TCP fallback, redirects, proxy, compression or custom framing. Standard
HTTP content length/chunked framing applies. Requests and responses each have a
16,384-byte limit. Unknown JSON fields and trailing values are rejected. Servers
must reject malformed types, invalid states and conflicting identity reuse.
No arbitrary extension bags are provided. All routes start `/v1/`.

Each call has a total configured deadline including socket connect, header and
body read. Caller cancellation may shorten it. Client capacity is bounded and
saturation immediately returns `ErrBusy`; no waiting queue or telemetry goroutine
is created. Standard `http.Transport` reuses connections and reconnects on later
calls. Failed calls are not automatically retried by this package. Callers may
retry admissions/events using **identical IDs and content**, within their own
bounded retry policy. A new application execution needs new invocation and
admission IDs. No exactly-once delivery claim is made. The client exposes one
narrow transport seam, `Client.Call(ctx, path, in, out)`, so a companion
protocol package (the storage byte quota client) can reuse the same socket,
connection pool and in-flight budget for its own `/v1/` endpoints; the path is
restricted to relative token-segment endpoints and everything else is rejected
before dialing.

`POST /v1/hello` with `{}` returns:

```json
{"version":"1","implementation":"pbvex-reference-memory","capabilities":["function.execute"]}
```

The client supports exactly version `1` and rejects another version. Unknown
version/path returns HTTP 426 in the reference service. There is no silent
downgrade. A successful handshake establishes compatibility, not an entitlement
lease. Providers should retain old endpoints during upgrades until old clients
are retired. Protocol versioning is independent of PBVex deployment manifests.

## Checks, admission and settlement

`POST /v1/check`: `{"capability":"settings.storage.write"}`.
Response: `{"allowed":false,"code":"denied","policyVersion":"p1"}`.
Unknown capabilities deny. Codes are bounded machine identifiers, never detailed
errors. Recommended denial codes: `denied`, `quota_exhausted`, `suspended`.
HTTP 200 can represent denial; check `allowed`. `Client.Require` converts denial
to `*DeniedError`; `Check` and `Admit` return the typed decision for inspection.
**Neither a successful transport nor a reservation is an actual execution.**

`POST /v1/admit`:

```json
{
  "requestId":"admission-123",
  "capability":"function.execute",
  "operation":{
    "id":"invocation-123","rootId":"invocation-123","sessionId":"process-123",
    "kind":"query","origin":"public","deploymentId":"deployment-123",
    "functionName":"messages/list"
  }
}
```

The same decision shape returns a nonempty `reservationId` when allowed.
The service must atomically reserve capacity before acknowledging an allow,
and remember both allowed and denied decisions keyed by request ID. Same ID,
same body returns the original decision even after policy changes. Conflicting
reuse returns 409. One session/invocation cannot reserve again under a different
request ID. Denial **must not enter user code**. Capability checking alone cannot
implement hard quotas. A production provider must durably reserve outside the
tenant database before acknowledging.

`Operation` fields: required `id`, `rootId`, `sessionId`, `kind`, `origin`;
optional `parentId`, `deploymentId`, `functionName`, `namespace`. All identifiers
are 1–128 ASCII letters/digits or `_ - . : /`; optional empty fields are allowed.
Generate process-session identity once per process and invocation IDs per actual
attempt. `NewID()` provides 128 random bits encoded as hex. Root/parent IDs
correlate nested work. Kinds may include `query`, `mutation`, `action`, `http`,
`hook.load`, `hook.callback`, `migration.host`, `migration.application`;
origins include `public`, `nested`, `scheduler`, `realtime`, `admin`. These are
extensible labels, not permission grants. Providers choose accounting treatment.

## Events and privacy

`POST /v1/events`:

```json
{
  "eventId":"event-123","sequence":1,
  "operation":{"id":"invocation-123","rootId":"invocation-123","sessionId":"process-123","kind":"query","origin":"public","deploymentId":"deployment-123","functionName":"messages/list"},
  "reservationId":"reservation-123","policyVersion":"p1",
  "phase":"started","at":"2026-09-06T12:00:00Z","durationMicros":0
}
```

Response `{"eventId":"event-123"}` acknowledges that event only. Repeated identical
events return the same acknowledgement; conflicting event IDs or session/sequence
reuse return 409. Sequence is a positive process-session-wide counter, unique but
not necessarily arriving in order across independent operations. Event operation
and policy version must match the reservation. Lifecycle:

1. Admission creates `admitted`, not executed.
2. `started` (no outcome, zero duration) transitions admitted to started.
3. `completed` transitions started to terminal, with outcome `success`, `error`,
   `timeout`, or `canceled`, and nonnegative integer wall duration in microseconds.
4. Alternatively `released` transitions admitted directly to terminal, outcome
   `canceled`, zero duration, only when execution is known not to have started.

Completion settles the reservation idempotently. Release is explicit settlement
of unused capacity. Wall duration is not CPU time. Timestamps use RFC3339 JSON
time strings; the provider's clock owns billing periods. Concurrent admission
and events must be synchronized by the service. There is no event for execution
denial in v1: the stored denied admission is the non-executed record.

Report started immediately at the execution boundary, and completion only after
an actual start. A provider-acknowledged start can still precede an engine crash;
it records entering the boundary, not proof that the first user instruction ran.
Crash between admission/start/completion leaves unresolved state; do not invent
completion or automatically refund unknown work. Provider recovery/reconciliation
and conservative charging policy must be documented separately.

There are deliberately no arguments, results, function error strings, HTTP
bodies, headers, credentials, logs or arbitrary metadata in the event type.
Use only validated code identifiers in names: never derive names from payloads
or errors. Failed delivery must not log an entire execution/error payload.

Delivery is synchronous and acknowledged, with no client spool. Caller must
handle report failure explicitly; loss on process crash is possible. A production
runtime adapter needs a bounded durable outbox or a documented reporting-failure
halt/reconciliation strategy. Reference state is bounded and never evicted:
when full, new requests get 503, while identical retries still work. Production
dedup retention must exceed all retry/reconciliation windows; never silently
expire a live reservation. HTTP 400/409/413/415/426 indicate invalid requests,
conflicts, size, media-type and version respectively; 503 is capacity/unavailable.
The current client maps non-200 HTTP responses to `ErrUnavailable`, malformed
success responses to `ErrProtocol`, and never interprets either as permission.

## Public Go API and reference service

Use `NewClient(Config)`, `Handshake(ctx)`, `Check(ctx, capability)`,
`Require(ctx, capability)`, `Admit(ctx, AdmissionRequest)`, `Report(ctx, Event)`,
and `Close()`. `Config.Validate()` validates bootstrap independently.
Callers must check both errors and admission `Allowed` before executing.
Keep the same `Client` for the process lifetime.

From `backend/`, run in a private directory owned by the service user:

```sh
go run ./examples/policy-service --socket /absolute/private/policy.sock
go run ./cmd/pbvex serve --hostingEnabled --hostingSocket /absolute/private/policy.sock --admin-ui
```

The reference service allows `function.execute` and denies all other capabilities.
`NewReferenceService(limit)` and `SetPolicy(version, map[string]bool)` are public
for local compatibility tests; policy replacement takes effect on the next check.
**It is not a production durable ledger or quota implementation.** It keeps at
most `limit` admission records and `3*limit` events in memory and loses everything
on restart. It intentionally does not expire/reconcile reservations or bill units.
The example refuses an existing socket path and sets socket mode 0600; a private
parent directory must prevent access during the bind/chmod interval.

## Runtime integration (observer adapter and environment gating)

When hosting is enabled, PBVex builds exactly one policy client per
application and shares it between the administrative gates and a
`runtime.ExecutionObserver` adapter installed before the runtime manager is
created. Every boundary the runtime observes is metered: function calls with
origin `call`, `http_action`, `realtime` or `scheduler` — nested calls
inherit the entry origin and correlate through root/parent identifiers — plus
bundle loads with origin `bundle_load` and application migrations with origin
`migration`. A denied Begin prevents user code from running; the runtime
reports such attempts as admission errors (mapped to HTTP 503, scheduler
defers unstarted work) and never automatically retries user functions.

Begin performs admission and the `started` report synchronously before
entering user code, each with bounded retries (two attempts, 50ms backoff) on
identical identifiers and content. The reservation travels to End through the
context returned by Begin, with a bounded fallback table (1024 entries,
fail-closed) keyed by the runtime execution ID. End settles exactly one
`completed` event per successful Begin using a cancellation-independent
context with a 10s budget, so a caller timeout still emits. Outcomes are
sanitized to `success`, `error`, `timeout` or `canceled` by error identity;
error strings, arguments and results are never transmitted or logged — logs
carry protocol identifiers only. Duration is wall time measured by the
runtime from the admission boundary, including admission latency; it is
reported before the surrounding mutation transaction commits, so `success`
means the handler returned, not that writes committed, and it is never CPU
time.

Failure handling is explicit and deliberately simple. Exhausted retries (two
admission attempts, two `started` attempts, three completion attempts) latch
the adapter unhealthy, rejecting new execution starts until process restart,
while in-flight completions still attempt settlement. A clean local capacity
deny (client `ErrBusy` on the first attempt) and a clean provider denial do
not latch; saturation following an already-uncertain attempt latches like any
other unresolved exchange. An
uncertain `started` acknowledgement denies the execution but never releases
the reservation, because the service may already have recorded the start; the
`released` phase is therefore never emitted by this adapter. Latching on an
unknown reservation at End, and process crashes between Begin and End, leave
provider reservations for provider reconciliation. This adapter is a
synchronous acknowledgement foundation, not a durable outbox, and claims no
crash recovery.

An externally supplied observer is composed, never silently overwritten:
external Begin runs first (its denial prevents reserving capacity), the
hosting adapter runs second, and the external observer receives its End
exactly once whenever its Begin succeeded — including when admission then
denies the execution. A nil external context is rejected safely
with that same End pairing. Admission always runs under the caller's
cancellation and deadline, which an external observer cannot erase. The
synchronous foundation trades latency for
simplicity: an execution start normally costs two socket round-trips
(admission plus started, up to four attempts total with retries, each bounded
by the configured client deadline and caller context),
completion adds a bounded best-effort report, and no queues, spools or
telemetry goroutines exist. Production metering needs a bounded durable
outbox or a documented reconciliation strategy instead of this latch.

Identity projection: the process generates one session identifier and a
process-wide atomic event sequence starting at 1; runtime attempt identifiers
(128-bit random hex) pass through when token-safe and are otherwise projected
onto deterministic SHA-256 hexadecimal identifiers. Required labels that
cannot be represented deny the execution — no invented labels, no unmetered
path — while optional labels (`deploymentId`, `functionName`, `namespace`)
are omitted when unsafe. Empty function types are not functions: `bundle_load`
maps to the `bundle.load` kind and `migration` to `migration.application`.

Coverage is limited to what the runtime observes. Native PocketBase
operations are not admitted or reported as protocol events. Changed storage,
backup and SMTP settings categories and backup downloads are
capability-checked; backup creation, direct SQL, collection import and backup upload are
denied outright; superuser authentication and native record/file endpoints do
not receive those new checks. Native record-file byte accounting is a
separate storage-layer concern covered by
[storage byte quotas](./hosting-storage-quotas.md), not by policy events.
The PocketBase JS plugin stays disabled
(temporary limitation below), so no `hook.load` or `hook.callback` events
occur yet. `runtime.Config.MaxConcurrentExecutions` remains independently
owned by the runtime; hosting policy cannot raise it.

Host environment reads are permission-gated per name: every hosted component
environment variable binding resolved from the host environment requires an
explicit `environment.read/<name>` capability grant before lookup, while
literal manifest bindings never consult policy. A configured custom
environment resolver is composed consciously — permission check first, then
the custom resolver; without one, the standard `os.LookupEnv` default
applies. There is no unrestricted fallback: denial or unavailability fails
the binding. Composed capability names are validated locally against the
token rules before any socket traffic, and the reference service denies these
capabilities unless the exact name is granted. This narrows, but does not
close, the secret-isolation blocker below: what providers may safely grant
and how managed credentials are injected remain runtime and platform
ownership.

## Implementation matrix and remaining gaps

| Area | Implemented foundation | Remaining limitations |
| --- | --- | --- |
| Public protocol | Public v1 wire contract, typed client, bounded reference service, lifecycle/conflict tests | Production ledger, independent third-party conformance, crash recovery, latency benchmark |
| Runtime integration | Explicit enablement, validated bootstrap, persistent socket, startup handshake, dynamic fail-closed admin checks, central runtime observer adapter with fail-closed reporting latch and per-name environment gating; integrated backend tests pass | Provider reconciliation of unknown reservations, latency benchmark |
| Administrative gates | Settings request compares old/new categories with host-managed locks and persisted-baseline neutralization; backup download gate; backup creation denied outright while storage quotas are enforced; restore, direct SQL, collection import and backup upload denied before side effects; managed storage/SMTP injection | Full collection-schema audit, provider-allowed restore, dynamic managed-secret rotation |
| Host scripting | Enabled integration skips the entire PocketBase JS plugin registration, preventing hook-file and custom JS migration loading | Dynamic optional hooks and per-callback telemetry require PocketBase execution-boundary changes; host JS remains disabled even if `host.scripts` allows |

Settings capabilities are `settings.storage.write`, `settings.backups.write`,
`settings.smtp.write`; these gate the entire changed category, not individual
fields. `environment.read/<name>` grants per-name host environment reads to
hosted component bindings; the reference service denies them unless each
exact name is granted. `backup.download` is dynamic. `backup.create`
remains part of the wire policy surface, but while storage byte quotas are
enforced the platform denies backup creation outright and does not consult
the provider: an allowed backup writes archive bytes with no pre-write
size bound, so a grant cannot authorize it (see
[storage byte quotas](./hosting-storage-quotas.md)).
`backup.restore` is reserved: restores are unconditionally rejected before
PocketBase's restore handler while enabled, because archives may replace
settings/files. Standard PocketBase admin UI is retained. These checks are
backend request/lifecycle hooks, not UI restrictions.
Built-in Go migrations and ordinary PBVex application deployment remain separate
from the disabled PocketBase JS plugin. Existing PBVex system-record protection
remains in force; these additions do not claim a complete collection-schema audit.

Pinned PocketBase v0.40.1 has `SettingsUpdateRequestEvent.OldSettings/NewSettings`
and `OnBackupCreate/OnBackupRestore`, inspected before implementing these gates.
Its `plugins/jsvm` owns `RunScript` loading and callback runtime pools; registration
is not a dynamic callback gate. Do not re-enable that plugin using a startup-only
allow check. Native collection upload quota enforcement cannot be solved by
request middleware either; the core-hook-based coverage that does exist is
documented in [storage byte quotas](./hosting-storage-quotas.md).

**Managed-secret isolation:** `backend/internal/runtime/component.go` resolves
component env bindings with `os.LookupEnv(binding.Name)` without a host-secret
allowlist on its own; when hosting is enabled, the resolver composed in the
runtime integration section additionally requires an explicit
`environment.read/<name>` grant before any host lookup, so ungranted names —
including managed credential names — fail closed. Host-owned storage and SMTP
configuration can additionally be injected into the running process without
being persisted in the tenant database, with the corresponding settings
categories locked and the hook-less native routes (SQL, collection import,
backup download/upload) restricted. That mechanism, its environment
variables, and its remaining boundaries are documented in
[hosted secret and configuration isolation](./hosting-secret-isolation.md).
Provider secrets should stay out of this process and its data; dynamic
secret rotation and provider-allowed restore remain unimplemented.

Focused validation lives in `backend/hosting/client_test.go` (persistent
connections, dynamic changes, idempotency across policy changes, conflicts,
ordering/release, capacity, local saturation without a queue, malformed
outcomes, version/oversize/timeout/disconnect failure, and environment
capability namespaces), `backend/internal/pbvex/hosting_test.go` (settings
effective changes and restore-before-side-effect denial),
`backend/internal/pbvex/hosting_managed_test.go` and
`backend/internal/pbvex/hosting_native_paths_test.go` (managed shadow and
persisted-baseline invariants, unrelated-edit preservation, archive secret
scanning, download gating, and native path denials; see
[hosted secret and configuration isolation](./hosting-secret-isolation.md)),
and
`backend/internal/pbvex/hosting_observer_test.go` (observer lifecycle:
admission/started/completion, denial before user code, uncertain-start latch
without release, completion after caller cancellation, outcome sanitization,
nested correlation and sequences, external observer composition, identity
projection, tracking bounds, concurrent sequences, busy admission, and
environment gating). The runtime branch carries its own observer seam tests;
the integrated full suite and race check are owned by the downstream
integration run, not by this protocol package.
