# Querying data and designing indexes

PBVex queries start from a table, optionally select an index and range, optionally apply a filter and order, and finish with a bounded terminal operation. Good query design begins with the access pattern: decide which records a function must read, in what order, and how many it can return before defining the index.

For relationships between tables, continue with [Relationships and joins](./relationships-and-joins.md). For inserts, updates, transactions, and schema deployment behavior, see [Schema and database](./schema-and-database.md).

## The query pipeline

A database query has four parts:

```ts
const page = await ctx.db
  .query('tasks')                                      // table
  .withIndex('by_owner_status_due', (q) =>             // access path and bounds
    q.eq('ownerId', args.ownerId)
      .eq('status', 'open')
      .gte('dueAt', args.from)
      .lt('dueAt', args.until),
  )
  .order('asc')                                        // index order
  .paginate({ cursor: args.cursor ?? null, numItems: 50 }); // terminal
```

1. `query(table)` selects a declared table and preserves its generated document type.
2. `withIndex(name, range)` narrows and orders records using a declared index. Use `fullTableScan()` when scanning is intentional and no index applies.
3. `filter(...)` applies additional predicates. It does not choose an index for you.
4. A terminal such as `first`, `take`, or `paginate` executes the query.

Queries are lazy until a terminal operation runs. Build the complete query before awaiting its result.

## Define indexes from access patterns

Declare indexes on the table in `pbvex/schema.ts`:

```ts
// pbvex/schema.ts
import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  tasks: defineTable({
    ownerId: v.id('users'),
    status: v.union(v.literal('open'), v.literal('done')),
    dueAt: v.number(),
    priority: v.number(),
    title: v.string(),
    metadata: v.object({ category: v.string() }),
  })
    .index('by_owner_status_due', ['ownerId', 'status', 'dueAt'])
    .index('by_owner_due', ['ownerId', 'dueAt'])
    .index('by_category', ['metadata.category']),
  users: defineTable({ name: v.string() }),
});
```

Run `pbvex codegen` after changing an index. Generated types autocomplete valid table, index, and range-field names.

Index order matters. For `['ownerId', 'status', 'dueAt']`, a range callback may use:

- Equality on the leading prefix: `ownerId`, then `status`.
- A lower and/or upper range on the next field: `dueAt`.
- No constraints after the range field.

Valid examples include:

```ts
// All tasks for one owner, ordered by status and then dueAt.
.withIndex('by_owner_status_due', (q) =>
  q.eq('ownerId', ownerId)
)

// Open tasks for one owner in a due-time window.
.withIndex('by_owner_status_due', (q) =>
  q.eq('ownerId', ownerId)
    .eq('status', 'open')
    .gte('dueAt', from)
    .lt('dueAt', until)
)
```

You cannot skip `status` and constrain `dueAt` through that index. The separate `by_owner_due` index exists for “all statuses for this owner in a time window.” One compound index is not automatically suitable for every subset or field order.

Indexes can use dotted paths through declared objects, such as `metadata.category`. Arrays are leaves: PBVex does not provide an “array contains” index or full-text search in v1.

## Index ranges

The index range builder supports `eq`, `gt`, `gte`, `lt`, and `lte`.

```ts
const overdue = await ctx.db
  .query('tasks')
  .withIndex('by_owner_status_due', (q) =>
    q.eq('ownerId', ownerId)
      .eq('status', 'open')
      .lt('dueAt', Date.now()),
  )
  .take(100);
```

Use `eq` for every known leading field. Put the field you sort or range over after those equality fields. A useful rule is:

```text
[fields always tested for equality, field used for range/order]
```

For example, the access pattern “open tasks for a tenant ordered by due date” suggests `['tenantId', 'status', 'dueAt']`.

## Filters

Use `filter` for predicates that are not part of the selected index range:

```ts
const urgent = await ctx.db
  .query('tasks')
  .withIndex('by_owner_status_due', (q) =>
    q.eq('ownerId', ownerId).eq('status', 'open'),
  )
  .filter((q) => q.gte(q.field('priority'), 8))
  .take(50);
```

The filter builder supports field and literal comparisons (`eq`, `neq`, `lt`, `lte`, `gt`, `gte`), boolean composition (`and`, `or`, `not`), and numeric expressions (`add`, `sub`, `mul`, `div`, `mod`, `neg`). Dotted object fields are type-safe.

An index range reduces the candidate set before the remaining filter is applied. If a filter is common, selective, or used on a growing table, design an index for it instead of relying on a full scan. Do not create an index for every possible field combination: each index consumes storage and adds write/deployment maintenance. Add indexes for real, bounded access patterns.

