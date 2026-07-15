---
name: pbvex
description: Route PBVex application work across its TypeScript authoring, Go/PocketBase backend, typed clients, UI integrations, components, and operations. Use when starting, planning, debugging, reviewing, or making an end-to-end PBVex change and when selecting a specialized PBVex skill.
---

# PBVex workflow router

In application repositories, install the CLI globally with `npm install --global pbvex` and keep a matching local `pbvex` dependency so authoring imports resolve. Invoke the global CLI directly as `pbvex`.

Use this skill to orient a PBVex change, then load only the specialized skill(s) it needs:

- Backend binary, PocketBase embedding, runtime route, migrations, or Go tests: `pbvex-backend`.
- `pbvex/` schema/functions, validators, generated references, HTTP actions, application email templates, or scheduling: `pbvex-functions`.
- Vanilla typed calls, auth, SSE/realtime, or storage: `pbvex-client`.
- React provider/hooks/tests: `pbvex-react`; Svelte 5 runes/tests: `pbvex-svelte`.
- Component definitions, mounts, namespaces, or compatibility: `pbvex-components`.
- Deployments, release, limits, security, backups, or test strategy: `pbvex-operations`.

## Work in the correct boundary

PBVex authoring is TypeScript under `pbvex/`; the CLI bundles it into a deployment artifact. The Go PBVex binary hosts PocketBase, validates/activates the artifact, and executes it in Goja. Deployed functions are not Node.js: do not use Node built-ins, `process`, `require`, dynamic imports, or arbitrary runtime npm dependencies.

Follow this path for an application change:

1. Define schema and functions under `pbvex/`, using public `pbvex/server` and `pbvex/values` APIs plus generated factories/references.
2. Run `pbvex codegen` after schema or exported-function changes.
3. Type-check and bundle with `pbvex typecheck` and `pbvex build` (or `build --check` when no artifact is needed).
4. Use generated `api`/`internal` references from `pbvex/_generated/` in server and client code.
5. Deploy with a superuser token only from trusted deployment automation; verify calls, realtime, scheduled work, and storage relevant to the release.

## Safety and navigation

Never manually edit `pbvex/_generated/` or `docs/api-reference/`; regenerate them with project scripts. Do not bypass validators, generated references, function visibility, manifest checks, or deployment activation. Public means reachable, not authorized—check identity and ownership.

Use repository source before assuming behavior:

```bash
rg --files packages backend docs | sort
rg -n "codegen|defineSchema|httpAction|scheduler" packages docs
pbvex --help
```

PBVex v1 is one binary process for one data directory. Preserve that single-binary, single-node deployment model; do not add a Node sidecar requirement or run multiple instances against one data directory.
