---
name: pbvex-backend
description: Build or review PBVex application backends, including deployed function and return contracts, strict object validators and DTOs, application errors, schema, indexed queries and pagination, mutations, actions, authorization, relationships, outbound HTTP, HTTP actions, email templates, generated references, nested calls, and scheduling. Use for application code under pbvex/ and generated-contract workflows.
---

# PBVex application backend

Use the project's local `pbvex` through package scripts or `npx pbvex`; a global CLI is optional and must match the local dependency. Do not assume pnpm is installed in an application repository.

Author application code under `pbvex/`. Import schema/runtime APIs from public `pbvex/server` and validators from `pbvex/values`; import factories/references from `pbvex/_generated/*` using the correct relative path for the current module. Do not import internal Go packages or manually construct function paths.

```bash
npx pbvex codegen
npx pbvex typecheck
npx pbvex build --check
rg -n "(query|mutation|action|httpAction)\(" pbvex
```

## Define bounded contracts

Ordinary TypeScript helpers are local implementation details: write normal parameter and return types, and do not add PBVex validators unless the helper itself needs runtime validation. In contrast, exports created with generated `query`, `internalQuery`, `mutation`, `internalMutation`, `action`, or `internalAction` factories are deployed wire boundaries. Every stable deployed value function must declare `returns` as well as intentional `args`; validators enforce runtime values and determine generated reference types. HTTP actions instead use their native `Request`/`Response` contract.

Omitting `returns` is exactly an implicit `v.any()`: PBVex still applies bounded wire-value encoding and checks, but application shape validation is lost and generated references expose `any`. Explicit `v.any()` is appropriate only for an intentionally dynamic contract or short-lived prototyping, not as a way to silence a contract error.

Use `defineSchema`, `defineTable`, indexes, and `v` validators. A deployed `v.object(...)` is a closed object: required fields must exist and unknown fields are rejected. Local `validator.validate(...)` is equally strict. Validation does not project or strip a larger object into the declared shape, so map a document to an explicit DTO before returning it, or use a complete document validator:

```ts
import { paginationResultValidator } from 'pbvex/server';
import { v } from 'pbvex/values';
import schema from './schema';

const taskDocument = schema.tables.tasks.documentValidator;
const taskSummary = taskDocument.pick('_id', 'title');
const editableSummary = taskSummary.omit('_id').extend({ title: v.string() }).partial();
const taskPage = paginationResultValidator(taskSummary);
```

`documentValidator` includes every schema field plus typed `_id` and `_creationTime`. Its `pick`, `omit`, `extend`, and `partial` methods preserve precise types; `paginationResultValidator(itemValidator)` declares the closed `{ page, isDone, continueCursor }` result. Prefer an explicit DTO validator and matching object mapping when the public shape intentionally differs from a stored document.

`v.optional` means absence, not `null`; `v.defaulted` materializes omitted insert/replace values but not omitted patch fields. Even `v.any()` accepts only PBVex wire values. Args, returns, documents, and validator values reject `v.delayed`, non-finite numbers, `Date`, `Map`, typed arrays, class instances, cycles, and other non-wire values; API-specific inputs such as scheduler dates or HTTP byte bodies are separate contracts. Use opaque `v.id('table')` values as IDs—never parse, manufacture, or retag them. `_id` and `_creationTime` are immutable; use `normalizeId` at untyped string boundaries.

Keep public and internal functions distinct: `api` is client-callable; `internal` is backend-only. Public exposure never replaces authorization: resolve `ctx.auth.getUserIdentity()` and enforce ownership/tenant rules.

Choose the narrowest function kind:

- Queries have read-only `ctx.db` and can be subscribed to.
- Mutations have transactional read/write `ctx.db`, scheduler, and mutation storage capabilities.
- Actions have no direct database access; use generated references with `ctx.runQuery`, `ctx.runMutation`, or `ctx.runAction`.
- HTTP actions are public `/api/pbvex/...` routes, take `Request`, return `Response`, cannot be internal/nested-called, and use action capabilities.

Do not treat nested calls from an action as one transaction. Keep handlers within wire, timeout, nesting, and runtime limits; deployed code is Goja, not Node.js.

## Migrate root documents

