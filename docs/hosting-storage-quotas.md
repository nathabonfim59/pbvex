# Storage byte quotas (experimental)

PBVex exposes a provider-neutral Go package at
`github.com/nathabonfim59/pbvex/backend/hosting/storagequota`. It reserves
storage bytes at a host-owned service **before any bytes are written**, so a
hard quota cannot be raced by concurrent uploads, and usage never lives in the
tenant database, so a tenant restore cannot reset consumed quota. This is an
experimental foundation, **not a complete hosted billing boundary**; see the
coverage matrix and the documented gaps below.

The storage quota protocol rides on the same transport, trust model and
configuration as the local policy protocol in
[hosting-policy-protocol](./hosting-policy-protocol.md): HTTP/1.1 POST with
JSON over a persistent Unix socket, `--hostingEnabled`/`--hostingSocket`
bootstrap, one service endpoint per tenant, 16,384-byte payloads, bounded
client capacity (`ErrBusy`, no waiting queue), and **no silent downgrade**.
A single provider socket serves both the `/v1` policy routes and the
`/v1/storage/` routes below through one shared client, connection pool and
in-flight budget. Hosting enabled enforces storage quotas: the provider
must serve the storage routes and list the `storage.reserve` capability in
the shared handshake, or startup fails (see the wiring section below).

## Guarantees and non-guarantees

- **Atomic host-authoritative reservations.** The provider must durably
  reserve the byte bound before acknowledging an allow, under a lock or
  equivalent, so concurrent reservations cannot exceed the tenant
  allowance. Neither a successful transport nor a reservation is an actual
  write.
- **Exact bound before the persistent write.** PBVex variants are encoded
  into a bounded local temporary filesystem first, so the reservation is
  taken with the exact encoded size before any persistent bytes exist;
  native upload reservations use the exact multipart spool sizes. The
  persisted object can therefore never exceed its reservation.
- **Usage only shrinks on verified absence.** Credits are reported only
  after a deletion is verifiably complete (PBVex prefix deletions and
  synchronous native update removals). Deletion paths whose cleanup cannot
  be verified synchronously — record deletion, uncertain failed writes —
  keep the usage reserved for provider reconciliation instead of guessing.
- **Idempotent retries preserve identity.** Reservations are identified by
  the caller's request ID; settlements and releases by reservation ID;
  deletion credits by event ID. Retries reuse identical IDs and content and
  receive the original acknowledgement. A new attempt needs a new ID. No
  exactly-once delivery claim is made.
- **Fail-closed.** Denials and unavailability prevent the write. A denied
  upload never enters the storage backend, a denied variant is never
  persisted, and native thumbnail requests are denied outright while
  quotas are enforced (see coverage below). Settled usage may only be
  freed by a verified credit or provider reconciliation; the provider
  rejects a settlement above the reserved bound as a protocol conflict
  and keeps the reservation for reconciliation, so a caller cannot raise
  its own charge or silently acknowledge an undercount.
- **Sanitized diagnostics.** Provider, transport and custom observer errors
  are never logged or returned verbatim; only fixed classifications and
  protocol identifiers cross the log stream.
- **Not covered here:** durable ledgers, reconciliation sweeps, billing
  periods, reservation expiry and the production provider itself. The
  reference service below is a bounded in-memory compatibility fixture, not
  a durable ledger. **Backup archives are not covered by reservations**:
  an allowed backup writes archive bytes with no pre-write size bound, so
  while quotas are enforced the platform denies backup creation outright —
  the `backup.create` capability grant is deliberately not consulted
  because it could bypass the byte quota. Restores remain unconditionally
  rejected while hosting is enabled.

## Wire contract (version 1)

Unknown JSON fields and trailing values are rejected; identifiers follow the
protocol token rules (1–128 ASCII letters/digits or `_ - . : /`; object keys
up to 256). HTTP 200 can represent a denial — check `allowed`. Non-200
responses map to `ErrUnavailable`, malformed responses to `ErrProtocol`,
and neither is ever interpreted as permission.

`POST /v1/hello` returns the shared handshake; providers that support
storage quotas list the `storage.reserve` capability. Clients that require
it can treat a missing capability as a denial of every reservation.

`POST /v1/storage/reserve`:

```json
{
  "requestId": "upload-9f1c",
  "purpose": "upload",
  "storageId": "pbv_ab12",
  "key": "storage/pbv_ab12/_stage/ab12/blob",
  "bytes": 1048576
}
```

