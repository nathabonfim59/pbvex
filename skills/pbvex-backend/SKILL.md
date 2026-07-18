---
name: pbvex-backend
description: Build, debug, review, or operate the Go PBVex/PocketBase backend, including runtime contracts, HTTP and realtime routes, schema activation, migrations, and the single-binary deployment boundary. Use for changes under backend/ or backend behavior rather than TypeScript application functions.
---

# PBVex Go backend

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

First-class definitions from `pbvex/migrations/*.ts` are bundled and registered with the candidate deployment. Activation resolves schema-hash chains, checks durable ID/checksum history, runs `up`, normalizes defaults/projections, records history, and changes the active deployment in one transaction. Deployment rollback runs `down` in reverse order under the same atomic guarantee. Preserve root-table/object-only scope, immutable `_id`/`_creationTime`, the pure synchronous context, mandatory `down`, the fixed 10,000-document/64-MiB hard ceilings, and structured warnings at 80%. `pbvex migrations plan` is structural only; do not add fake record or byte estimates. Maintenance mode is not implemented.

Use PocketBase migrations/collections through the separate host lifecycle. Application-owned host migrations live in `pbvex/pocketbaseMigrations/`; create them with the nested command `pbvex migrations pocketbase create <name>` so they reference the generated `pbvex/_generated/pocketbase.d.ts`, and validate them with `pbvex typecheck`. They run at backend startup and are not deployment artifacts or reversed by PBVex rollback. The npm CLI override is `--pocketbaseMigrationsDir`; the direct backend flag remains `--migrationsDir`. Use host migrations for PocketBase state such as auth collections, not tables owned by `pbvex/schema.ts`.

## Single binary and tests

Build with `(cd backend && go build ./cmd/pbvex)`; run it from the working directory that owns `./pb_data` (or pass `--dir`). The runtime needs no Node.js, repository checkout, or TypeScript package at runtime. Run exactly one process for a data directory—there is no multi-node locking, scheduler coordination, or realtime clustering.

Add focused package tests plus `go test ./...`; use existing end-to-end tests around routing, activation, auth, realtime, scheduler, and storage when changing their contracts. Preserve the disabled upstream self-update behavior: releases replace the PBVex binary through its release process.