For an incompatible `pbvex/schema.ts` change, run `pbvex migrations plan` and scaffold a concrete migration such as `pbvex migrations create add_message_status --table messages`. Implement the resulting `defineMigration` in `pbvex/migrations/*.ts` with object `from`/`to` validators and required synchronous `up` and `down` handlers. It can transform one root PBVex table only; it cannot access components, other tables, PocketBase collections, or side-effect APIs. The input exposes read-only `_id` and `_creationTime`, but output must omit them. The pure context exposes only `migrationId`, stable `activationTime`, and `fail(message)`.

Migrations are bundled and run during atomic deployment activation; deployment rollback runs `down` in reverse order. Never edit/reuse an applied migration ID because history binds it to checksums and source/target schema hashes. Activation has fixed 10,000-document and 64-MiB hard limits with warnings at 80%; there is no force bypass or maintenance mode. `pbvex migrations plan` reports structural changes and chain matches only, with `--active-artifact path/to/artifact.json` for offline source selection. Use a concrete command such as `pbvex migrations pocketbase create add_users_auth` only for host-level PocketBase state such as auth collections, never a PBVex schema table.

## Query and authorize bounded data

Design indexes from access patterns: equality fields first, then the range/order field. Use `withIndex` to reduce candidates, reserve `filter` for residual predicates, and use `fullTableScan` only when the bounded scan is deliberate. Finish every growing query with `first`, `unique`, `take`, or `paginate`; reserve `collect` for provably small sets.

Pagination is keyset-based. Pass `{ numItems, cursor: cursor ?? null }`, declare `returns: paginationResultValidator(itemValidator)`, and return `page`, `isDone`, and `continueCursor`. `isDone: true` means the result is complete and there is no next page; `continueCursor` is then the empty string. Otherwise pass the opaque non-empty cursor to the next request. Do not decode it or reuse it after changing query arguments, index bounds, ordering, or page size. Paginate before hydrating typed-ID relationships, and bound related reads to avoid N+1 client calls.

An ID validates table/namespace identity, not referenced-record existence; PBVex has no automatic expansion, cascades, or foreign-key enforcement. Model many-to-many data with a junction table and an index for each traversal. Indexes are not uniqueness constraints, and `unique()` detects duplicate results rather than enforcing writes. Choose and implement delete policy explicitly: restrict, transactional cascade, detach, or preserve history.

Authentication is not authorization. Resolve `ctx.auth.getUserIdentity()`, derive ownership from `tokenIdentifier`, and check stored ownership, membership, tenant, or role state at every sensitive public function. A valid `v.id(...)` proves table/namespace identity, not access. Put the same authorization inside subscribed queries and re-read durable permission state in delayed jobs.

## Return application failures safely

Import `ApplicationError` from `pbvex/server` for expected, client-actionable failures. Its recognized categories and deterministic HTTP statuses are `bad_request` (400), `unauthorized` (401), `forbidden` (403), `not_found` (404), and `conflict` (409). Use `unauthorized` only when authentication is required or invalid; use `forbidden` when an authenticated identity is not allowed.

```ts
throw new ApplicationError('conflict', {
  resource: 'task',
  reason: 'version_mismatch',
});
```

The optional `data` is returned to clients, so include only bounded PBVex wire values that are deliberately safe to disclose; never put secrets, stack traces, raw provider responses, or internal diagnostics there. Throwing an `ApplicationError` from a mutation rolls back the transaction like any other handler failure. Calls and HTTP actions receive the category's structured error and status. Realtime has already established an HTTP 200 stream, so a failed subscription result carries the structured error as a stream event rather than changing HTTP status.

Use ordinary `Error` for unexpected faults. PBVex masks those from clients as a generic internal error and logs the original server-side with available correlation context such as request, subscription, or job ID. Do not convert unexpected exceptions into `ApplicationError` merely to expose their messages. See `docs/guides/client/errors.md` for client handling and `docs/guides/authorization.md` for policy structure.

## Author storage contracts

Use `v.image({ thumbs, mimeTypes })` for top-level image-owning fields and call `generateUploadUrl({ table, field })` so the upload snapshots that schema policy. Generic files use an application-chosen field representation. Authorize before generating uploads, calling `getMetadata`/`getUrl`, or deleting; a valid `StorageId` proves neither ownership nor publication permission. Identity URLs are the default. Use the `pbvex-storage` skill for capability/public modes, thumbnail URL construction, metadata, and migration behavior.

