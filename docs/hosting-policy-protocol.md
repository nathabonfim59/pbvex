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
admission IDs. No exactly-once delivery claim is made.

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

## Implementation matrix and remaining gaps

| Downstream task | Implemented foundation | Remaining acceptance gaps |
| --- | --- | --- |
| DS-01 | Public v1 wire contract, typed client, bounded reference service, lifecycle/conflict tests | Production ledger, independent third-party conformance, crash recovery, latency benchmark |
| DS-02 | Explicit enablement, validated bootstrap, persistent socket, startup handshake, dynamic fail-closed admin checks | Runtime observer adapter must wire admission/reporting centrally; hosted execution enforcement is not complete in this commit |
| DS-03 | Settings request compares old/new S3, backups and SMTP categories; unchanged protected values permit unrelated saves; backup-create gate; unconditional restore denial | Managed secret injection/redaction, settings export/backup confidentiality, collection import and full native-path audit |
| DS-04 | Enabled integration skips the entire PocketBase JS plugin registration, preventing hook-file and custom JS migration loading | Dynamic optional hooks and per-callback telemetry require PocketBase execution-boundary changes; host JS remains disabled even if `host.scripts` allows |

Settings capabilities are `settings.storage.write`, `settings.backups.write`,
`settings.smtp.write`; these gate the entire changed category, not individual
fields. `backup.create` is dynamic. `backup.restore` is reserved: restores are
unconditionally rejected before PocketBase's restore handler while enabled,
because archives may replace settings/files. Standard PocketBase admin UI is
retained. These checks are backend request/lifecycle hooks, not UI restrictions.
Built-in Go migrations and ordinary PBVex application deployment remain separate
from the disabled PocketBase JS plugin. Existing PBVex system-record protection
remains in force; these additions do not claim a complete collection-schema audit.

Pinned PocketBase v0.40.1 has `SettingsUpdateRequestEvent.OldSettings/NewSettings`
and `OnBackupCreate/OnBackupRestore`, inspected before implementing these gates.
Its `plugins/jsvm` owns `RunScript` loading and callback runtime pools; registration
is not a dynamic callback gate. Do not re-enable that plugin using a startup-only
allow check. Native collection upload quota enforcement likewise cannot be solved
by request middleware and remains outside this foundation.

**Secret isolation blocker:** `backend/internal/runtime/component.go` resolves
component env bindings with `os.LookupEnv(binding.Name)` without a host-secret
allowlist. Do not inject provider/managed storage credentials into the tenant
process environment until that boundary is constrained. SMTP environment overrides
also persist in PocketBase settings; the new category write gate does not redact
them from settings export or backups. Provider secrets should stay out of this
process and its data. Full DS-03/DS-08 acceptance is blocked on these changes.

Focused validation lives in `backend/hosting/client_test.go` and
`backend/internal/pbvex/hosting_test.go`: persistent connections, dynamic changes,
idempotency/conflicts, ordering/release, capacity, version/malformed/oversize/
timeout/disconnect failure, settings effective changes, and restore-before-side-
effect denial. Parent integration must test every execution origin and reporting
failure; the protocol package alone does not instrument executions.