`purpose` is `upload` or `variant`; `storageId` is the PBVex storage id when
one exists and is omitted on native PocketBase record files; `key` is the
object key or prefix when known; `bytes` is the worst-case bound. The same
decision shape as the policy protocol returns a nonempty `reservationId`
when allowed and a bounded code when denied (`quota_exhausted`,
`denied`, `suspended` are recommended).

`POST /v1/storage/settle` — `{"reservationId":"r1","bytes":4096}` —
transitions the reservation to the actual stored count and acknowledges
`{"reservationId":"r1","chargedBytes":4096}`. A settlement above the
reserved bound is a protocol conflict (HTTP 409): the provider rejects it
and keeps the reservation and its usage reserved for reconciliation
instead of acknowledging an undercount.
`POST /v1/storage/release` — `{"reservationId":"r1"}` — returns a
reservation whose write definitively did not persist. Settling a released
reservation, releasing a settled one, or conflicting bodies under the same
IDs return 409.

`POST /v1/storage/credit`:

```json
{"eventId":"delete-pbv_ab12","storageId":"pbv_ab12","key":"storage/pbv_ab12/","bytes":42000}
```

reports confirmed freed bytes after a deletion; the acknowledgement echoes
the event ID and the provider floors its usage total at zero.

## What is covered

| Write path | Reservation boundary | Settlement |
| --- | --- | --- |
| PBVex single-use upload | Worst-case per-attempt staging cap reserved before the body is staged | Actual size settled after the metadata commit |
| PBVex image variant | EXACT encoded size reserved after generation into a local temp filesystem, before the persistent write | Actual (equals the reservation) settled after the write |
| PBVex deletion | — | Confirmed freed bytes credited (verified prefix deletion) |
| Native record create/update uploads | Exact multipart spool sizes reserved before upstream writes the files | Stored sizes settled after the record commit |
| Native update file replacements | New bytes reserved before the write | New bytes settled; old bytes credited only after verified removal |
| Native record deletion | — | NOT credited: upstream cleanup is asynchronous, so usage stays reserved for reconciliation |
| Native on-demand thumbnails | All native thumbnail requests DENIED while quotas are enforced — cached selectors included, because no reservation is possible and a cached object can disappear before serving | — |

Standalone behavior is unchanged: with no observer attached, no byte
accounting is performed, no native hooks are installed, and thumbnail
generation works as upstream ships it.

### PBVex storage paths

The storage service accepts a quota observer (`Service.SetQuotaObserver`).
Uploads reserve the effective staging cap before the request body is read;
staging failures release the reservation, failures after the backend persist
keep it when the stage object may still exist, and the metadata commit
settles the actual size. Lazy variants are generated through the unchanged
upstream generation path into a private local temporary filesystem (disk
backed, like the existing upload staging); the exact encoded size is then
reserved and only afterwards is the staged object persisted to the storage
backend, so the stored bytes can never exceed the reservation. A denied
variant aborts before any persistent bytes exist and fails that download
closed. Deletions credit the byte total of the removed prefix (original plus
variants) under the stable `delete-<storageId>` identity, reported only when
the prefix deletion completed without error. Reports use a
cancellation-independent context with a small bounded retry budget, so a
caller timeout does not lose a settlement; a lost report is logged with a
sanitized classification for provider reconciliation.

### Native PocketBase record files

The pinned upstream performs record file uploads inside the system file
interceptor at priority 99 on `OnRecordCreateExecute`/`OnRecordUpdateExecute`
and post-commit removals at priority -99 on the after-success/after-error
hooks. The installed hooks use priorities on the safe side of those
boundaries: reservations run before any upload write, settlements and
credits run after the upstream commit or cleanup step, and failure handlers
release a reservation only when every uploaded object is verified absent —
an object that failed cleanup keeps its reservation for reconciliation.
Update-time file replacements are credited only after the removed objects
and their variant prefixes are verifiably absent. Saves without file changes
never consult the quota service. Denials abort the save before any write and
before the record exists.

The reserved upload sizes are trusted because they are measured, not
declared: PB builds multipart uploads through
`filesystem.NewFileFromMultipart`, whose `Size` the Go standard library
computes from the bytes it actually spooled for the part (the part is
terminated by its boundary, so the reader the upload streams later yields
exactly that many bytes).

Record deletion is different upstream: the whole record files prefix is
deleted asynchronously after the delete transaction (`FireAndForget`), so a
synchronous absence check is impossible without racing the cleanup. Under
the hard quota contract no credit is reported on this path — the tenant's
usage stays reserved until the provider reconciles the object store.

### Native on-demand thumbnails

