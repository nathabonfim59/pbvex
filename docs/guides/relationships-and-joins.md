# Relationships and joins

PBVex v1 does not currently provide joins or automatic relation expansion. Model relationships with typed `v.id('table')` fields, fetch a known related document with `ctx.db.get`, and index the referencing table for reverse lookups. Compose the final result inside one server query.

This keeps record IDs table- and namespace-safe and makes every database access path explicit. Review [Querying data and designing indexes](./querying-and-indexes.md) first if index prefixes, ranges, or pagination are unfamiliar.

## The PBVex relationship model

Use these primitives to work with related data:

- `v.id('users')` declares a typed reference to the `users` table.
- `ctx.db.get(id)` resolves a known reference.
- An index on the referencing field provides the reverse lookup.
- A junction table represents a many-to-many relationship.
- A server query fetches and aggregates the related records into the returned view model.

PBVex validates that an ID is authentic and belongs to the expected table, but it does not currently verify that the referenced record exists, expand it automatically, create back-relations, or cascade deletes. Resolve and aggregate related records yourself using the bounded patterns on this page. Keeping that work inside one query means the client makes one request rather than a request per related record.

## One-to-many relationships

For “one user has many messages,” store the user ID on each message and index it:

```ts
// pbvex/schema.ts
export default defineSchema({
  users: defineTable({
    name: v.string(),
  }),
  messages: defineTable({
    authorId: v.id('users'),
    channel: v.string(),
    body: v.string(),
  })
    .index('by_author', ['authorId'])
    .index('by_channel_created', ['channel']),
});
```

The `v.id('users')` validator is the typed reference contract. It ensures a message cannot accidentally store a messages-table ID in `authorId`, but it does not prove that the referenced user currently exists. There is no automatic cascade or relationship expansion.

### Child to parent

When you have a message, fetch its author directly by ID:

```ts
const message = await ctx.db.get(args.messageId);
if (message === null) return null;

const author = await ctx.db.get(message.authorId);
return { message, author };
```

The author may be `null` if it was deleted, so decide whether the relationship is required by application policy and handle the missing case.

### Parent to children

Use the reverse index to fetch a user’s messages:

```ts
const messages = await ctx.db
  .query('messages')
  .withIndex('by_author', (q) => q.eq('authorId', args.userId))
  .order('desc')
  .paginate({ cursor: args.cursor ?? null, numItems: 50 });
```

Without `by_author`, the reverse lookup would require a full-table filter. Add a reverse index for every relationship traversal that is part of a real access pattern.

## Hydrate related records in a bounded query

Clients should not need one network request per related record. Page the primary records first, fetch each distinct related ID once, and assemble the response on the backend:

```ts
export const listWithAuthors = query({
  args: {
    channel: v.string(),
    cursor: v.optional(v.string()),
  },
  handler: async (ctx, { channel, cursor }) => {
    const result = await ctx.db
      .query('messages')
      .withIndex('by_channel_created', (q) => q.eq('channel', channel))
      .order('desc')
      .paginate({ cursor: cursor ?? null, numItems: 25 });

    const authorIds = [...new Set(result.page.map((message) => message.authorId))];
    const authors = await Promise.all(authorIds.map((id) => ctx.db.get(id)));
    const authorById = new Map(
      authorIds.map((id, index) => [id, authors[index] ?? null]),
    );

    return {
      ...result,
      page: result.page.map((message) => ({
        message,
        author: authorById.get(message.authorId) ?? null,
      })),
    };
  },
});
```

The important bound is the page size: hydrate relationships only after limiting the primary result. Deduplicate IDs before fetching them. For a deeply connected graph, return the first useful layer rather than recursively expanding every relationship.

This pattern avoids client-side request waterfalls, but it is still multiple database lookups inside one function—not a native join. If the same hot query repeatedly hydrates many records, consider a deliberate denormalized projection.

Two or a small bounded number of reads are normally inexpensive because the database calls run locally inside the PBVex server; there is no network round trip between `ctx.db` calls. The danger is an unbounded N+1 pattern. Always page or `take` the primary records first, deduplicate related IDs, and avoid recursively expanding a graph. PBVex does not currently provide a batched `getMany` or automatic relation API.

## Realtime subscriptions and hydrated relationships

Subscribe to the query that returns the fully assembled view model—not separately to every related record:

```ts
client.watch(api.contacts.listWithCompanies, args, {
  onUpdate(result) {
    renderContacts(result.data);
  },
});
```

PBVex currently uses conservative realtime invalidation. Every successful record create, update, or delete asks every active PBVex subscription to rerun its query. Invalidation bursts are coalesced, and the server sends a message only when the query’s canonical result differs from the last result.

Therefore, if `listWithCompanies` pages contacts and hydrates each company with `ctx.db.get`, changing a returned contact or company automatically causes the query to rerun and the client receives the updated composite result. A change unrelated to that result may still cause a rerun, but no client update is sent when the returned value is unchanged.

