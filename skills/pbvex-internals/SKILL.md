---
name: pbvex-internals
description: Modify, debug, review, or operate PBVex's own Go/PocketBase implementation, including runtime contracts, HTTP and realtime routes, storage internals, schema activation, migrations, and the single-binary boundary. Use for PBVex repository work under backend/, not for building an application backend or a product platform with PBVex.
---

# PBVex implementation internals

Start with the public executable boundary in `backend/cmd/pbvex/main.go` and the application host in `backend/internal/pbvex/`. Inspect the owning package before changing a contract:

```bash
rg -n "Register|Route|realtime|activation|migration" backend/internal
(cd backend && go test ./... && go vet ./...)
```

## Preserve runtime contracts

The binary embeds PocketBase and runs uploaded TypeScript bundles in Goja. It must validate deployment envelopes, manifests, bundle hashes/sizes, function descriptors, wire values, visibility, and component mounts before activation. Activation compiles functions and materializes compatible schema/index changes transactionally; on failure the prior active deployment stays active.

Keep the distinction clear:

- Public TypeScript authoring APIs are exported by `pbvex`, `pbvex/server`, `pbvex/values`, and generated files.
- `backend/internal/**` Go packages are implementation details, not APIs for application TypeScript or external Go consumers.

Do not expose an internal function through the public call path, weaken argument/return validation, accept handwritten component IDs, or bypass transaction/activation checks.

## HTTP, realtime, and data lifecycle

Keep platform routes ahead of application HTTP actions: deployment, call, realtime, jobs, and storage are reserved under `/api/pbvex`. Public call routing, optional PocketBase record-token identity, and HTTP-action route validation are server-owned. Realtime query subscriptions use SSE and must close/reconnect across deployment activation or rollback.

Preserve immutable deployment snapshots while admitted realtime work or durable scheduler jobs reference them. Jobs execute the snapshot captured at enqueue, not whichever deployment is active later; trimming, retry, activation, and rollback must retain or reject snapshots deterministically. Test old-snapshot execution and forced realtime reconnects.

Storage keeps bytes in PocketBase's local/S3 filesystem and metadata in internal collections. Preserve single-use upload claims, schema-bound image-policy snapshots, byte-derived MIME/dimensions, identity/capability/public authorization, public cache headers, predefined-only lazy thumbnails, decode concurrency/size limits, prefix deletion, and orphan recovery. Never put file bytes in SQLite or let `thumb` become an arbitrary signed transformation. Use `pbvex-storage` for the public contract.

First-class definitions from `pbvex/migrations/*.ts` are bundled and registered with the candidate deployment. Activation resolves schema-hash chains, checks durable ID/checksum history, runs `up`, normalizes defaults/projections, records history, and changes the active deployment in one transaction. Deployment rollback runs `down` in reverse order under the same atomic guarantee. Preserve root-table/object-only scope, immutable `_id`/`_creationTime`, the pure synchronous context, mandatory `down`, the fixed 10,000-document/64-MiB hard ceilings, and structured warnings at 80%. `pbvex migrations plan` is structural only; do not add fake record or byte estimates. Maintenance mode is not implemented.

Use PocketBase migrations/collections through the separate host lifecycle. Application-owned host migrations live in `pbvex/pocketbaseMigrations/`; create one with a concrete command such as `pbvex migrations pocketbase create add_users_auth` so it references the generated `pbvex/_generated/pocketbase.d.ts`, and validate it with `pbvex typecheck`. They run at backend startup and are not deployment artifacts or reversed by PBVex rollback. The npm CLI override is `--pocketbaseMigrationsDir`; the direct backend flag remains `--migrationsDir`. Use host migrations for PocketBase state such as auth collections, not tables owned by `pbvex/schema.ts`.

## Single binary and tests

Build with `(cd backend && go build ./cmd/pbvex)`; run it from the working directory that owns `./pb_data` (or pass `--dir`). The runtime needs no Node.js, repository checkout, or TypeScript package at runtime. Run exactly one process for a data directory—there is no multi-node locking, scheduler coordination, or realtime clustering.

Add focused package tests and use existing end-to-end tests around routing, activation, auth, realtime, scheduler, and storage. Protocol changes also require `pbvex-protocol`. From the repository's `backend/` directory, run `test -z "$(gofmt -l .)"`, `go vet ./...`, `go test -count=1 ./...`, `CGO_ENABLED=1 go test -race -count=1 ./...`, and `go build ./cmd/pbvex` before a PR, as documented in `CONTRIBUTING.md`. Preserve the disabled upstream self-update behavior: releases replace the PBVex binary through its release process.
