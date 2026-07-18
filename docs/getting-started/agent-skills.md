# PBVex Agent Skills

PBVex provides installable instructions for coding agents. They help an agent navigate PBVex conventions; they do not install the PBVex server, CLI, or client/runtime packages.

List the suite available from this repository:

```bash
npx skills add nathabonfim59/pbvex --list
```

## Install

Install the umbrella skill for end-to-end routing and workflow guidance:

```bash
npx skills add nathabonfim59/pbvex --skill pbvex
```

Install focused skills when the task stays in one layer, for example:

```bash
npx skills add nathabonfim59/pbvex --skill pbvex-backend
npx skills add nathabonfim59/pbvex --skill pbvex-storage
npx skills add nathabonfim59/pbvex --skill pbvex-auth
npx skills add nathabonfim59/pbvex --skill pbvex-realtime
npx skills add nathabonfim59/pbvex --skill pbvex-client
npx skills add nathabonfim59/pbvex --skill pbvex-react
npx skills add nathabonfim59/pbvex --skill pbvex-deployment
```

Install the complete suite when one agent will work across project layers:

```bash
npx skills add nathabonfim59/pbvex --skill '*'
```

The default install scope is the current project. Install globally instead with `--global`:

```bash
npx skills add nathabonfim59/pbvex --skill pbvex-operations --global
```

Target one or more supported coding agents with `--agent`:

```bash
npx skills add nathabonfim59/pbvex --skill pbvex-svelte --agent codex
npx skills add nathabonfim59/pbvex --skill pbvex-client --agent codex cursor
```

Use `--agent '*'` to target every supported agent. Add `--yes` where non-interactive CI or scripting requires it. Run `npx skills add --help` to see the agent identifiers supported by your installed CLI.

## Choose skills

- `pbvex`: start here for a full-stack change or to route a task.
- `pbvex-internals`: PBVex's own Go/PocketBase implementation, runtime routes, migrations, and realtime internals.
- `pbvex-protocol`: cross-language manifests, wire values, validators, fixtures, canonical artifacts, IDs, and compatibility.
- `pbvex-backend`: application `pbvex/` schema/functions, indexed queries and pagination, authorization, outbound HTTP, HTTP actions, and scheduling.
- `pbvex-storage`: generic/image uploads, metadata, download access modes, CDN caching, resizing, object storage, and deletion.
- `pbvex-auth`: PocketBase auth collections, native signup/sign-in methods, auth stores, OAuth2/MFA, and token lifecycle.
- `pbvex-realtime`: SSE query subscriptions, retries, deduplication, auth/deployment reconnects, proxy streaming, and cleanup.
- `pbvex-client`: framework-neutral typed calls, configuration, errors/cancellation, and storage transfers.
- `pbvex-react` or `pbvex-svelte`: framework-layer patterns and tests.
- `pbvex-components`: component definitions, mounts, namespaces, and compatibility.
- `pbvex-deployment`: interactive first-time provisioning, configuration, deployment, smoke testing, and handoff.
- `pbvex-operations`: deployment, release, limits, security, backup, testing, and documentation verification.

Suggested combinations: use `pbvex` + `pbvex-backend` + `pbvex-client` for a new application feature; add the focused auth, realtime, or storage skill when involved; add one UI skill for React or Svelte; add `pbvex-components` for mounted modules; add `pbvex-deployment` for the first environment setup; add `pbvex-operations` for ongoing release work. Contributors changing PBVex internals pair `pbvex-protocol` with `pbvex-internals` when the wire contract is involved and normally add `pbvex-operations` for final gates.

## Update and inspect

Update project-installed skills:

```bash
npx skills update
```

Update a named skill or global skills:

```bash
npx skills update pbvex-backend
npx skills update --global
```

Inspect installed project or global skills:

```bash
npx skills list
npx skills list --global
```

After installing instructions for a project, install its actual PBVex dependencies separately—for example `pbvex` for TypeScript authoring, `@pbvex/client` for client calls, and `@pbvex/react` or `@pbvex/svelte` where applicable. An independently managed deployment target requires an already running PBVex server; managed local `pbvex dev` starts the bundled backend itself.
