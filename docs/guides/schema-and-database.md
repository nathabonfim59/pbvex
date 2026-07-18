# Schema and database

Define the root schema in `pbvex/schema.ts`. `defineSchema` names tables and `defineTable` defines user fields; documents always also have `_id` and `_creationTime`.

```ts
// pbvex/schema.ts
import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  messages: defineTable({
    channel: v.string(),
    body: v.string(),
    author: v.id('users'),
    state: v.defaulted(v.union(v.literal('new'), v.literal('sent')), 'new'),
  }).index('by_channel', ['channel']),
  users: defineTable({ name: v.string(), profile: v.optional(v.object({ city: v.string() })) }),
});
```

Validators use the same `v` API as function arguments. Table and index names must be identifiers. Index fields must exist and may be dotted fields through an object validator; arrays, IDs, and primitive fields are leaves. An index has a non-empty field list and a unique name/field list.

Run `pbvex codegen` after schema changes. The generated `Doc`, `Id`, and context types make table names, fields, index names, and ID tables type-safe.

## CRUD and query entry points

Queries have read-only database access; mutations add writes. Actions and HTTP actions have no `ctx.db` and must call a query or mutation instead.

```ts
// pbvex/messages.ts
import { query, mutation } from './_generated/server';
import { v } from 'pbvex/values';

export const recent = query({
  args: { channel: v.string(), cursor: v.optional(v.string()) },
  handler: async (ctx, args) => {
    return ctx.db.query('messages')
      .withIndex('by_channel', (q) => q.eq('channel', args.channel))
      .order('desc')
      .paginate({ cursor: args.cursor ?? null, numItems: 20 });
  },
});

export const edit = mutation({
  args: { id: v.id('messages'), body: v.string() },
  handler: async (ctx, args) => {
    await ctx.db.patch(args.id, { body: args.body });
    return ctx.db.get(args.id);
  },
});
```

`get(id)` returns a document or `null`; `normalizeId(table, string)` returns a typed ID or `null`. `insert` returns an ID. `patch` updates supplied user fields, `replace` requires all insert fields, and `delete` removes a document. System fields are never insertable, replaceable, or patchable.

Start with `ctx.db.query(table)`. `withIndex(name, range)` binds an existing index, while `fullTableScan()` makes an intentional scan explicit. Terminal operations include `first`, `unique`, `take`, `collect`, and `paginate`.

For compound index rules, filters, ordering, cursor pagination, performance guidance, and common mistakes, read [Querying data and designing indexes](./querying-and-indexes.md). For typed record references, one-to-many and many-to-many models, bounded hydration, and denormalization, read [Relationships and joins](./relationships-and-joins.md).

## Transactions, IDs, and isolation

Each top-level mutation is a PocketBase transaction. Writes roll back when the handler errors, times out, is canceled, or returns an invalid value. PBVex does not expose a manual transaction API or promise cross-action atomicity. Keep external side effects out of a mutation or make them idempotent.

IDs are opaque authenticated capabilities, bound to a logical table and namespace. Persist/pass them as `Id<'messages'>`; do not use PocketBase raw IDs or forge strings. `v.id` validates that binding.

Components can declare their own schemas. Their tables are materialized in distinct physical collections and mount namespaces, so the same component mounted twice has isolated data and IDs. Root functions cannot use a component’s table namespace.

## Deployment changes and limitations

Deploy/activation validates and applies schema/index changes transactionally; failed activation leaves the previous deployment active. For incompatible root-table document changes, define typed, reversible transformations in `pbvex/migrations/*.ts`; activation runs their `up` handlers and deployment rollback runs `down`. These first-class migrations accept object validators for one root table and do not target component tables or direct PocketBase collections. See [Migrations](./migrations.md).

Component schema materialization likewise does not silently adopt unrelated physical collections. Removing a root table or component mount does not make its data a new table elsewhere; component-owned data remains dormant for rollback or remount at the same path. Schema descriptors are optional, but without one there is no declared table authority for typed database access. PBVex v1 is single-node and does not provide vector search, arbitrary SQL, native join operators, cross-table migration access, or migration side effects. Compose relationships explicitly as described in [Relationships and joins](./relationships-and-joins.md). Plan destructive data changes explicitly and back up before deployment changes.
