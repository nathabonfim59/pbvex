# Messages and realtime

Messages combine three rules: only current conversation members may read or send, the server derives the sender identity, and only the original sender may edit or delete.

## Send a message

```ts
// pbvex/messages.ts
import { mutation, query } from './_generated/server';
import { ApplicationError } from 'pbvex/server';
import { v } from 'pbvex/values';
import { requireIdentity } from './lib/identity';
import { requireMembership } from './lib/membership';
import { messagePageValidator } from './lib/validators';

export const send = mutation({
  args: {
    conversationId: v.id('conversations'),
    body: v.string(),
  },
  returns: v.id('messages'),
  handler: async (ctx, { conversationId, body }) => {
    const user = await requireIdentity(ctx.auth);
    const membership = await requireMembership(
      ctx,
      conversationId,
      user.tokenIdentifier,
    );

    const normalizedBody = body.trim();
    if (!normalizedBody || normalizedBody.length > 4_000) {
      throw new ApplicationError('bad_request', {
        field: 'body',
        reason: 'invalid_length',
        maxLength: 4_000,
      });
    }

    return ctx.db.insert('messages', {
      conversationId,
      sender: user.tokenIdentifier,
      senderProfileId: membership.profileId,
      body: normalizedBody,
    });
  },
});
```

The client supplies the conversation and content, but never `sender` or `senderProfileId`.

## Page messages and hydrate senders

Verify membership before touching the message range. Then page messages before resolving related profiles:

```ts
export const list = query({
  args: {
    conversationId: v.id('conversations'),
    cursor: v.optional(v.string()),
  },
  returns: messagePageValidator,
  handler: async (ctx, { conversationId, cursor }) => {
    const user = await requireIdentity(ctx.auth);
    await requireMembership(ctx, conversationId, user.tokenIdentifier);

    const result = await ctx.db
      .query('messages')
      .withIndex('by_conversation', (q) =>
        q.eq('conversationId', conversationId),
      )
      .order('desc')
      .paginate({ cursor: cursor ?? null, numItems: 50 });

    const profileIds = [
      ...new Set(result.page.map((message) => message.senderProfileId)),
    ];
    const profiles = await Promise.all(
      profileIds.map((profileId) => ctx.db.get(profileId)),
    );
    const profileById = new Map(
      profileIds.map((profileId, index) => [
        profileId,
        profiles[index] ?? null,
      ]),
    );

    return {
      ...result,
      page: result.page.map((message) => {
        const profile = profileById.get(message.senderProfileId);
        return {
          message: {
            _id: message._id,
            _creationTime: message._creationTime,
            conversationId: message.conversationId,
            senderProfileId: message.senderProfileId,
            body: message.body,
            editedAt: message.editedAt,
          },
          sender: profile
            ? {
                _id: profile._id,
                handle: profile.handle,
                displayName: profile.displayName,
                avatarStorageId: profile.avatarStorageId,
              }
            : null,
        };
      }),
    };
  },
});
```

This performs a bounded server-side aggregation. It avoids a client request per sender while keeping the primary query limited to one page.

## Edit only messages you sent

```ts
export const edit = mutation({
  args: {
    messageId: v.id('messages'),
    body: v.string(),
  },
  returns: v.boolean(),
  handler: async (ctx, { messageId, body }) => {
    const user = await requireIdentity(ctx.auth);
    const message = await ctx.db.get(messageId);

    if (!message || message.sender !== user.tokenIdentifier) {
      return false;
    }

    await requireMembership(
      ctx,
      message.conversationId,
      user.tokenIdentifier,
    );

    const normalizedBody = body.trim();
    if (!normalizedBody || normalizedBody.length > 4_000) {
      throw new ApplicationError('bad_request', {
        field: 'body',
        reason: 'invalid_length',
        maxLength: 4_000,
      });
    }

    await ctx.db.patch(messageId, {
      body: normalizedBody,
      editedAt: Date.now(),
    });
    return true;
  },
});
```

A delete mutation uses the same sender and current-membership checks before `ctx.db.delete(messageId)`. If administrators may moderate content, add an explicit administrator policy rather than bypassing checks conditionally in the client.

## Subscribe from the client

Use the same authorized query for the initial result and subsequent updates:

```ts
import { PBVexError } from '@pbvex/client';

const applicationStatuses = {
  bad_request: 400,
  unauthorized: 401,
  forbidden: 403,
  not_found: 404,
  conflict: 409,
} as const;

const unsubscribe = client.watch(
  api.messages.list,
  { conversationId },
  {
    onUpdate(result) {
      if (result.error) {
        if (result.error instanceof PBVexError) {
          const category = result.error.code;
          const status = category in applicationStatuses
            ? applicationStatuses[
                category as keyof typeof applicationStatuses
              ]
            : undefined;
          showConversationUnavailable({
            category,
            status,
            data: result.error.data,
          });
        } else {
          showConversationUnavailable();
        }
        return;
      }
      if (!result.isLoading) {
        renderMessages(result.data?.page ?? []);
      }
    },
    onConnectionStateChange(state) {
      renderConnectionState(state);
    },
  },
);
```

For an `ApplicationError`, `PBVexError.code` is the category and `.data` is the deliberately safe payload supplied by the backend. The status above is the deterministic HTTP status for an ordinary call; a realtime subscription has already opened an HTTP 200 stream, so its failed evaluation carries the category and data in an event instead. Treat ordinary errors as generic failures rather than displaying their messages.

The authenticated identity is preserved for subscription evaluations. Sending, editing, deleting, or changing membership causes subscribed queries to reevaluate. PBVex sends a new result only when its canonical value changed.

Current invalidation is conservative: any successful record change asks all active subscriptions to rerun. Keep realtime queries indexed and bounded as shown above.

## Send from the client

```ts
await client.mutation(api.messages.send, {
  conversationId,
  body: composerText,
});
```

The subscription becomes the authoritative source of the updated message list. A UI may add an optimistic local message for responsiveness, but it should reconcile with the subscribed result and handle mutation failure.

## Pagination strategy

The example subscribes to the newest page. Load older pages with ordinary query calls using `continueCursor`. Do not reuse a cursor after changing the conversation, page size, filters, or ordering.

For long conversations, keep previously loaded pages in client state while the live subscription owns the newest window. PBVex does not merge historical pages for the client automatically.

Continue with [Attachments](./attachments.md).
