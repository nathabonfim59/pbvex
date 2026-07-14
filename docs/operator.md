# PBVex Backend Operator Guide

This guide covers the self-contained `pbvex` server executable. The binary
contains PocketBase, the admin UI, and all PBVex backend services. It does not
need Node.js, the TypeScript packages, or a repository checkout at runtime.

PBVex v1 is single-node software. Run exactly one process for a data directory.

## Install and start

Choose the release archive for the server platform, verify it against
`checksums.txt`, and extract `pbvex` (`pbvex.exe` on Windows).

```bash
tar -xzf pbvex_0.1.0_linux_amd64.tar.gz
chmod +x pbvex
mkdir -p /srv/pbvex
cd /srv/pbvex
/usr/local/bin/pbvex --dir ./pb_data serve --http 127.0.0.1:8090
```

Without `--dir`, data defaults to `./pb_data` in the current working directory,
never beside the executable. The executable may live in a read-only directory.

Health check:

```bash
curl -f http://127.0.0.1:8090/api/health
```

## Bootstrap deployment credentials

Create the first PocketBase superuser while the service is stopped or before
exposing it publicly:

```bash
pbvex --dir ./pb_data superuser create admin@example.com 'strong-password'
```

Obtain its short-lived auth token through PocketBase:

```bash
curl -sS http://127.0.0.1:8090/api/collections/_superusers/auth-with-password \
  -H 'Content-Type: application/json' \
  -d '{"identity":"admin@example.com","password":"strong-password"}'
```

Use the response's `token` as `PBVEX_TOKEN`. Deployment upload, list,
activation, rollback, and scheduler administration require a superuser token.
Do not commit tokens to `pbvex.config.ts`; use environment variables or the
gitignored `.pbvex/credentials.json`.

From the application project:

```bash
PBVEX_TOKEN='<token>' pnpm exec pbvex deploy --url http://127.0.0.1:8090
```

## Reverse proxy, public URL, and CORS

Bind plain HTTP to a private interface and terminate TLS in Nginx, Caddy, or a
load balancer. Forward the original `Host`, `X-Forwarded-For`, and
`X-Forwarded-Proto` values. PocketBase can also manage TLS through its `--https`
serve option.

Set PocketBase **Settings > Application > Application URL** to the canonical
external origin. PBVex uses this trusted setting when constructing HTTP action
`Request.url` values and storage URLs; it never trusts an arbitrary inbound
Host header for that purpose. The URL must use HTTP or HTTPS and must not
contain userinfo, query, or fragment components.

The default PBVex CORS policy allows any origin without credentialed-cookie
mode and permits `Authorization`, `Content-Type`, and `X-Request-Id`. Deploy a
custom Go embedding configuration if a restricted origin allowlist is needed.

## Persistent state and backup

Persistent state lives below the configured data directory:

- `data.db`: PocketBase records, deployments, jobs, component state, storage
  metadata, signing keys, and PBVex system collections.
- `auxiliary.db`: PocketBase auxiliary state.
- `storage/`: locally stored objects and PocketBase-managed files.
- `backups/`: PocketBase backups.

Stop the process before copying the directory directly, or use PocketBase's
backup API. Back up before every binary downgrade or upgrade. When an external
S3-compatible filesystem is configured through PocketBase, back up that bucket
under its provider's consistency and retention rules as well.

PBVex workers recover durable scheduler leases, staged uploads, and cleanup
state after restart. A backup must include both the database and external file
objects to preserve that relationship.

Mounted components use deterministic, path-derived namespaces and separate
physical collections. Removing a mount or a component table leaves its owned
data dormant so the same mount path can restore it later. Renaming a mount
intentionally selects a different namespace. Back up the component catalog and
its physical collections together with the rest of `data.db`.

## Storage configuration

Storage works with defaults, but production deployments should set an
application URL and review these flags/environment variables:

