# Changelog

All notable PBVex changes are recorded here. PBVex uses Semantic Versioning.
The PocketBase project has its own release history; inherited PocketBase
entries are intentionally not duplicated in this changelog.

## Unreleased

### Added

- Convex-shaped TypeScript authoring, schemas, validators, deterministic code
  generation, and CLI deployment workflow.
- Typed database queries and mutations with opaque IDs, indexes, pagination,
  and schema migration.
- Realtime query subscriptions with bounded reconnect and React/Svelte
  adapters.
- Durable scheduled functions, file storage, application authentication,
  nested actions, HTTP actions, and typed components.
- Publishable `pbvex`, `@pbvex/protocol`, `@pbvex/sdk-core`,
  `@pbvex/sdk-react`, and `@pbvex/sdk-svelte` packages.
- Single-binary GoReleaser archives with checksums, build attestations, and
  no-Node runtime validation.
- Tokenless npm trusted publishing with provenance and protected tag releases.

### Changed

- The Go module is `github.com/nathabonfim59/pbvex/backend`; PocketBase is used
  as a framework dependency rather than as the repository module.

### Compatibility

- Protocol v1 is single-node. Deployed functions run in Goja and cannot import
  Node built-ins or arbitrary npm packages.
