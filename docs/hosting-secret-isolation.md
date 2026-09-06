# Hosted secret and configuration isolation

PBVex hosting integration assumes the tenant superuser is untrusted for
platform configuration: they hold full PocketBase superuser credentials, but
must not be able to read, overwrite, or exfiltrate host-owned configuration
and secrets. This document describes the isolation mechanisms implemented on
top of the [local policy protocol](./hosting-policy-protocol.md). It is
experimental and its exact boundaries are stated below — read the
limitations section before treating any claim as complete.

## Isolation model

Hosted mode combines three layers:

1. **Capability gates** (policy protocol): dynamic provider checks for
   changed storage, backups, and SMTP settings categories, backup creation,
   and backup downloads. A provider outage fails closed.
2. **Host-managed injection**: host-owned storage and mail configuration is
   supplied through the process environment, applied to the running app, and
   never persisted in the tenant database. The corresponding settings
   categories are locked; no capability grant unlocks them because they are
   deployment configuration, not dynamic policy.
3. **Native path restrictions**: PocketBase routes without dedicated hooks
   (direct SQL, collection import, backup upload/download) are restricted so
   they cannot be used to read persisted settings or bypass record-level
   protections.

Standalone deployments (hosting disabled) keep normal PocketBase behavior:
no restrictions, and `PBVEX_SMTP_*` overrides persist as documented in the
self-hosting guide.

## Host-managed storage

Set `PBVEX_HOST_STORAGE_S3_ENABLED=true` together with the connection fields
below to inject host-owned S3-compatible storage:

| Variable | Meaning |
| --- | --- |
| `PBVEX_HOST_STORAGE_S3_ENABLED` | Must be `true` to activate managed storage |
| `PBVEX_HOST_STORAGE_S3_BUCKET` | Managed bucket name |
| `PBVEX_HOST_STORAGE_S3_REGION` | Managed bucket region |
| `PBVEX_HOST_STORAGE_S3_ENDPOINT` | S3-compatible endpoint URL |
| `PBVEX_HOST_STORAGE_S3_ACCESS_KEY` | Managed access key ID |
| `PBVEX_HOST_STORAGE_S3_SECRET` | Managed secret key |
| `PBVEX_HOST_STORAGE_S3_FORCE_PATH_STYLE` | Optional, `true`/`false` |

These variables are environment-only (no CLI flags) so the secret key cannot
leak through `argv` or shell history. Invalid or partial configuration fails
startup. Managed storage requires hosting integration to be enabled;
`RegisterCore` rejects the combination otherwise.

Behavior while active:

- Record files and backup archives use the managed bucket. PocketBase builds
  its file and backups filesystems from the in-memory settings on every use,
  so PBVex storage, native record uploads, and backup creation all target the
  managed bucket without any per-path plumbing.
- The persisted settings keep the neutral baseline (`s3` and `backups.s3`
  disabled and empty). Every settings save passes through a neutralization
  hook, so the injected values and their secrets never reach the database,
  backup archives, or a restore.
- The `s3` settings category and the `backups.s3` sub-category are locked.
  A tenant superuser PATCH that changes them is rejected with `403`. The
  unchanged values shown by `GET /api/settings` round-trip cleanly, so
  ordinary edits of unrelated categories keep working.
- `backups.cron` and `backups.cronMaxKeep` stay editable under the existing
  `settings.backups.write` capability.
- Enabling managed storage rewrites any previously persisted storage
  configuration at startup (the baseline save). Migrate existing file data
  into the managed bucket before switching a populated deployment, because
  lookups follow the active managed configuration.

## Host-managed mail (SMTP)

`PBVEX_SMTP_*` variables configure the process mail settings. Their
persistence behavior depends on the mode:

- **Standalone** (hosting disabled): unchanged. Provided values are written
  to the persisted settings on every boot.
- **Hosted** (hosting enabled): the values are host-owned. They are applied
  to the in-memory settings only — the persisted mail settings stay neutral —
  and the whole `smtp` category is locked against tenant edits with `403`,
  including disabling. Mail delivery keeps using the host configuration, and
  the SMTP password never exists in the database, in backup archives, or in
  settings responses (PocketBase masks the SMTP password in JSON output).

Providing `PBVEX_SMTP_*` without `PBVEX_SMTP_ENABLED` in hosted mode leaves
the mail settings tenant-editable under the `settings.smtp.write`
capability, as in standalone mode.

## Settings responses and backups

- `GET /api/settings` and the settings PATCH response serialize the active
  (shadowed) settings. PocketBase masks the secret fields — the S3 secret
  keys and the SMTP password are omitted from JSON — so no host secret value
  leaves the process. The non-secret connection fields of managed categories
  (endpoint, bucket, region, access key ID, SMTP host/port/username) are
  rendered as configured; see limitations.
