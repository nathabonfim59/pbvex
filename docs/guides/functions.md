# Backend functions

PBVex functions are exported from files under `pbvex/`. Run `pbvex codegen` after changing them, then import the generated factories and references. A public function is callable by a client; an internal function is callable only by backend code.

```ts
// pbvex/messages.ts
import { query, mutation, action, internalQuery } from './_generated/server';
import { api, internal } from './_generated/api';
import { v } from 'pbvex/values';

export const list = query({
  args: { channel: v.string() },
  returns: v.array(v.string()),
  handler: async (ctx, { channel }) => {
    const rows = await ctx.db
      .query('messages')
      .withIndex('by_channel', (q) => q.eq('channel', channel))
      .collect();
    return rows.map((row) => row.body);
  },
});

export const send = mutation({
  args: { channel: v.string(), body: v.string() },
  returns: v.id('messages'),
  handler: (ctx, args) => ctx.db.insert('messages', args),
});

export const count = internalQuery({
  returns: v.number(),
  handler: async (ctx) => (await ctx.db.query('messages').collect()).length,
});

export const notify = action({
  args: { channel: v.string(), body: v.string() },
  returns: v.id('messages'),
  handler: async (ctx, args) => {
    // Do network or other non-database work here, then use a mutation for writes.
    return ctx.runMutation(api.messages.send, args);
  },
});
```

## Kinds, visibility, and contexts

- `query` / `internalQuery` receive `QueryCtx`: read-only `ctx.db`, `ctx.auth`, and `ctx.storage.getUrl`.
- `mutation` / `internalMutation` receive `MutationCtx`: read/write `ctx.db`, `ctx.scheduler`, and storage upload/delete capabilities. A top-level mutation runs in a database transaction.
- `action` / `internalAction` receive `ActionCtx`: no direct database access; use `ctx.runQuery`, `ctx.runMutation`, `ctx.runAction`, or `ctx.run`. Actions also have auth, scheduler, and storage capabilities.
- `httpAction` is a separate public HTTP entry point; see [HTTP actions](./http-actions.md).

Public is an exposure rule, not an authorization rule. Check `await ctx.auth.getUserIdentity()` in every function that requires a user. Internal references are not accepted by the public call API.

## Validators and returns

`args` and `returns` accept either one validator (usually `v.object(...)`) or an object of field validators. Validators are enforced at the invocation boundary and define generated TypeScript types. Common constructors are `v.string()`, `v.number()`, `v.boolean()`, `v.int64()`/`v.bigint()`, `v.bytes()`, `v.id('table')`, `v.array`, `v.object`, `v.record`, `v.union`, `v.optional`, and `v.defaulted`. `v.number()` and `v.float64()` require finite numbers; `v.int64()` uses `bigint`; `v.bytes()` uses `ArrayBuffer`.

Use `v.id('messages')` for an ID argument rather than `string`. IDs are opaque, table- and namespace-bound values; do not construct, parse, or rewrite them.

## Calling functions

Generated `api` and `internal` references carry the function kind, visibility, arguments, and return type. They are the supported way to make typed backend calls.

```ts
// From an action.
const id = await ctx.runMutation(api.messages.send, { channel: 'general', body: 'Hi' });
const total = await ctx.runQuery(internal.messages.count, {});
```

For a function with no arguments, generated calls permit omitting the argument object; passing `{}` is also valid. Only actions and HTTP actions can make nested calls. Nested calls preserve the authenticated identity and request metadata, execute in a fresh runtime entry, and are bounded to depth 8 and 64 total nested calls. A nested mutation started outside a mutation gets its own transaction; do not treat an action plus one or more nested mutations as one atomic transaction.

Clients call only public query, mutation, and action references through `PBVexClient`; see [queries, mutations, and actions](./client/queries-mutations-actions.md). A query result may be subscribed to by the realtime client, and mutations invalidate matching subscriptions.

## Runtime limits

Functions execute an ahead-of-time bundle in the server’s JavaScript runtime, not Node.js. There is no `require`, `import`, Node built-in, process global, or arbitrary npm runtime at invocation time. Use the exposed web-compatible `Request`, `Response`, `URL`, headers, encoders, and the context capabilities only. Arguments and returns use PBVex’s bounded wire-value codec; `undefined` fields are omitted and an `undefined` return becomes `null`.

The active deployment’s request timeout, argument-size, and return-size limits apply (defaults are 30 seconds and 1 MiB for arguments and return values). Do not rely on retries for mutations: PBVex guarantees rollback on a mutation error, timeout, cancellation, or invalid return, but does not provide an application-level automatic mutation retry contract.
