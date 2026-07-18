# Changelog

All notable PBVex changes are recorded here. PBVex uses Semantic Versioning.
The PocketBase project has its own release history; inherited PocketBase
entries are intentionally not duplicated in this changelog.

## Unreleased

### Added

- `@pbvex/server` now publishes the complete GoReleaser backend binary matrix
  for npm installations. The `pbvex` CLI depends on it and exposes
  `pbvex serve`.
- `pbvex dev` now starts a persistent project-local backend for loopback local
  targets, authorizes deployment with a scoped development token, performs the
  initial deployment, and watches for TypeScript application changes.
- `pbvex init` adds namespaced development, serving, deployment, and typecheck
  scripts by default, with an interactive default-yes prompt and
  `--no-scripts` opt-out.
- The server now gates the bundled PocketBase dashboard behind
  `serve --admin-ui`. Managed `pbvex dev` enables it by default and supports
  `--no-admin-ui`.

### Fixed

- Development watch deployments no longer report success after a deployment
  request fails.

## 0.3.4 - 2026-07-18

### Added

- `ctx.storage.getUrl(id, { mode: 'capability' })` now creates short-lived
  bearer URLs for native media elements, links, and other clients that cannot
  attach an authorization header. Identity binding remains the default.
- `ctx.storage.getUrl(id, { mode: 'public' })` creates stable, queryless bearer
  URLs with browser and CDN cache headers for intentionally public immutable
  assets.
- `v.image({ thumbs, mimeTypes })` adds schema-bound image uploads, trusted file
  metadata, and lazy PocketBase-compatible `?thumb=...` variants for local and
  S3 storage.

## 0.1.1 - 2026-07-15

### Fixed

- Global `pbvex` installations now include TypeScript, which the CLI uses at
  runtime to load configuration and bundle projects.

## 0.1.0

### Added

- Convex-shaped TypeScript authoring, schemas, validators, deterministic code
  generation, and CLI deployment workflow.
- Typed database queries and mutations with opaque IDs, indexes, pagination,
  and schema migration.
- Realtime query subscriptions with bounded reconnect and React/Svelte
  adapters.
- Durable scheduled functions, file storage, application authentication,
  nested actions, HTTP actions, and typed components.
- Publishable `pbvex`, `@pbvex/protocol`, `@pbvex/client`,
  `@pbvex/react`, and `@pbvex/svelte` packages.
- Single-binary GoReleaser archives with checksums, build attestations, and
  no-Node runtime validation.
- Tokenless npm trusted publishing with provenance and protected tag releases.

### Changed

- The Go module is `github.com/nathabonfim59/pbvex/backend`; PocketBase is used
  as a framework dependency rather than as the repository module.

### Compatibility

- Protocol v1 is single-node. Deployed functions run in Goja and cannot import
  Node built-ins or arbitrary npm packages.