Root functions cannot read `process.env` and do not receive `ctx.env`. When backend code needs server-provisioned configuration, isolate it in a component with an explicit `envVar` declaration. Do not substitute target metadata, deployment token variables, mount arguments, or committed literals for a secret store. Consult `docs/guides/environment-variables.md` for the supported boundary.

## Application email templates

Put application-owned templates in flat `pbvex/emails/<name>.json` files with a non-empty `subject` and at least one non-empty `text` or `html` body. Use bounded `{{ variable }}` placeholders and call the generated action context:

```ts
await ctx.email.send({
  template: 'welcome',
  to: args.email,
  variables: { name: args.name },
});
```

Run `pbvex codegen` after adding or renaming a template. Generated `ActionCtx` and `HttpActionCtx` types expose the discovered template names as a literal union, so preserve autocomplete and never widen or cast an unknown template name to bypass it. `ctx.email` exists only on actions, internal actions, component actions, and HTTP actions; queries and mutations must not send irreversible external mail. Every referenced variable is required, sends allow at most 50 total recipients, and escaped HTML variables still require safe URL-scheme handling. Make scheduled/retried delivery idempotent.

PBVex sends through PocketBase's configured mailer and sender settings. Configure and test production SMTP separately; never put SMTP credentials in templates or function arguments. HTML substitutions are escaped, while subject and text substitutions are plain text; do not pre-render untrusted HTML into a variable. These application templates do not replace PocketBase's dashboard-managed verification, password-reset, OTP, or other authentication templates. Consult `docs/guides/email-templates.md` for format, limits, and deployment behavior.

## HTTP and scheduled work

Actions and HTTP actions may call external services with bounded `ctx.http.send`; queries and mutations may not. Allowlist destinations, keep credentials in component environment bindings, set explicit timeouts, validate 3xx/status/shape, and stay within the buffered response limit. For provider writes, persist local intent, call with a stable idempotency key, then attach/reconcile provider state. Never accept an arbitrary client-provided URL.

For `httpAction`, use an allowed method and a relative exact `path` or trailing-slash `pathPrefix`; do not claim reserved first segments. Authenticate/authorize before side effects. Request bodies are single-consumption. Verify webhooks over raw bytes at the credential boundary, map them to a narrow typed event, then apply one idempotent mutation. Let server CORS policy govern CORS headers.

Mutations, actions, and HTTP actions can use `ctx.scheduler.runAfter`/`runAt`; only public/internal mutations and actions can be targets. Mutation scheduling commits/rolls back atomically, while action scheduling is a separate durable write. Jobs pin the creating deployment snapshot and do not preserve request auth. Delivery is at least once with bounded automatic retries for eligible infrastructure failures/timeouts; application exceptions are terminal. Cancellation is owner/namespace-scoped and cannot undo a running external effect, so handlers must be idempotent.

Import `SECOND_MS`, `MINUTE_MS`, `HOUR_MS`, `DAY_MS`, or `WEEK_MS` from `pbvex/server` instead of embedding unexplained millisecond values. Multiply the smallest useful constant, such as `5 * MINUTE_MS` or `3 * DAY_MS`. A `runAfter` delay of `0` means eligible immediately/asynchronously.

For fixed recurring work, define one default `cronJobs()` export in `pbvex/crons.ts` and call `crons.cron(name, expression, generatedRef, args?)`. Expressions use PocketBase's five-field UTC format or supported macros. Cron ticks enqueue pinned durable jobs; missed ticks are not backfilled and overlapping occurrences are possible.

## Generated contract rule

Never edit `pbvex/_generated/` by hand. Run `pbvex codegen` after changing schema, function exports, components, mounts, or email-template names, then fix types at call sites. Do not bypass generated references, template-name unions, or validators to silence a type error.

Module path plus export name determines generated API paths, so moving files is a breaking caller change. `schema.ts` and `app.ts` are conventions discovered from exports; `crons.ts`, `emails/`, migration, config, and `_generated` locations have platform meaning. `.test.ts` and `node_modules` are excluded from function discovery. Test pure helpers directly, type/build generated contracts, mock SDK boundaries, and use a disposable deployed backend for integration tests—there is no public in-memory `ctx` runner.

For the complete supported value set and runtime-validation boundaries, consult `docs/guides/data-types-and-validation.md` rather than assuming Convex, PocketBase, JSON, or JavaScript values are interchangeable.