## Ordering

Without a selected index, records are ordered by `_creationTime`, with `_id` as a deterministic tie-breaker. With an index, records are ordered by the index fields, then `_creationTime`, then `_id`.

```ts
const newest = await ctx.db
  .query('tasks')
  .withIndex('by_owner_due', (q) => q.eq('ownerId', ownerId))
  .order('desc')
  .take(20);
```

`order('asc')` is the default. `order('desc')` reverses the complete ordering; it does not sort by an arbitrary field. To order by a different field, define an index whose field order represents that access pattern.

## Choose a terminal operation

| Terminal | Result | Use it when |
| --- | --- | --- |
| `first()` | First document or `null` | Only the first ordered match matters |
| `unique()` | One document or `null`; errors on multiple | The application expects at most one matching record |
| `take(n)` | Up to `n` documents | The result has a small explicit bound |
| `collect()` | Every match, within platform limits | The complete result is provably small |
| `paginate(options)` | Page, completion flag, and cursor | Results can grow or are displayed incrementally |

`take`, `collect`, and a page are limited to 1,024 documents and also remain subject to the deployment return-value byte limit. Prefer `first` over `collect()[0]`, `unique` when duplicates indicate a data error, and pagination for user- or data-dependent result sizes.

## Pagination

Accept a cursor in the query arguments and return PBVex’s pagination result directly:

```ts
import { paginationResultValidator } from 'pbvex/server';
import schema from './schema';

export const listOpen = query({
  args: {
    ownerId: v.id('users'),
    cursor: v.optional(v.string()),
  },
  returns: paginationResultValidator(schema.tables.tasks.documentValidator),
  handler: (ctx, { ownerId, cursor }) =>
    ctx.db
      .query('tasks')
      .withIndex('by_owner_status_due', (q) =>
        q.eq('ownerId', ownerId).eq('status', 'open'),
      )
      .paginate({ cursor: cursor ?? null, numItems: 50 }),
});
```

The result contains:

- `page`: the documents in this page.
- `isDone`: whether pagination is complete and there is no next page.
- `continueCursor`: the opaque cursor for the next request; it is empty when the result is complete.

Treat the cursor as opaque. Do not decode, edit, or reuse it with different query bounds, ordering, or page size. When search or filter arguments change, restart with `cursor: null`.

For a paginated DTO, pass its fluent validator instead. The page item input and output types are inferred from that validator:

```ts
const taskSummary = v.object({ title: v.string() }).extend({ dueAt: v.number() });

export const listSummaries = query({
  returns: paginationResultValidator(taskSummary),
  handler: async () => ({ page: [], isDone: true, continueCursor: '' }),
});
```

## Look up a known ID directly

If you already have a typed ID, use `get` instead of querying `_id`:

```ts
const task = await ctx.db.get(args.taskId);
if (task === null) {
  throw new ApplicationError('not_found', { resource: 'task' });
}
```

`ApplicationError` is imported from `pbvex/server`; use it when absence is an expected, client-actionable domain result. `get` returns the document or `null`. Use `normalizeId(table, rawString)` only at a boundary where an untyped string must be validated. Never cast or manufacture IDs to make a query compile.

## Query best practices

- Start from the product access pattern, then define the index.
- Put tenant/owner and authorization scope in the index prefix when possible; do not load broad data and authorize afterward.
- Use equality fields first and the range/order field last in a compound index.
- Bound every result with `first`, `unique`, `take`, or `paginate`; reserve `collect` for known-small sets.
- Paginate before hydrating related records or performing expensive in-memory work.
- Select one useful index, then use `filter` only for residual conditions.
- Avoid fetching a table and filtering it in JavaScript.
- Keep realtime queries especially small because PBVex currently reruns every subscribed query after any successful record mutation, suppressing only unchanged results.
- Measure actual access patterns before adding overlapping indexes.
- Use [typed ID relationships](./relationships-and-joins.md) instead of copying entire related records by default.

## Common mistakes

| Mistake | Better approach |
| --- | --- |
| `collect()` on a table that grows with users or time | Use a selective index and `paginate()` |
| `fullTableScan().filter(...)` for a common request | Add an index matching the equality and range fields |
| Compound index fields in display order rather than constraint order | Put equality fields first, range/order field next |
| Skipping a field in a compound index range | Add a separate index for that access pattern |
| Sorting the collected array in JavaScript | Encode the required order in an index |
| Querying `_id` with a filter | Use `ctx.db.get(id)` |
| Reusing a cursor after changing filters | Restart pagination with `null` |
| Returning related IDs and making clients issue many calls | Hydrate bounded relationships in one backend query |
