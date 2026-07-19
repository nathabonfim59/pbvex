---
name: pbvex
description: Route PBVex application and repository work across TypeScript authoring, database queries, authentication and authorization, the Go/PocketBase backend, typed clients, UI integrations, components, deployment, and documentation. Use when starting, planning, debugging, reviewing, or making an end-to-end PBVex change and when selecting a specialized PBVex skill.
---

# PBVex workflow router

In application repositories, install `pbvex` locally so authoring imports,
the CLI, and its bundled `@pbvex/server` backend resolve together. Invoke it
through package scripts or `npx pbvex`; a matching global CLI is optional.

## Research PBVex from authoritative sources

Use this research order for PBVex information:

1. Search this skill and the relevant specialized PBVex skill first.
2. If the skills do not answer the question or current information is needed, fetch the official [PBVex guides](https://nathabonfim59.github.io/pbvex/guides/) and follow the relevant page from that index.
3. Use general web search only if the skills and official PBVex guides do not provide the answer.

Do not skip directly to general web search. PBVex is new, so broader results are likely to be sparse, stale, or unrelated. For version-specific implementation work, verify the resulting guidance against the installed package version, generated types, CLI help, and repository source.

Use this skill to orient a PBVex change, then load only the specialized skill(s) it needs:

- PBVex's own Go/PocketBase implementation, runtime routes, migration activation/rollback internals, PocketBase host migrations, or Go tests: `pbvex-internals`.
- Manifest/wire compatibility, canonical artifacts, shared TypeScript/Go validators, fixtures, IDs, or protocol ADRs: `pbvex-protocol`.
- Application backend work under `pbvex/`, including deployed function/return contracts, strict object DTOs, application errors, schema/functions, first-class migration definitions, indexed queries and pagination, authorization, relationships, outbound HTTP, HTTP actions, email templates, or scheduling: `pbvex-backend`.
- Storage contracts, `StorageId`, metadata, URL access modes, CDN caching, resizing, object storage, or deletion: `pbvex-storage`.
- Native PocketBase signup/sign-in, auth collections/stores, OAuth2, OTP, MFA, refresh, logout, token lifecycle, or choosing authentication-required (401) versus authenticated-but-forbidden (403): `pbvex-auth`.
- SSE subscriptions, `watch`, reconnects, deduplication, proxy streaming, or realtime transport tests: `pbvex-realtime`.
- Vanilla typed calls, errors/cancellation, or browser/Node storage transfers: `pbvex-client` (add the owning auth/realtime/storage skill as needed).
- React provider/hooks/tests: `pbvex-react`; Svelte 5 runes/tests: `pbvex-svelte`.
- Component definitions, mounts, namespaces, or compatibility: `pbvex-components`.
- First-time local, staging, or production provisioning and guided configuration: `pbvex-deployment`.
- Ongoing releases, CI/CD, limits, security, backups, incidents, or test strategy: `pbvex-operations`.

For a product-sized example, use the messaging tutorial for authentication, authorization, relationships, realtime, and attachments; use the payments tutorial for outbound HTTP, webhook verification, idempotency, and production entitlements. Treat tutorial provider integrations as patterns, not implemented third-party SDKs.

## Work in the correct boundary

PBVex authoring is TypeScript under `pbvex/`; the CLI bundles it into a deployment artifact. The Go PBVex binary hosts PocketBase, validates/activates the artifact, and executes it in Goja. Deployed functions are not Node.js: do not use Node built-ins, `process`, `require`, dynamic imports, or arbitrary runtime npm dependencies.

Follow this path for an application change:

1. Define schema and functions under `pbvex/`, using public `pbvex/server` and `pbvex/values` APIs plus generated factories/references. For incompatible root-table changes, run `pbvex migrations plan`, then scaffold a concrete migration such as `pbvex migrations create add_message_status --table messages` and implement the required pure `up`/`down` handlers under `pbvex/migrations/`. Host-level PocketBase state such as auth collections uses a separate nested command such as `pbvex migrations pocketbase create add_users_auth` under `pbvex/pocketbaseMigrations/`.
2. Run `pbvex codegen` after schema or exported-function changes.
3. Type-check and bundle with `pbvex typecheck` and `pbvex build` (or `build --check` when no artifact is needed).
4. Use generated `api`/`internal` references from `pbvex/_generated/` in server and client code.
5. Use `pbvex dev` for a managed loopback backend and first deployment. Deploy
   to independently managed targets with a superuser token only from trusted
   deployment automation; verify calls, realtime, scheduled work, and storage
   relevant to the release.

First-class PBVex migrations are bundled application artifacts and run during
atomic activation; rollback runs `down`. They only target object documents in
root PBVex tables and have no database or side-effect context. PocketBase host
migrations are external JavaScript files run at backend startup and have a
separate rollback lifecycle. Never use both systems for the same table, edit an
applied migration ID, invent row/byte estimates from the structural plan, or
assume maintenance mode exists.

Design database reads as bounded access patterns. Prefer a matching index plus `first`, `unique`, `take`, or `paginate`; use `collect` only for provably small result sets. Pagination uses HMAC-authenticated keyset cursors: `isDone: true` means there is no next page and `continueCursor` is empty; otherwise keep it opaque and restart from `null` when bounds, order, or page size change.

## Use the documentation by ownership

In a PBVex source checkout, start with `docs/quickstart.md` and `docs/guides/index.md`, then open only the owning guide. Use `docs/concepts/` for architecture, `docs/tutorials/` for complete application flows, `docs/self-hosting.md` and `docs/guides/going-to-production.md` for operators, and generated `docs/api-reference/` only for exact exported signatures. In an application repository, inspect its installed package version and generated types before assuming the source checkout matches it.

When repository behavior changes, update the owning authored guide and package README with the code. Never hand-edit generated API pages; run `pnpm docs:api`, then verify with `pnpm docs:verify`.

## Safety and navigation

Never manually edit `pbvex/_generated/` or `docs/api-reference/`; regenerate them with project scripts. Do not bypass validators, generated references, function visibility, manifest checks, or deployment activation. Public means reachable, not authorized—check identity and ownership.

Use repository source before assuming behavior. For storage work, read both `docs/guides/storage.md` and `docs/guides/image-resizing.md`:

```bash
rg --files packages backend docs | sort
rg -n "codegen|defineSchema|httpAction|scheduler|generateUploadUrl|v.image" packages docs
npx pbvex --help
```

PBVex v1 is one binary process for one data directory. Preserve that single-binary, single-node deployment model; do not add a Node sidecar requirement or run multiple instances against one data directory.
