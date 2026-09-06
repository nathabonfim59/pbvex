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
A single provider socket can serve both the `/v1` policy routes and the
`/v1/storage/` routes below. The same `hosting.Config` bootstraps both
clients.

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
  persisted, and native thumbnail generation is denied outright while
  quotas are enforced (see coverage below). Settled usage may only be
  freed by a verified credit or provider reconciliation; the provider
  clamps settlements to the reserved bound, so a caller cannot raise its
  own charge.
- **Sanitized diagnostics.** Provider, transport and custom observer errors
  are never logged or returned verbatim; only fixed classifications and
  protocol identifiers cross the log stream.
- **Not covered here:** durable ledgers, reconciliation sweeps, billing
  periods, reservation expiry and the production provider itself. The
  reference service below is a bounded in-memory compatibility fixture, not
  a durable ledger. **Backup archives are not covered by reservations**:
  while quotas are enforced, the platform must deny `backup.create` (the
  policy protocol's capability gate) because an allowed backup writes
  archive bytes with no pre-write size bound. Restores remain
  unconditionally rejected while hosting is enabled.

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
transitions the reservation to the actual stored count (clamped to the
reserved bound) and acknowledges `{"reservationId":"r1","chargedBytes":4096}`.
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
| Native on-demand thumbnails | Generation DENIED while quotas are enforced (no reservation is possible) | Cached variants keep serving |

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
path. While a quota observer is attached, the installed global request
middleware therefore fails closed: any request upstream would serve by
GENERATING a variant is denied before the write. Cached variants keep
serving, requests upstream serves without writing (invalid selector,
missing original, non-image original) keep their upstream behavior, and
standalone deployments (no observer) generate exactly as upstream ships.
The generation conditions mirror the pinned upstream route; if upstream
changes them, the gate errs closed — it may deny a request upstream would
no longer write, never allow an unreserved write.

## Documented gaps

- **Backup archives are not reserved.** An allowed backup creation writes a
  zip archive through the backups filesystem with no pre-write size bound.
  Until this path is covered, the platform must deny the `backup.create`
  capability whenever storage quotas are enforced; the quota layer itself
  does not and cannot cover it. Restores remain unconditionally rejected
  while hosting is enabled, so a restore cannot write unreserved bytes
  either.
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
- **Native thumbnail gate is route-level and denies closed.** Because
  upstream lacks a core hook before thumbnail generation (and its writer
  hook carries no byte count), enforced-mode deployments cannot serve
  freshly generated native thumbnails; the gate denies the generation and
  the platform should surface this as an expected restriction. Upstream
  changes to the files route conditions must be mirrored; the pinned
  version is documented in the implementation. PBVex's own variant path
  (encode-then-reserve) is not affected.

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
while identical retries still work), loses everything on restart, and
performs no reconciliation. **It is not a production durable ledger or
quota implementation.**

The embedding side wires three calls (the parent application owns the
configuration surface):

```go
client, err := storagequota.NewClient(cfg.Hosting) // hosting.Config, enabled
observer := storage.NewHostedQuotaObserver(client)
storageService.SetQuotaObserver(observer)
storageService.InstallNativeQuotaHooks(app) // core.App: native record hooks + thumb gate
```

`NewHostedQuotaObserver` maps denials to `storage.ErrQuotaDenied` and
everything else — transport failures, saturation, malformed responses, any
custom observer error — to `storage.ErrQuotaUnavailable`; raw provider
error text never propagates into errors or logs. Both outcomes fail the
write closed. Focused validation lives in `backend/internal/storage`
(reservation ordering, denial fail-closed, uncertain-write retention,
exact variant reservations, deletion crediting only on verified removal,
record-deletion usage retention, concurrent uploads against a
capacity-limited observer, native record hooks and the thumbnail gate
exercised end to end through the real router) and in
`backend/hosting/storagequota` (wire round-trips, idempotent replays,
conflicts, settlement clamping to the reserved bound, atomic capacity under
concurrency, saturation and malformed responses).