Upstream generates missing record thumbnails inside the files download
route — after routing and authorization but before serving — with no core
hook in between (`OnFileDownloadRequest` fires after generation) and no
size-carrying writer hook, so no exact reservation is possible on this
path. While a quota observer is attached, the installed gate therefore
denies every native files-download request that carries a nonempty thumb
selector with an explicit `403` — cached selectors included. A serve-only
fast path cannot be made safe: upstream falls back to generating whenever
the cached variant is missing at serve time, so a transient storage error
on the cache lookup, or a cache deletion between the gate and the upstream
handler, would produce an unreserved generation. The gate keys on the
matched route pattern, so encoded path characters cannot rename the route
that actually runs. Original downloads without a thumb selector keep their
upstream behavior, and standalone deployments (no observer) generate
exactly as upstream ships. PBVex's own variant path (encode-then-reserve)
is a different route and is not affected.

## Documented gaps

- **Backup archives are not reserved.** An allowed backup creation would
  write a zip archive through the backups filesystem with no pre-write
  size bound, so it can never be covered by this quota layer. The platform
  therefore denies backup creation outright whenever hosting is enabled:
  the API refuses before scheduling and the `OnBackupCreate` hook refuses
  scheduled and programmatic creates, and the `backup.create` capability
  grant is deliberately not consulted because it could bypass the byte
  quota. Restores remain unconditionally rejected while hosting is
  enabled, so a restore cannot write unreserved bytes either.
- **Record deletion keeps usage reserved.** Because upstream cleanup is
  asynchronous, deleted record bytes stay charged until the provider
  reconciles the object store; the quota layer never credits unverified
  deletions.
- **Failed native cleanups keep reservations.** If upstream cannot delete a
  failed upload's objects, the reservation stays unsettled by design; the
  bytes may still exist and providers reconcile.
- **Reconciliation is a provider duty.** Unknown-settled reservations
  (process crash between reserve and settle), lost reports after exhausted
  bounded retries, and retained record-deletion usage all require the
  documented provider reconciliation pass over the actual object store.
  The protocol supports reconciliation; it does not fake a durable host
  service.
- **Native thumbnail requests are denied wholesale.** Because upstream
  lacks a core hook before thumbnail generation (and its writer hook
  carries no byte count), no exact reservation is possible on the files
  download route — and no serve-only fast path is safe either, since
  upstream falls back to generating whenever the cached variant is missing
  at serve time (a transient storage error or a cache deletion between a
  gate and the handler would produce an unreserved generation).
  Enforced-mode deployments therefore refuse every native thumbnail
  request with an explicit `403`, cached variants included, and the
  platform should surface this as an expected restriction. Original
  downloads without a thumb selector are unaffected; upstream changes to
  that route must be mirrored, and the pinned version is documented in the
  implementation. PBVex's own variant path (encode-then-reserve) is not
  affected.

## Public Go API

Providers implement the four routes above (or run the reference fixture)
behind a Unix socket with the same private-directory precautions as the
policy service. The reference service is public for local development and
compatibility tests:

```go
svc := storagequota.NewReferenceQuotaService(1000, 1<<30) // 1000 records, 1 GiB
// serve svc on a private Unix socket with net/http
```

It keeps bounded in-memory records, never evicts live ones (full means 503
while identical retries still work), loses everything on restart, performs
no reconciliation, and rejects above-bound settlements as conflicts.
**It is not a production durable ledger or quota implementation.** A single
socket can serve the policy routes and the storage routes behind one
handler that dispatches `/v1/storage/*` to the quota service and composes
the handshake so the capability list covers both protocols — exactly what
`go run ./examples/policy-service` does with its in-memory demo capacity
(flags `--quotaRecords` and `--quotaBytes`; state is lost on restart).

### Wiring and handshake compatibility

Hosting enabled enforces storage quotas; there is no separate bootstrap
flag and no silent downgrade. At startup the shared `/v1/hello` handshake
must list the `storage.reserve` capability, and startup fails with an
explicit error otherwise, so an enabled deployment can never silently
depend on a provider that cannot account bytes. Providers extend a
policy-only endpoint by serving the `/v1/storage/` routes and adding the
capability to the composed hello; the protocol version string is shared,
so a future incompatible quota protocol would be a new version.

The embedding wiring (the `pbvex` binary does this inside `RegisterCore`;
the parent application owns the configuration surface):

```go
client, hello, _ := newHostingClient(cfg.Hosting) // handshake, fail on error
if !storagequota.SupportsReserve(hello) { /* fail startup */ }
quotaClient, _ := storagequota.NewClientFromRoot(client) // borrowed transport
storageService.SetQuotaObserver(storage.NewHostedQuotaObserver(quotaClient))
storageService.InstallNativeQuotaHooks(app) // native record hooks + thumb gate
```

