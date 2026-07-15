# Limits and boundaries

This page records limits implemented by PBVex v1 source. A deployment manifest can lower selected request/value limits; server embedding and storage flags can change the limits identified below. Do not rely on an unlisted number as a compatibility contract.

## Protocol, deployment, and runtime

| Area | Default / enforced limit | Can change? |
| --- | --- | --- |
| Decoded deployment bundle | 64 MiB maximum | No in v1 protocol. |
| Manifest `maxFunctionArgsBytes` | 1 MiB default; 16 MiB ceiling | A manifest may set a non-negative value up to the ceiling. |
| Manifest `maxReturnValueBytes` | 1 MiB default; 16 MiB ceiling | A manifest may set a non-negative value up to the ceiling. |
| Function request timeout | 30 seconds default | Manifest configuration can set the request timeout. |
| Goja pool | 5 runtime instances per deployment by default | Go embedding configuration only. |
| Nested calls | Depth 8; 64 total nested calls per invocation tree | No application configuration. |
| Wire codec | Depth 128; 16,384 nodes; 4 MiB internal wire-byte budget; 1,024 array entries/object fields | No application configuration. |
| Query `take`, `collect`, and page size | At most 1,024 documents | No application configuration. Use pagination. |
| Schema | 64 tables, 256 fields per table, 64 indexes per table, 16 fields per index | No application configuration. |
| Schema activation migration | 10,000 rows and 64 MiB of encoded migration data | No application configuration. |
| Deployment history | 10 retained entries by default | Go embedding configuration only. |

The deploy HTTP endpoint accepts a larger base64 JSON envelope so that a maximum decoded bundle fits; the decoded-bundle limit above is the meaningful artifact limit. Schema and descriptor validation also imposes identifier, path, field-name, and value-depth rules; see the [protocol ADR](../adr/001-protocol-v1.md) rather than copying protocol grammar into application code.

## Realtime and HTTP actions

| Area | Default / enforced limit | Can change? |
| --- | --- | --- |
| SSE connections | 1,000 total; 100 per IP | Go embedding configuration only. |
| Concurrent realtime query evaluations | 100 | Go embedding configuration only. |
| Realtime ping | Every 30 seconds | Go embedding configuration only. |
| SSE event payload | Active deployment return-value limit plus 4 KiB envelope allowance | Return-value manifest setting changes this. |
| HTTP headers | 100 values; 256-byte names; 8 KiB values; 64 KiB aggregate | No application configuration. |
| HTTP action request body | Active deployment argument-size limit | Manifest setting changes this. |
| Outbound HTTP request body | 1 MiB | No application configuration. |
| Outbound HTTP response body | 4 MiB, buffered | No application configuration. |
| Outbound HTTP timeout | 10 seconds default; 30 seconds maximum | Each call may lower it with `timeoutMs`. |

Realtime uses one SSE subscription query per request. Every successful record create, update, or delete invalidates all active subscriptions; bursts are coalesced, queries rerun, and unchanged canonical results are not sent. Deployment activation/rollback closes connections for reconnection against the new deployment. These limits do not turn PBVex into a distributed pub/sub system.

## Scheduler and storage

| Area | Default / enforced limit | Can change? |
| --- | --- | --- |
| Scheduler concurrency | 5 jobs | Go embedding configuration only. |
| Scheduler poll / lease / execution window | 2 seconds / 2 minutes / 2 minutes | Go embedding configuration only. |
| Scheduler attempts | 5; retry delay ranges from 1 second to 1 minute | Go embedding configuration only. |
| Scheduler retained history | 7 days; cleanup batch 1,000 | Go embedding configuration only. |
| Scheduled delay | At most 5 years | No application configuration. |
| Scheduled timestamp | At most 100 years in the future | No application configuration. |
| Recurring cron jobs | 64 per deployment; one-minute UTC precision | No application configuration. Expressions use the supported five-field cron format. |
| Storage upload file size | 64 MiB default | `--storageMaxFileSize` / `PBVEX_STORAGE_MAX_FILE_SIZE`. |
| Storage file count | Unlimited by default (`0`) | `--storageMaxFiles` / `PBVEX_STORAGE_MAX_FILES`. |
| Storage upload URL | 1 hour default | `--storageUploadTtl` / `PBVEX_STORAGE_UPLOAD_TTL`. |
| Storage download URL | 15 minutes default; requested lifetime cannot exceed 24 hours | Server embedding controls the absolute maximum; the default lifetime is flag/env configurable. |
| Storage cleanup | Every 5 minutes | `--storageCleanupInterval` / `PBVEX_STORAGE_CLEANUP_INTERVAL`. |

Storage also supports per-token maximum upload size (`--storageTokenMaxSize` / `PBVEX_STORAGE_TOKEN_MAX_SIZE`), clamped to the server file-size maximum, and MIME allowlists (`--storageAllowedTypes` / `PBVEX_STORAGE_ALLOWED_TYPES`). The [self-hosting guide](../self-hosting.md) lists all storage flags and environment variables.

## Runtime and topology incompatibilities

Deployed code is bundled JavaScript executed in Goja, not Node.js. Node built-ins, `require`, dynamic import, arbitrary npm runtime dependencies, asset imports, and a `process` global are unavailable. Function modules may use PBVex server/validator imports and relative TypeScript modules accepted by the bundler.

PBVex v1 is single-node: use one application per server process and one dedicated data directory. It has no consensus, distributed scheduler coordination, or distributed realtime coordination. Components are bounded to 1,024 definitions and dependency depth 32; they isolate mount data/IDs and do not provide cross-mount database access or component HTTP routes. For operational sizing, backups, external storage, and proxy boundaries, see [the self-hosting guide](../self-hosting.md).