| Flag | Environment | Purpose |
| --- | --- | --- |
| `--storageMaxFileSize` | `PBVEX_STORAGE_MAX_FILE_SIZE` | Maximum bytes per upload |
| `--storageTokenMaxSize` | `PBVEX_STORAGE_TOKEN_MAX_SIZE` | Default upload-token limit |
| `--storageMaxFiles` | `PBVEX_STORAGE_MAX_FILES` | Active file cap; `0` is unlimited |
| `--storageAllowedTypes` | `PBVEX_STORAGE_ALLOWED_TYPES` | Comma-separated MIME patterns |
| `--storageUploadTtl` | `PBVEX_STORAGE_UPLOAD_TTL` | Upload URL lifetime |
| `--storageUrlSignTtl` | `PBVEX_STORAGE_URL_SIGN_TTL` | Download URL lifetime |
| `--storageCleanupInterval` | `PBVEX_STORAGE_CLEANUP_INTERVAL` | Cleanup pass interval |
| `--storageBaseUrl` | `PBVEX_STORAGE_BASE_URL` | Explicit absolute public base URL |
| `--storageBasePath` | `PBVEX_STORAGE_BASE_PATH` | Storage API path |
| `--storageFilePrefix` | `PBVEX_STORAGE_FILE_PREFIX` | Object-key prefix |

Durations use Go duration syntax such as `30s`, `15m`, or `24h`. Invalid
explicit values fail startup rather than silently weakening limits.

## Scheduler operations

The scheduler is durable and pins the deployment snapshot associated with each
job. It retries bounded failures and resumes eligible work after restart.
Administrative endpoints are under `/api/pbvex/jobs` and require a superuser:

- `GET /api/pbvex/jobs`
- `GET /api/pbvex/jobs/{id}`
- `POST /api/pbvex/jobs/{id}/cancel`
- `POST /api/pbvex/jobs/{id}/retry`

The v1 binary uses tested scheduler defaults for concurrency, polling, leases,
retry, and retention. They are Go embedding settings rather than command-line
flags.

## HTTP actions and public routes

Application calls use `POST /api/pbvex/call`; realtime uses
`POST /api/pbvex/realtime` with a bounded GET compatibility fallback. Deployed
HTTP actions are mounted below `/api/pbvex`. Reserved deployment, call,
realtime, job, and storage routes always take precedence over application
catch-all routes.

Application auth tokens are optional on public calls and realtime requests.
When present, PocketBase validates the record token and PBVex exposes the
portable identity through `ctx.auth.getUserIdentity()`.

Document IDs emitted by current deployments use the authenticated `pbv2`
envelope and bind the logical table to `root` or the exact component namespace.
They are opaque capabilities: persist and pass them through the SDK without
constructing or rewriting them. Legacy authenticated root `pbv1` IDs remain a
root-only migration compatibility path and are never valid inside components.

## Upgrade and rollback

1. Stop the process.
2. Back up the data directory and external object storage.
3. Replace the executable.
4. Start it and wait for `/api/health`.
5. Verify a representative call, realtime subscription, scheduled job, and
   storage download.

To downgrade, restore both the old executable and the matching backup. An
application deployment rollback is separate and uses
`POST /api/pbvex/deployments/{id}/rollback`; it does not replace the binary.

PBVex intentionally has no self-update command. Only install artifacts from
the PBVex release pipeline.

## Common flags

- `--dir <path>`: data directory, default `./pb_data`.
- `serve --http <addr>`: HTTP listener, default `127.0.0.1:8090`.
- `serve --https <domain>`: PocketBase-managed HTTPS.
- `--publicDir <path>` and `--indexFallback`: static application files.
- `--hooksDir`, `--hooksWatch`, `--hooksPool`: PocketBase JS hooks.
- `--migrationsDir`, `--automigrate`: user migrations.
- `--dev`: development logging.
- `--version`: binary version.

Run `pbvex --help` and `pbvex serve --help` for the authoritative flags in the
installed version.
