# How PBVex works

PBVex separates TypeScript authoring from server execution. The CLI turns the application into a deployment artifact; the standalone Go binary stores, validates, activates, and executes that artifact alongside PocketBase services.

## From source to an active deployment

1. The CLI reads the JSON-like `pbvex/pbvex.config.ts`, resolves a target, discovers PBVex modules, validates imports, and bundles the permitted TypeScript module graph into executable JavaScript.
2. It derives a versioned v1 manifest containing function descriptors, validators, optional schema, components, and deployment configuration. `pbvex codegen` uses the same discovery result to produce typed `api`, data-model, and server references in `pbvex/_generated/`.
3. `pbvex build` writes the manifest, base64 bundle, SHA-256, and byte size to `.pbvex/dist/artifact.json`. `pbvex deploy` sends that upload envelope to `POST /api/pbvex/deployments`, then calls the activation endpoint with `{ "atomic": true }`.
4. The backend verifies the envelope, manifest, decoded bundle size, and hash before storing a deployment. Activation verifies and compiles every function, authenticates component mount arguments, and materializes compatible schema/index changes inside a PocketBase transaction. Only then does it replace the active deployment pointer.

If validation, compilation, or schema work fails, the transaction does not swap the pointer; the previous active deployment remains active. Activation and rollback also close realtime subscriptions so clients reconnect and negotiate against the new immutable snapshot.

## Request flow

Public query, mutation, and action calls enter through `POST /api/pbvex/call`. The backend selects the active deployment, validates the public function and wire arguments, resolves optional PocketBase record-token identity, and dispatches the function to that deployment's runtime pool.

The runtime is Goja inside the PBVex binary, not Node.js. Function code has PBVex context capabilities and web-compatible globals, but it does not get Node built-ins, `require`, arbitrary npm packages, dynamic imports, or a Node process. Queries receive read-only database access; mutations receive transactional read/write access; actions make nested calls instead of accessing the database directly. Arguments and returns pass through the bounded PBVex wire codec.

PocketBase supplies the record database, authentication validation, HTTP serving/admin UI, and persistent data directory. PBVex adds owned backing collections for declared tables, schema/index lifecycle, opaque IDs, call routing, and the runtime bridge. A successful record mutation invalidates active PBVex subscriptions; the realtime service coalesces notifications and re-runs subscribed queries over SSE.

HTTP actions are separately mounted under `/api/pbvex`; reserved deployment, call, realtime, job, and storage routes take precedence over application catch-all routes.

## Durable services around the runtime

The same binary hosts these PBVex services:

- The scheduler persists jobs and pins the deployment snapshot each job invokes. It can resume eligible work after restart.
- Storage persists metadata and objects through PocketBase's configured filesystem, creates bounded upload/download capabilities, and runs cleanup work.
- Components derive deterministic mount namespaces. Their tables use separate physical collections, so two mounts of one component do not share data or IDs.

The generated references are a build-time contract over this manifest: `api` represents public functions, `internal` represents backend-only functions, and component references include their mount namespace. They are safer than recreating paths as strings, but they do not replace server-side validator and visibility checks.

## Single-binary, single-node boundary

At runtime there is no Node.js sidecar and no repository checkout requirement: the binary, PocketBase, and uploaded deployment data are sufficient. Persistent state defaults to `./pb_data` relative to the server working directory.

PBVex v1 is intentionally a single-process, single-node system. Run one application per server process and dedicated data directory. It does not provide multi-node consensus, distributed scheduler leases, or distributed realtime coordination; do not point multiple instances at the same data directory or treat them as a cluster. See [Deployment](../guides/deployment.md) for application rollout and [the self-hosting guide](../self-hosting.md) for running the binary.
