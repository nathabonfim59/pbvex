# Backend primitives

PBVex applications are built from a small set of primitives. A **query** reads data, a **mutation** changes data transactionally, and an **action** coordinates side effects such as email or external APIs. HTTP actions expose custom HTTP endpoints, while scheduled jobs and cron jobs decide when a mutation or action should run.

## Function primitives at a glance

| Primitive | Use it for | Database access | External side effects | Client callable | Realtime |
| --- | --- | --- | --- | --- | --- |
| Query | Reading and deriving data | Read-only | No | Public queries only | Can be subscribed to |
| Mutation | Creating, updating, or deleting durable state | Transactional read/write | No irreversible network work | Public mutations only | Triggers subscription reevaluation |
| Action | Email, external APIs, and multi-step orchestration | Indirectly through function calls | Yes | Public actions only | Not directly subscribable |
| HTTP action | Webhooks and custom HTTP endpoints | Indirectly through function calls | Yes | Public HTTP request | No |

Choose the narrowest primitive that can perform the work. This makes transaction boundaries, allowed capabilities, and failure behavior obvious.

## Query

A query is a read-only function. Use it to fetch records, filter or aggregate data, and return a value without changing durable state.

```ts
export const list = query({
  args: { channel: v.string() },
  returns: v.array(v.string()),
  handler: async (ctx, { channel }) => {
    const messages = await ctx.db
      .query('messages')
      .withIndex('by_channel', (q) => q.eq('channel', channel))
      .collect();
    return messages.map((message) => message.body);
  },
});
```

Queries receive a `QueryCtx` with read-only database access, authentication information, and read-only storage access. They cannot write records, schedule work, send email, or call external services. Clients can subscribe to a public query. PBVex currently reruns every active subscribed query after any successful record mutation and emits only changed results.

Use a query when the operation can safely run more than once and has no observable effect beyond returning data.

## Mutation

A mutation is a transactional read/write function. Use it whenever durable application state changes.

```ts
export const send = mutation({
  args: { channel: v.string(), body: v.string() },
  returns: v.id('messages'),
  handler: (ctx, args) => ctx.db.insert('messages', args),
});
```

A top-level mutation runs in one database transaction. If its handler throws, times out, is canceled, or returns an invalid value, its database changes and transactionally scheduled jobs roll back together. Mutations can also upload/delete storage objects and enqueue durable work through `ctx.scheduler`.

Do not send email or call an external API from a mutation. A database transaction cannot roll back an effect that already reached another service. Schedule or call an action for that work.

## Action

An action handles non-transactional work and external side effects. Use it to send email, call third-party APIs, or coordinate multiple PBVex functions.

```ts
export const notify = action({
  args: { channel: v.string(), body: v.string() },
  returns: v.null(),
  handler: async (ctx, args) => {
    const messageId = await ctx.runMutation(api.messages.send, args);
    await ctx.email.send({
      template: 'new-message',
      to: 'operator@example.com',
      variables: { messageId },
    });
    return null;
  },
});
```

Actions do not receive direct `ctx.db` access. They use generated references with `ctx.runQuery`, `ctx.runMutation`, or `ctx.runAction`. Each nested mutation owns its own transaction; an action and the functions it calls are not one atomic transaction. Design external effects to be idempotent because a failure can happen between steps.

## HTTP action

An HTTP action is a public endpoint under the application HTTP prefix. It accepts a web-compatible `Request` and returns a `Response`.

Use HTTP actions for webhooks, callbacks, or integrations that cannot use the typed PBVex client. They can verify raw request bodies and call generated query, mutation, or action references, but they do not receive direct database access. Unlike the other function primitives, HTTP actions are addressed by an HTTP method and route rather than through `api` client references.

See [HTTP actions](../guides/http-actions.md) for routing, authentication, CORS, and webhook verification.

## Public and internal visibility

Every query, mutation, and action is either public or internal:

- `query`, `mutation`, and `action` create public functions represented under generated `api` references. A client can invoke them.
- `internalQuery`, `internalMutation`, and `internalAction` create backend-only functions represented under generated `internal` references.
- HTTP actions are always public HTTP routes.

Visibility controls reachability, not authorization. A public function that requires a signed-in user must still inspect `ctx.auth`, validate ownership, and enforce application permissions. Prefer internal functions for implementation details, scheduled targets, and cron targets.

## Scheduling primitives

Scheduling does not introduce another handler kind. It invokes an existing mutation or action later:

- `ctx.scheduler.runAfter(...)` creates one durable job after an elapsed delay.
- `ctx.scheduler.runAt(...)` creates one durable job for an exact instant.
- `pbvex/crons.ts` declares fixed recurring cron schedules.

Queries and HTTP actions cannot be scheduled targets. The generated reference preserves the target’s argument types, and the eventual invocation runs without the originating user session. See [Scheduling one-shot work](../guides/scheduling.md) and [Cron jobs](../guides/cron-jobs.md).

## Data and composition primitives

The function kinds sit on top of a few related building blocks:

- A **schema** declares the application’s tables.
- A **table** describes a durable record shape and its indexes; see [Querying data and designing indexes](../guides/querying-and-indexes.md).
- A **validator** checks arguments, return values, stored fields, component configuration, and generated TypeScript types.
- A generated **function reference** identifies a function while retaining its kind, visibility, arguments, and return type.
- A **component** packages schema and functions behind an isolated mount namespace for reuse.

Relationships are explicit typed IDs rather than implicit ORM fields. Read [Relationships and joins](../guides/relationships-and-joins.md) for one-to-many, many-to-many, hydration, deletion, and denormalization patterns.

These definitions are discovered during `pbvex codegen` and deployment builds. Application code should use generated references rather than constructing function names or record IDs manually.

## How to choose

Start with the effect the operation needs:

1. Only reads durable data: use a query.
2. Changes durable state atomically: use a mutation.
3. Calls an external service or coordinates several functions: use an action.
4. Must receive a webhook or custom HTTP request: use an HTTP action.
5. Must happen later once: schedule a mutation or action.
6. Must recur on a fixed application-wide UTC calendar: declare a cron job targeting a mutation or action.

Continue with [Backend functions](../guides/functions.md) for complete authoring syntax, validators, generated calls, and runtime limits.
