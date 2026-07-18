---
name: pbvex-functions
description: Author or review PBVex TypeScript schema and functions, including indexed queries and pagination, mutations, actions, authorization, relationships, outbound HTTP, HTTP actions, email templates, validators, generated references, nested calls, and scheduling. Use for application code under pbvex/ and generated-contract workflows.
---

# PBVex TypeScript functions

Expect a global CLI installed with `npm install --global pbvex` plus a matching local `pbvex` dependency. Use the direct `pbvex` command below; do not assume pnpm is installed in an application repository.

Author application code under `pbvex/`. Import schema/runtime APIs from public `pbvex/server` and validators from `pbvex/values`; import function factories and references from `./_generated/server` and `./_generated/api` after code generation. Do not import internal Go packages or manually construct function paths.

```bash
pbvex codegen
pbvex typecheck
pbvex build --check
rg -n "(query|mutation|action|httpAction)\(" pbvex
```

## Define bounded contracts

Use `defineSchema`, `defineTable`, indexes, and `v` validators. Declare `args` and `returns`; validators are both runtime boundaries and generated TypeScript types. Use opaque `v.id('table')` values as IDs—never parse, manufacture, or retag them. Keep public and internal functions distinct: `api` is client-callable; `internal` is backend-only. Public exposure never replaces authorization: resolve `ctx.auth.getUserIdentity()` and enforce ownership/tenant rules.

Choose the narrowest function kind:

- Queries have read-only `ctx.db` and can be subscribed to.
- Mutations have transactional read/write `ctx.db`, scheduler, and mutation storage capabilities.
- Actions have no direct database access; use generated references with `ctx.runQuery`, `ctx.runMutation`, or `ctx.runAction`.
- HTTP actions are public `/api/pbvex/...` routes, take `Request`, return `Response`, cannot be internal/nested-called, and use action capabilities.

Do not treat nested calls from an action as one transaction. Keep handlers within wire, timeout, nesting, and runtime limits; deployed code is Goja, not Node.js.

## Migrate root documents

For an incompatible `pbvex/schema.ts` change, run `pbvex migrations plan` and scaffold `pbvex migrations create <name> --table <table>`. Implement the resulting `defineMigration` in `pbvex/migrations/*.ts` with object `from`/`to` validators and required synchronous `up` and `down` handlers. It can transform one root PBVex table only; it cannot access components, other tables, PocketBase collections, or side-effect APIs. The input exposes read-only `_id` and `_creationTime`, but output must omit them. The pure context exposes only `migrationId`, stable `activationTime`, and `fail(message)`.

Migrations are bundled and run during atomic deployment activation; deployment rollback runs `down` in reverse order. Never edit/reuse an applied migration ID because history binds it to checksums and source/target schema hashes. Activation has fixed 10,000-document and 64-MiB hard limits with warnings at 80%; there is no force bypass or maintenance mode. `pbvex migrations plan` reports structural changes and chain matches only, with `--active-artifact <path>` for offline source selection. Use the separate `pbvex migrations pocketbase create <name>` command only for host-level PocketBase state such as auth collections, never a PBVex schema table.

## Query and authorize bounded data

Design indexes from access patterns: equality fields first, then the range/order field. Use `withIndex` to reduce candidates, reserve `filter` for residual predicates, and use `fullTableScan` only when the bounded scan is deliberate. Finish every growing query with `first`, `unique`, `take`, or `paginate`; reserve `collect` for provably small sets.

Pagination is keyset-based. Pass `{ numItems, cursor: cursor ?? null }`, return `page`, `isDone`, and `continueCursor`, and treat the cursor as opaque. Do not reuse it after changing query arguments, index bounds, ordering, or page size. Paginate before hydrating typed-ID relationships, and bound related reads to avoid N+1 client calls.

Authentication is not authorization. Resolve `ctx.auth.getUserIdentity()`, derive ownership from `tokenIdentifier`, and check stored ownership, membership, tenant, or role state at every sensitive public function. A valid `v.id(...)` proves table/namespace identity, not access. Put the same authorization inside subscribed queries and re-read durable permission state in delayed jobs.

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

Run `pbvex codegen` after adding or renaming a template. Generated `ActionCtx` and `HttpActionCtx` types expose the discovered template names as a literal union, so preserve autocomplete and never widen or cast an unknown template name to bypass it. `ctx.email` exists only on actions, internal actions, component actions, and HTTP actions; queries and mutations must not send irreversible external mail. Make scheduled/retried delivery idempotent.

PBVex sends through PocketBase's configured mailer and sender settings. Configure and test production SMTP separately; never put SMTP credentials in templates or function arguments. HTML substitutions are escaped, while subject and text substitutions are plain text; do not pre-render untrusted HTML into a variable. These application templates do not replace PocketBase's dashboard-managed verification, password-reset, OTP, or other authentication templates. Consult `docs/guides/email-templates.md` for format, limits, and deployment behavior.

## HTTP and scheduled work

Actions and HTTP actions may call external services with bounded `ctx.http.send`; queries and mutations may not. Allowlist destinations, keep credentials in component environment bindings, set explicit timeouts, validate response status and shape, and make external writes plus local follow-up mutations idempotent. Redirects are not followed automatically. Never accept an arbitrary client-provided URL.

For `httpAction`, use an allowed method and a relative exact `path` or trailing-slash `pathPrefix`; do not claim reserved first segments. Authenticate/authorize before side effects. Verify webhooks over the raw body before parsing, and let server CORS policy govern CORS headers.

Mutations, actions, and HTTP actions can use `ctx.scheduler.runAfter`/`runAt`; only public/internal mutations and actions can be targets. Retain/cancel the opaque job ID as needed, and make handlers idempotent because jobs may resume/retry. A scheduled job is not a live authenticated request.

Import `SECOND_MS`, `MINUTE_MS`, `HOUR_MS`, `DAY_MS`, or `WEEK_MS` from `pbvex/server` instead of embedding unexplained millisecond values. Multiply the smallest useful constant, such as `5 * MINUTE_MS` or `3 * DAY_MS`. A `runAfter` delay of `0` means eligible immediately/asynchronously.

For fixed recurring work, define one default `cronJobs()` export in `pbvex/crons.ts` and call `crons.cron(name, expression, generatedRef, args?)`. Expressions use PocketBase's five-field UTC format or supported macros. Cron ticks enqueue durable PBVex jobs; missed ticks during downtime are not backfilled, and overlapping occurrences are possible.

## Generated contract rule

Never edit `pbvex/_generated/` by hand. Run `pbvex codegen` after changing schema, function exports, components, mounts, or email-template names, then fix types at call sites. Do not bypass generated references, template-name unions, or validators to silence a type error.

For the complete supported value set and runtime-validation boundaries, consult `docs/guides/data-types-and-validation.md` rather than assuming Convex, PocketBase, JSON, or JavaScript values are interchangeable.
