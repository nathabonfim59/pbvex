---
name: pbvex
description: Route PBVex application and repository work across TypeScript authoring, database queries, authentication and authorization, the Go/PocketBase backend, typed clients, UI integrations, components, deployment, and documentation. Use when starting, planning, debugging, reviewing, or making an end-to-end PBVex change and when selecting a specialized PBVex skill.
---

# PBVex workflow router

In application repositories, install the CLI globally with `npm install --global pbvex` and keep a matching local `pbvex` dependency so authoring imports resolve. Invoke the global CLI directly as `pbvex`.

Use this skill to orient a PBVex change, then load only the specialized skill(s) it needs:

- Backend binary, PocketBase embedding, runtime route, migrations, or Go tests: `pbvex-backend`.
- `pbvex/` schema/functions, indexed queries and pagination, authorization, relationships, outbound HTTP, HTTP actions, email templates, or scheduling: `pbvex-functions`.
- Vanilla typed calls, PocketBase application auth, errors, SSE/realtime, or storage: `pbvex-client`.
- React provider/hooks/tests: `pbvex-react`; Svelte 5 runes/tests: `pbvex-svelte`.
- Component definitions, mounts, namespaces, or compatibility: `pbvex-components`.
- First-time local, staging, or production provisioning and guided configuration: `pbvex-deployment`.
- Ongoing releases, CI/CD, limits, security, backups, incidents, or test strategy: `pbvex-operations`.

For a product-sized example, use the messaging tutorial for authentication, authorization, relationships, realtime, and attachments; use the payments tutorial for outbound HTTP, webhook verification, idempotency, and production entitlements. Treat tutorial provider integrations as patterns, not implemented third-party SDKs.

## Work in the correct boundary

PBVex authoring is TypeScript under `pbvex/`; the CLI bundles it into a deployment artifact. The Go PBVex binary hosts PocketBase, validates/activates the artifact, and executes it in Goja. Deployed functions are not Node.js: do not use Node built-ins, `process`, `require`, dynamic imports, or arbitrary runtime npm dependencies.

Follow this path for an application change:

1. Define schema and functions under `pbvex/`, using public `pbvex/server` and `pbvex/values` APIs plus generated factories/references.
2. Run `pbvex codegen` after schema or exported-function changes.
3. Type-check and bundle with `pbvex typecheck` and `pbvex build` (or `build --check` when no artifact is needed).
4. Use generated `api`/`internal` references from `pbvex/_generated/` in server and client code.
5. Deploy with a superuser token only from trusted deployment automation; verify calls, realtime, scheduled work, and storage relevant to the release.

Design database reads as bounded access patterns. Prefer a matching index plus `first`, `unique`, `take`, or `paginate`; use `collect` only for provably small result sets. Pagination uses HMAC-authenticated keyset cursors: keep `continueCursor` opaque and restart from `null` when bounds, order, or page size change.

## Use the documentation by ownership

In a PBVex source checkout, start with `docs/quickstart.md` and `docs/guides/index.md`, then open only the owning guide. Use `docs/concepts/` for architecture, `docs/tutorials/` for complete application flows, `docs/self-hosting.md` and `docs/guides/going-to-production.md` for operators, and generated `docs/api-reference/` only for exact exported signatures. In an application repository, inspect its installed package version and generated types before assuming the source checkout matches it.

When repository behavior changes, update the owning authored guide and package README with the code. Never hand-edit generated API pages; run `pnpm docs:api`, then verify with `pnpm docs:verify`.

## Safety and navigation

Never manually edit `pbvex/_generated/` or `docs/api-reference/`; regenerate them with project scripts. Do not bypass validators, generated references, function visibility, manifest checks, or deployment activation. Public means reachable, not authorized—check identity and ownership.

Use repository source before assuming behavior:

```bash
rg --files packages backend docs | sort
rg -n "codegen|defineSchema|httpAction|scheduler" packages docs
pbvex --help
```

PBVex v1 is one binary process for one data directory. Preserve that single-binary, single-node deployment model; do not add a Node sidecar requirement or run multiple instances against one data directory.
