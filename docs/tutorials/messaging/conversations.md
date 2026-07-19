# Conversations and permissions

Conversation membership controls who can see and send messages. Every protected conversation operation reads current membership from the server.

## Reusable membership helper

Create a helper that works in queries and mutations:

```ts
// pbvex/lib/membership.ts
import { ApplicationError } from 'pbvex/server';
import type { MutationCtx, QueryCtx } from '../_generated/server';
import type { Id } from '../_generated/dataModel';

export async function requireMembership(
  ctx: QueryCtx | MutationCtx,
  conversationId: Id<'conversations'>,
  user: string,
) {
  const membership = await ctx.db
    .query('conversationMembers')
    .withIndex('by_conversation_user', (q) =>
      q.eq('conversationId', conversationId).eq('user', user),
    )
    .unique();

  if (!membership) {
    throw new ApplicationError('forbidden', {
      reason: 'conversation_membership_required',
    });
  }
  return membership;
}
```

The caller passes the authenticated `tokenIdentifier`, never a value supplied by the client.

## Create a conversation

The creator becomes an administrator. Selected participant profiles become members:

```ts
// pbvex/conversations.ts
import { mutation, query } from './_generated/server';
import { ApplicationError } from 'pbvex/server';
import { v } from 'pbvex/values';
import { requireIdentity } from './lib/identity';
import { conversationListItemValidator } from './lib/validators';

export const create = mutation({
  args: {
    title: v.optional(v.string()),
    participantProfileIds: v.array(v.id('profiles')),
  },
  returns: v.id('conversations'),
  handler: async (ctx, { title, participantProfileIds }) => {
    const user = await requireIdentity(ctx.auth);
    const uniqueIds = [...new Set(participantProfileIds)];
    if (uniqueIds.length > 49) {
      throw new ApplicationError('bad_request', {
        field: 'participantProfileIds',
        reason: 'too_many_participants',
        limit: 49,
      });
    }

    const ownProfile = await ctx.db
      .query('profiles')
      .withIndex('by_auth_user', (q) =>
        q.eq('authUser', user.tokenIdentifier),
      )
      .unique();
    if (!ownProfile) {
      throw new ApplicationError('conflict', {
        reason: 'profile_required',
      });
    }

    const participantProfiles = await Promise.all(
      uniqueIds.map((profileId) => ctx.db.get(profileId)),
    );
    if (participantProfiles.some((profile) => profile === null)) {
      throw new ApplicationError('not_found', {
        resource: 'participant_profile',
      });
    }

    const conversationId = await ctx.db.insert('conversations', {
      createdBy: user.tokenIdentifier,
      ...(title?.trim() ? { title: title.trim() } : {}),
    });

    await ctx.db.insert('conversationMembers', {
      conversationId,
      user: user.tokenIdentifier,
      profileId: ownProfile._id,
      role: 'admin',
    });

    for (const profile of participantProfiles) {
      if (!profile || profile.authUser === user.tokenIdentifier) continue;
      await ctx.db.insert('conversationMembers', {
        conversationId,
        user: profile.authUser,
        profileId: profile._id,
        role: 'member',
      });
    }

    return conversationId;
  },
});
```

All inserts share one mutation transaction. A failure does not leave a conversation without its creator membership.

In a production messaging app, decide whether participants must already be contacts before they can be invited. If so, verify each selected profile through `contacts.by_owner_profile` before creating memberships.

## List the current user’s conversations

Start from `by_user_conversation`, bound the result, and hydrate conversation records:

```ts
export const listMine = query({
  args: {},
  returns: v.array(conversationListItemValidator),
  handler: async (ctx) => {
    const user = await requireIdentity(ctx.auth);

    const memberships = await ctx.db
      .query('conversationMembers')
      .withIndex('by_user_conversation', (q) =>
        q.eq('user', user.tokenIdentifier),
      )
      .take(100);

    const conversations = await Promise.all(
      memberships.map((membership) =>
        ctx.db.get(membership.conversationId),
      ),
    );

    return memberships.flatMap((membership, index) => {
      const conversation = conversations[index];
      return conversation
        ? [{
            conversation: {
              _id: conversation._id,
              _creationTime: conversation._creationTime,
              title: conversation.title,
            },
            role: membership.role,
          }]
        : [];
    });
  },
});
```

The client never sends a user ID to list its conversations.

## Add a member as an administrator

```ts
export const addMember = mutation({
  args: {
    conversationId: v.id('conversations'),
    profileId: v.id('profiles'),
  },
  returns: v.boolean(),
  handler: async (ctx, { conversationId, profileId }) => {
    const user = await requireIdentity(ctx.auth);
    const callerMembership = await ctx.db
      .query('conversationMembers')
      .withIndex('by_conversation_user', (q) =>
        q.eq('conversationId', conversationId)
          .eq('user', user.tokenIdentifier),
      )
      .unique();

    if (!callerMembership || callerMembership.role !== 'admin') {
      return false;
    }

    const profile = await ctx.db.get(profileId);
    if (!profile) return false;

    const existing = await ctx.db
      .query('conversationMembers')
      .withIndex('by_conversation_user', (q) =>
        q.eq('conversationId', conversationId)
          .eq('user', profile.authUser),
      )
      .unique();
    if (existing) return true;

    await ctx.db.insert('conversationMembers', {
      conversationId,
      user: profile.authUser,
      profileId,
      role: 'member',
    });
    return true;
  },
});
```

Removing a member follows the same administrator check before deleting the target membership. Do not accept an `actor`, caller role, or administrator flag from the client.

## Membership removal and subscriptions

Protected message queries check membership on every evaluation. After an administrator removes a member, realtime invalidation reruns that user’s subscription and the query fails its membership check instead of returning further messages.

Historical messages can retain their original sender fields after membership removal. Current access and current authorship are separate facts.

Continue with [Messages and realtime](./messages-and-realtime.md).
