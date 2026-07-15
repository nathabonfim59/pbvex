# Contacts

Contacts are private to the current user. A contact row stores the owner’s authenticated identity and a typed reference to the other user’s public application profile.

## Find a profile by handle

Require authentication even when handles are searchable, then return only fields intended for discovery:

```ts
// pbvex/profiles.ts
export const findByHandle = query({
  args: { handle: v.string() },
  handler: async (ctx, { handle }) => {
    await requireIdentity(ctx.auth);

    const profile = await ctx.db
      .query('profiles')
      .withIndex('by_handle', (q) =>
        q.eq('handle', handle.trim().toLowerCase()),
      )
      .unique();

    if (!profile) return null;

    return {
      _id: profile._id,
      handle: profile.handle,
      displayName: profile.displayName,
      avatarStorageId: profile.avatarStorageId,
    };
  },
});
```

Do not return `authUser`. It is an internal ownership/membership key, not public profile data.

## Add a contact

The client supplies the selected profile ID. The mutation derives the owner and prevents self-contact or duplicates:

```ts
// pbvex/contacts.ts
import { mutation, query } from './_generated/server';
import { v } from 'pbvex/values';
import { requireIdentity } from './lib/identity';

export const add = mutation({
  args: {
    profileId: v.id('profiles'),
    nickname: v.optional(v.string()),
  },
  returns: v.id('contacts'),
  handler: async (ctx, { profileId, nickname }) => {
    const user = await requireIdentity(ctx.auth);
    const profile = await ctx.db.get(profileId);
    if (!profile) throw new Error('profile not found');
    if (profile.authUser === user.tokenIdentifier) {
      throw new Error('cannot add yourself');
    }

    const existing = await ctx.db
      .query('contacts')
      .withIndex('by_owner_profile', (q) =>
        q.eq('owner', user.tokenIdentifier)
          .eq('contactProfileId', profileId),
      )
      .unique();

    if (existing) return existing._id;

    return ctx.db.insert('contacts', {
      owner: user.tokenIdentifier,
      contactProfileId: profileId,
      ...(nickname ? { nickname: nickname.trim() } : {}),
    });
  },
});
```

The client cannot add a contact on behalf of someone else because `owner` is not an argument.

## Return hydrated contact cards

Load a bounded set of the current user’s contact rows, deduplicate profile IDs, and aggregate safe profile fields in the same server query:

```ts
export const list = query({
  args: {},
  handler: async (ctx) => {
    const user = await requireIdentity(ctx.auth);

    const contacts = await ctx.db
      .query('contacts')
      .withIndex('by_owner', (q) =>
        q.eq('owner', user.tokenIdentifier),
      )
      .take(200);

    const profileIds = [
      ...new Set(contacts.map((contact) => contact.contactProfileId)),
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

    return contacts.flatMap((contact) => {
      const profile = profileById.get(contact.contactProfileId);
      if (!profile) return [];

      return [{
        contactId: contact._id,
        nickname: contact.nickname,
        profile: {
          _id: profile._id,
          handle: profile.handle,
          displayName: profile.displayName,
          avatarStorageId: profile.avatarStorageId,
        },
      }];
    });
  },
});
```

This is not a native join, but it remains one client request and the reads run locally in the server. The initial `take(200)` prevents unbounded relationship hydration.

## Remove only your own contact

```ts
export const remove = mutation({
  args: { contactId: v.id('contacts') },
  returns: v.boolean(),
  handler: async (ctx, { contactId }) => {
    const user = await requireIdentity(ctx.auth);
    const contact = await ctx.db.get(contactId);

    if (!contact || contact.owner !== user.tokenIdentifier) {
      return false;
    }

    await ctx.db.delete(contactId);
    return true;
  },
});
```

Return the same `false` result for missing and unauthorized contact IDs so the function does not reveal another user’s private contact list.

## Client flow

```ts
const profile = await client.query(api.profiles.findByHandle, {
  handle: searchText,
});

if (profile) {
  await client.mutation(api.contacts.add, {
    profileId: profile._id,
  });
}

const contacts = await client.query(api.contacts.list);
```

Continue with [Conversations and permissions](./conversations.md).
