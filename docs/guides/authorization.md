# Authorization

Authentication establishes who called a function. Authorization decides what that identity may read or change. PBVex validates function visibility, arguments, and record IDs; the application defines and enforces its permission policy inside functions.

For a complete application that applies these rules, follow the [Messaging app tutorial](../tutorials/messaging/).

## Authorization happens at the function boundary

Public means client-reachable, not permitted for every caller. Every sensitive public query, mutation, action, and HTTP action must make an authorization decision before exposing data or producing an effect.

| Question | Application responsibility |
| --- | --- |
| Is a user signed in? | Resolve `ctx.auth.getUserIdentity()` |
| Does a record belong to them? | Compare a stored owner with `user.tokenIdentifier` |
| May they access a group or tenant? | Read current membership through an index |
| May they perform this operation? | Check an explicit owner, role, or capability policy |
| Is a supplied ID safe? | Validate it with `v.id(...)`, then authorize the resolved record separately |
| Does a subscription remain protected? | Put the same checks inside the subscribed query |

Authorization applies to reads and writes. Hiding a button, filtering results in the browser, or requiring a valid ID is not a server-side permission check.

## Require identity

`ctx.auth.getUserIdentity()` returns the current application identity or `null`. Reject anonymous callers before reading protected state:

```ts
import { ApplicationError } from 'pbvex/server';

const user = await ctx.auth.getUserIdentity();
if (!user) {
  throw new ApplicationError('unauthorized', {
    reason: 'authentication_required',
  });
}
```

Use `user.tokenIdentifier` as a durable ownership key. It is stable and globally qualified across auth collections. Do not authorize with an email address, display name, route parameter, or client-provided identity.

You may centralize this first step in a helper:

```ts
import {
  ApplicationError,
  type AuthContext,
  type UserIdentity,
} from 'pbvex/server';

export async function requireIdentity(
  auth: AuthContext,
): Promise<UserIdentity> {
  const user = await auth.getUserIdentity();
  if (!user) {
    throw new ApplicationError('unauthorized', {
      reason: 'authentication_required',
    });
  }
  return user;
}
```

The helper authenticates the caller; each function must still perform the ownership, membership, or role check appropriate to its operation.

## Owner-scoped data

For private records, store an owner and index it:

```ts
comments: defineTable({
  owner: v.string(),
  body: v.string(),
}).index('by_owner', ['owner'])
```

Derive the owner during creation. Never accept it as a client argument:

```ts
const user = await requireIdentity(ctx.auth);

return ctx.db.insert('comments', {
  owner: user.tokenIdentifier,
  body: args.body,
});
```

Scope list queries through the ownership index:

```ts
return ctx.db
  .query('comments')
  .withIndex('by_owner', (q) =>
    q.eq('owner', user.tokenIdentifier),
  )
  .take(100);
```

This prevents unrelated records from entering the result in the first place. Do not collect a broad table and filter it in application or client code.

## Authorize ID-based operations

`v.id('comments')` proves that the value is an authentic ID for the comments table. It does not prove the record exists or that the caller owns it.

Fetch the record and check its stored policy fields before returning, updating, or deleting it:

```ts
const user = await requireIdentity(ctx.auth);
const comment = await ctx.db.get(args.commentId);

if (!comment || comment.owner !== user.tokenIdentifier) {
  return false;
}

await ctx.db.patch(comment._id, { body: args.body });
return true;
```

Returning the same result for missing and unauthorized records prevents callers from using the function to discover another user’s data.

## Membership and roles

Use a membership table when access belongs to a group, conversation, project, or tenant:

```ts
memberships: defineTable({
  resourceId: v.id('projects'),
  user: v.string(),
  role: v.union(v.literal('member'), v.literal('admin')),
}).index('by_resource_user', ['resourceId', 'user'])
```

Resolve current membership before reading the resource:

```ts
const membership = await ctx.db
  .query('memberships')
  .withIndex('by_resource_user', (q) =>
    q.eq('resourceId', args.projectId)
      .eq('user', user.tokenIdentifier),
  )
  .unique();

if (!membership) {
  throw new ApplicationError('forbidden', {
    reason: 'project_membership_required',
  });
}
```

For privileged operations, check `membership.role` as well. Store role and membership state on the server; never accept a client claim that the caller is an administrator.

Keep the permission check and protected database write inside the same mutation when possible so they share one transaction.

## Tenant-scoped indexes

For multi-tenant data, make the tenant or owner part of the index prefix:

```ts
.index('by_tenant_status_created', ['tenantId', 'status'])
```

Verify current tenant membership, then query only that tenant’s range. An index is a performance tool, not an authorization rule—the function must still establish that the identity may use the tenant ID.

## Queries and subscriptions

Put authorization inside the query clients subscribe to. Realtime reevaluation invokes the same query with the authenticated identity, so the permission check runs every time:

```ts
const publicCommentValidator =
  schema.tables.comments.documentValidator.omit('owner');

export const listPrivate = query({
  args: {},
  returns: v.array(publicCommentValidator),
  handler: async (ctx) => {
    const user = await requireIdentity(ctx.auth);
    const comments = await ctx.db
      .query('comments')
      .withIndex('by_owner', (q) =>
        q.eq('owner', user.tokenIdentifier),
      )
      .take(100);

    return comments.map((comment) => ({
      _id: comment._id,
      _creationTime: comment._creationTime,
      body: comment.body,
    }));
  },
});
```

If ownership or membership changes, the next evaluation must derive access from current database state rather than a value cached in the client.

## Function visibility is not permission

- Public functions require explicit authorization whenever they expose protected behavior.
- Internal functions cannot be invoked through the public call API, but backend callers can still supply incorrect or untrusted data. Validate their arguments and assumptions.
- Nested calls preserve the originating identity.
- HTTP actions receive optional application identity, while webhook signatures and shared secrets are separate authentication mechanisms.
- Scheduled and cron jobs do not represent a live user session. Pass stable record IDs and re-read current authorization state when the job executes.

## Direct API access cannot bypass functions

PBVex application tables are exposed through PBVex functions and query subscriptions, not through the generic record CRUD API. HTTP list, view, create, update, and delete requests for PBVex-owned backing collections are rejected, including privileged administrative requests.

Authentication/account endpoints remain available for signup, login, token refresh, OTP, OAuth2, and account management. Separately created non-PBVex collections have their own access configuration and are not an alternate write path into PBVex application state.

## Authorization checklist

- Derive identity from `ctx.auth`, never request arguments.
- Use `tokenIdentifier` for durable user ownership.
- Scope list queries with owner/tenant indexes.
- Treat every ID as an identifier, not permission.
- Check stored ownership before single-record reads, updates, and deletes.
- Re-read membership and roles for every protected operation.
- Keep checks and writes in one mutation when possible.
- Apply the same policy inside subscribed queries.
- Do not leak record existence through different missing/forbidden responses unless intentional.
- Re-check durable authorization state in delayed work.
- Test anonymous, owner, member, non-member, and administrator cases.

Next, build these rules into a working product with the [Messaging app tutorial](../tutorials/messaging/), or continue with [Querying data and designing indexes](./querying-and-indexes.md) and [Relationships and joins](./relationships-and-joins.md).