The observer and the native hooks are installed before the storage service
starts, so no write path can run unaccounted. `NewClientFromRoot` sends
storage requests through `hosting.Client.Call(ctx, path, in, out)` — the
policy client's own bounded transport — so one socket, one persistent
connection pool and one in-flight budget are shared per application; the
borrowed client's `Close` is a no-op and terminate closes the root client
once. `Call` accepts only relative endpoints below `/v1` (protocol-token
segments, no traversal, query, fragment or authority) and rejects anything
else with a protocol error before dialing. Standalone single-protocol
callers (compatibility tests, providers without the policy routes) can use
`storagequota.NewClient(cfg.Hosting)`, which owns its root transport and
closes it.

`NewHostedQuotaObserver` maps denials to `storage.ErrQuotaDenied` and
everything else — transport failures, saturation, malformed responses, any
custom observer error — to `storage.ErrQuotaUnavailable`; raw provider
error text never propagates into errors or logs. Both outcomes fail the
write closed. Focused validation lives in `backend/internal/storage`
(reservation ordering, denial fail-closed, uncertain-write retention,
exact variant reservations, deletion crediting only on verified removal,
record-deletion usage retention, concurrent uploads against a
capacity-limited observer, native record hooks and the thumbnail gate
exercised end to end through the real router), in
`backend/hosting/storagequota` (wire round-trips, idempotent replays,
conflicts, above-bound settlement rejection preserving the reservation,
atomic capacity under concurrency, saturation, malformed responses, and
shared-transport reuse, budget and close semantics), in
`backend/hosting` (endpoint path constraints of `Call`), and in
`backend/internal/pbvex` (startup handshake compatibility against a
policy-only provider, full startup smoke over the example-compatible
composed provider with uploads, variants and native record files through
the real router, fail-closed denial before writes, wholesale native
thumbnail denial including cached selectors with originals still served,
unchanged standalone behavior,
and terminate closing the shared transport).
## Provider initialization and recovery

The following guidance applies to compatible durable providers. It does not
add wire fields or implement durability in the in-memory reference service.

### Establish usage before admitting writes

Registering a tenant or configuring its byte limit is not proof that its storage
is empty. Before the first allowed reservation, establish usage from a complete,
authoritative inventory. A successfully completed empty inventory can establish
zero usage; a failed or partial scan cannot. Preserve both usage and initialization
state across provider restarts. Changing a limit must not reset either state.

### Admission retries need a live reservation

An identical reserve retry can replay an allowed decision while the original
hold is still active and admission is permitted. A settled, released, or
reconciled-away reservation must not return an allow that could authorize another
write without reserved capacity. Retain its identity and reject stale retries
as conflicts. During maintenance, deny admission without discarding existing
holds. Retrying a settlement or release is distinct from authorizing another
write; preserve deterministic acknowledgements for already applied operations.

If a response is lost, the provider may already have committed the operation.
Persist state and idempotency records atomically before acknowledgement. Never
expire uncertain reservations into free capacity based only on elapsed time.

### Credit claims and authoritative reconciliation

The protocol's credit message reports a client's claim of confirmed deletion;
it is not independent proof that object bytes have disappeared. Providers must
define their trust boundary explicitly. A conservative provider can acknowledge
durable receipt of the claim while retaining charged usage until authoritative
inventory confirms the adjustment. In that mode, acknowledgement does not mean
allowance is immediately available again. Document this behavior for operators
and usage displays. Do not treat arbitrary new event IDs as proof of distinct
deletions or subtract the same bytes repeatedly.

For inventory that replaces charged usage, serialize maintenance for the tenant
and establish quiescence of all writers. Pausing reservation requests alone does
not stop writes already admitted. Hold the exclusive maintenance lease throughout
the scan and accounting update; if another run could have resumed admission while
waiting for that lease, reaffirm the pause after acquiring it. Apply only a full
successful inventory, retain identities of superseded reservations, and prevent
delayed pre-inventory messages from subtracting the newly inventoried usage.

### Provider verification checklist

- Configured-but-never-inventoried storage denies new reservations, including
  after restart; a completed inventory initializes it.
- Concurrent reservations cannot exceed available capacity.
- A lost acknowledgement followed by a retry preserves the original operation.
- Reopening the ledger preserves holds, usage, denials and conflict detection.
- Stale reserve retries cannot authorize writes after settlement or inventory.
- Failed scans preserve accounting; concurrent maintenance cannot scan while
  another run resumes writers.
- Tenant identity comes from the trusted endpoint, not request fields.