- Backup archives embed the tenant database. Because the persisted settings
  row never contains managed values, newly created archives contain neither
  host secrets nor managed connection fields. Archives created before
  hosting was enabled may still contain older persisted secrets, which is
  one reason downloads are gated (below).

## Backup and export restrictions

Hosted mode restricts the native routes that could otherwise read persisted
settings or bypass record-level protections. These gates are Go-side, ahead
of the operation:

| Route | Behavior in hosted mode |
| --- | --- |
| `POST /api/backups` | `backup.create` capability check (existing) |
| `GET/HEAD /api/backups/{key}` | `backup.download` capability check; denied when the provider is unavailable |
| `POST /api/backups/{key}/restore` | Denied before scheduling, with a `403` response; the `OnBackupRestore` hook refusal remains as a second layer |
| `POST /api/backups/upload` | Denied: restore is unavailable, so an uploaded archive has no legitimate use and would turn the backups storage into blob storage |
| `DELETE /api/backups/{key}` | Allowed: removes the tenant's own archive |
| `GET /api/backups` | Allowed: name/size metadata only |
| `POST /api/sql` | Denied: arbitrary SQL can read the persisted settings row and bypasses every record-level protection |
| `POST /api/collections/import` | Denied before any side effect: the import path saves without per-model validation and could rewrite system collections; hosted tenants change schema through deployments |
| `POST /api/settings/test/s3` | Denied while storage is host-managed: the connection test would exercise the host credentials |
| `POST /api/settings/test/email` | Denied while SMTP is host-managed: the test would send mail through the host relay to an arbitrary recipient |

The `backup.download` capability is denied unless the provider explicitly
grants it, which also keeps pre-hosting archives from being exfiltrated by a
tenant superuser.

## Bootstrap and validation summary

- Managed storage requires `--hostingEnabled`; otherwise startup fails.
- `PBVEX_HOST_STORAGE_S3_ENABLED=true` requires every connection field;
  `forcePathStyle` defaults to false.
- Credentials present with `ENABLED` unset or `false` fails startup rather
  than being silently ignored.
- `PBVEX_HOST_STORAGE_S3_*` accepts no CLI flags by design.
- The policy handshake still gates startup exactly as in the policy protocol
  document; the managed locks above do not depend on the provider being
  reachable after startup, so an unrelated settings edit keeps working when
  the provider is temporarily unavailable.

## Limitations and honest boundaries

- **Non-secret fields are tenant-visible.** Masking covers secret values
  only. A tenant superuser can read the managed endpoint, bucket, region,
  access key ID, and SMTP connection fields from settings responses. The
  access key ID alone does not grant bucket access (the secret key never
  leaves the process), but it does disclose infrastructure naming. Hosts
  that consider this sensitive should treat tenant superuser accounts as
  full platform-break-glass credentials and issue them accordingly.
- **Collection definitions are outside this audit.** Individual collection
  create/update/delete endpoints and raw superuser record access to
  non-backing collections are unchanged; only collection *import* is denied.
  PBVex system-record and backing-collection protections remain as
  documented in the policy protocol. A complete collection-schema audit is
  not claimed.
- **Diagnostics that stay allowed can surface provider SDK error strings**
  (for example S3 errors from ordinary upload failures). Host secret values
  are not part of those operations, but endpoint/bucket names may appear in
  error messages visible to the tenant.
- **Managed storage and backups share one configuration.** A dedicated
  backups bucket with its own credentials is not yet configurable; the
  managed bucket receives both record files (`storage/` keys) and backup
  archives (root-level keys).
- **Static injection only.** Changing the managed storage or SMTP
  configuration requires a process restart. There is no tenant-readable
  secret store and no dynamic secret rotation socket yet.
- **SMTP lock granularity.** When `PBVEX_SMTP_*` is provided in hosted
  mode, the entire mail category is locked even for fields the host did not
  provide; unprovided fields fall back to the persisted values and cannot be
  changed through the API. Hosts should provide a complete mail
  configuration, including the password.
- **Restore remains fully denied.** Provider-allowed restore (with
  post-restore policy reapplication) is future work; a restored archive
  could otherwise reintroduce arbitrary settings and executable files.

## Validation

Focused regression coverage lives in `backend/internal/pbvex`
(`hosting_managed_test.go`, `hosting_native_paths_test.go`,
`hosting_test.go`) and `backend/cmd/pbvex/main_test.go`: shadow and
persisted-baseline invariants, unrelated-edit preservation, managed-category
denial before side effects, whole-settings-save neutralization, archive
secret scanning, download gating including provider outage, native path
denials with unchanged collections, standalone SMTP persistence, and env
validation. Run the backend gates from `CONTRIBUTING.md` before a PR.