This is correct but intentionally broad: PBVex does not yet track which tables or records each query read. Keep subscribed queries selective, indexed, bounded, and inexpensive enough to rerun.

## Many-to-many relationships

Represent many-to-many relationships with a junction table. For users belonging to projects:

```ts
export default defineSchema({
  users: defineTable({ name: v.string() }),
  projects: defineTable({ name: v.string() }),
  memberships: defineTable({
    userId: v.id('users'),
    projectId: v.id('projects'),
    role: v.union(v.literal('member'), v.literal('admin')),
  })
    .index('by_user', ['userId'])
    .index('by_project', ['projectId'])
    .index('by_project_user', ['projectId', 'userId']),
});
```

Each traversal needs its own leading index:

- `by_user` lists projects for one user.
- `by_project` lists users for one project.
- `by_project_user` checks one exact membership with two equality constraints.

```ts
const membership = await ctx.db
  .query('memberships')
  .withIndex('by_project_user', (q) =>
    q.eq('projectId', projectId).eq('userId', userId),
  )
  .unique();
```

Indexes are not unique constraints. `unique()` reports an application invariant violation if duplicates already exist; it does not turn the index into a database uniqueness constraint. Keep membership creation idempotent, check for an existing relationship inside the mutation, and decide how repeated requests should behave.

## One-to-one relationships

A one-to-one relationship is usually one table holding the other table’s ID. Put the ID on the side whose lifecycle depends on the other record.

For a separate user profile:

```ts
profiles: defineTable({
  userId: v.id('users'),
  bio: v.string(),
}).index('by_user', ['userId'])
```

Read it with `withIndex('by_user', ...).unique()`. As with many-to-many relationships, the index documents and accelerates the expected invariant but is not itself a uniqueness constraint. Enforce creation and repair behavior in mutations.

If the fields always share the same lifecycle, permissions, and read pattern as the parent, a nested `v.object(...)` field may be simpler than a separate table.

## Denormalization

Denormalization copies selected related data into the record being read. It can remove repeated hydration from a hot read path:

```ts
messages: defineTable({
  authorId: v.id('users'),
  authorNameAtSendTime: v.string(),
  body: v.string(),
})
```

Use denormalization when the copied value is intentionally a snapshot, or when the application has a clear process for updating every copy. The field name should communicate snapshot semantics when appropriate.

Do not copy entire mutable records merely to imitate a join. Duplication introduces consistency work, larger documents, and fan-out writes. Keep the source ID even when copying display data so authorization and navigation can use the canonical record.

## Deletes and referential integrity

PBVex validates ID authenticity and target-table binding when an ID value is written, but it does not automatically verify existence, prevent deletion of a referenced record, or cascade to related rows. Implement the desired policy in a mutation:

- **Restrict:** query the reverse index and refuse deletion while references exist.
- **Cascade:** delete bounded related rows in the same mutation, or schedule bounded cleanup batches for a large relationship.
- **Detach:** patch optional references to remove them.
- **Preserve history:** retain the ID and an intentional snapshot, and allow the current related record to resolve to `null`.

Always re-read authorization-relevant parent records in the mutation. Possessing a valid typed ID does not prove the caller may access or modify that record.

## Relationship best practices

- Store relationships as `v.id('targetTable')`, never as unvalidated strings.
- Add an index on the referencing field for every required reverse traversal.
- Put tenant or owner scope first in a compound relationship index when it is part of authorization.
- Fetch by known ID with `ctx.db.get`; do not filter `_id`.
- Page or `take` the primary records before hydrating related data.
- Deduplicate related IDs and fetch each once per function call.
- Return composite view models from queries instead of exposing client request waterfalls.
- Use junction tables for many-to-many relationships.
- Treat one-to-one and membership uniqueness as an application invariant; indexes are not unique constraints.
- Define deletion behavior explicitly because there are no automatic cascades.
- Denormalize only with a documented consistency or snapshot policy.
- Avoid unbounded graph expansion and cyclic hydration.
- Remember that component tables and IDs are namespace-isolated; root code cannot join into a component’s private tables.

## Common relationship shapes

| Shape | Storage model | Read strategy |
| --- | --- | --- |
| Message belongs to user | `messages.authorId: v.id('users')` | `get(authorId)`; `messages.by_author` for reverse lookup |
| User has one profile | `profiles.userId: v.id('users')` | `profiles.by_user(...).unique()` |
| Users belong to projects | `memberships` junction table | Separate `by_user` and `by_project` indexes |
| Immutable historical author name | ID plus snapshot field | Read snapshot; resolve ID only when current data is needed |
| Large parent-child collection | Child stores parent ID | Paginate child index, then hydrate bounded related IDs |
