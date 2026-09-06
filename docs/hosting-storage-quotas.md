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
  reserve the worst-case byte bound before acknowledging an allow, under a
  lock or equivalent, so concurrent reservations cannot exceed the tenant
  allowance. Neither a successful transport nor a reservation is an actual
  write.
- **Worst-case before writes, actual after.** Callers reserve an upper bound
  before each write and settle to the actual stored byte count after the
  metadata commit. The provider clamps the charge to the reserved bound.
- **Idempotent retries preserve identity.** Reservations are identified by
  the caller's request ID; settlements and releases by reservation ID;
  deletion credits by event ID. Retries reuse identical IDs and content and
  receive the original acknowledgement. A new attempt needs a new ID. No
  exactly-once delivery claim is made.
- **Uncertain writes are never released.** A reservation ends in exactly one
  of `settle` (bytes persisted) or `release` (the write definitively did not
  persist). When the outcome is unknown — a possibly partial write, a failed
  cleanup, a crash — the reservation stays unsettled and the provider
  reconciles it. Fail-closed callers abort instead of writing unreserved
  bytes.
- **Fail-closed.** Denials and unavailability prevent the write. A denied
  upload never enters the storage backend, and a denied variant generation
  fails the download instead of storing an unbounded derived object.
- **Not covered here:** durable ledgers, reconciliation sweeps, billing
  periods, reservation expiry and the production provider itself. The
  reference service below is a bounded in-memory compatibility fixture, not
  a durable ledger.

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
| PBVex image variant | Worst-case derived-image bound reserved before generation | Actual object size settled after the write |
| PBVex deletion | — | Confirmed freed bytes credited (original + variants) |
| Native record create/update uploads | Exact multipart sizes reserved before upstream writes the files | Stored sizes settled after the record commit |
| Native update file replacements | New bytes reserved before the write | New bytes settled; old bytes credited after the synchronous removal |
| Native record deletion | — | Captured bytes credited at the deletion commit (see gap below) |
| Native on-demand thumbnails | Reserved before upstream generates a missing variant | Actual size settled after the request |

Standalone behavior is unchanged: with no observer attached, no reservation,
settlement or credit happens, and no native hooks are installed.

### PBVex storage paths

The storage service accepts a quota observer (`Service.SetQuotaObserver`).
Uploads reserve the effective staging cap before the request body is read;
staging failures release the reservation, failures after the backend persist
keep it when the stage object may still exist, and the metadata commit
settles the actual size. Lazy variants reserve a derived-image bound before
generation — a denied variant fails that download closed rather than storing
unreserved bytes — and settle the stored object size. Deletions credit the
byte total of the removed prefix (original plus variants) under the stable
`delete-<storageId>` identity. Reports use a cancellation-independent
context with a small bounded retry budget, so a caller timeout does not lose
a settlement; a lost report is logged for provider reconciliation.

### Native PocketBase record files

The pinned upstream performs record file uploads inside the system file
interceptor at priority 99 on `OnRecordCreateExecute`/`OnRecordUpdateExecute`
and post-commit removals at priority -99 on the after-success/after-error
hooks. The installed hooks use priorities on the safe side of those
boundaries: reservations run before any upload write, settlements and
credits run after the upstream commit or cleanup step, and failure handlers
release a reservation only when every uploaded object is verified absent —
an object that failed cleanup keeps its reservation for reconciliation.
Saves without file changes never consult the quota service. Denials abort
the save before any write and before the record exists.

Native record deletion is different upstream: the whole record files prefix
is deleted asynchronously after the delete transaction (`FireAndForget`),
so a synchronous absence check is impossible. The credit therefore reports
the captured byte total at the deletion commit; a background deletion that
later fails can transiently over-credit until the provider reconciles the
object store.

### Native on-demand thumbnails

Upstream generates missing record thumbnails inside the files download
route, after routing and authorization but before serving, with no core hook
in between (`OnFileDownloadRequest` fires after generation). The installed
global request middleware reserves before that write by replicating the
upstream generation conditions exactly — selector validated against the
field thumbs and the built-in default, original present, image content type,
variant missing — so requests that upstream serves without writing are never
blocked or double-counted. Requests that would write share one reservation
through an in-process gate keyed by the variant; a denied or failed
generation denies the concurrent followers closed instead of letting them
retry the write unreserved. The bound derives from the original's decoded
dimensions (RGBA pixels plus encoder overhead) and settles to the stored
object size.

## Documented gaps

- **Backup archives are not reserved.** Allowed backup creation writes a zip
  archive through the backups filesystem with no pre-write size bound; the
  `backup.create` policy capability is the intended throttle. Restores
  remain unconditionally rejected while hosting is enabled, so a restore
  cannot write unreserved bytes either.
- **Failed native cleanups keep reservations.** If upstream cannot delete a
  failed upload's objects, the reservation stays unsettled by design; the
  bytes may still exist and providers reconcile.
- **Record-delete credits are optimistic.** Because the upstream deletion of
  record files is asynchronous, the credit is committed when the metadata
  deletion commits and can transiently over-credit if the background
  deletion fails.
- **Variant bounds are approximations.** The pre-generation bound is a
  conservative encoder bound at the validated variant dimensions; the
  settlement trims the charge to the actual object size.
- **Reconciliation is a provider duty.** Unknown-settled reservations
  (process crash between reserve and settle), lost reports after exhausted
  bounded retries, and failed background deletions all require the
  documented provider reconciliation pass over the actual object store.
  The protocol supports reconciliation; it does not fake a durable host
  service.
- **Native thumbnail gate is route-level.** Because upstream lacks a core
  hook before thumbnail generation, the gate is a request middleware that
  mirrors upstream's route behavior. Upstream changes to the files route
  conditions must be mirrored; the pinned version is documented in the
  implementation.

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
unavailability to `storage.ErrQuotaUnavailable`; both fail the write closed.
A client that cannot reach the socket on a dead path produces the same
fail-closed behavior. Focused validation lives in
`backend/internal/storage` (reservation ordering, denial fail-closed,
uncertain-write retention, variant and deletion accounting, concurrent
uploads against a capacity-limited observer, native record hooks and the
thumbnail gate against the real router) and in
`backend/hosting/storagequota` (wire round-trips, idempotent replays,
conflicts, clamping, atomic capacity under concurrency, saturation and
malformed responses).
