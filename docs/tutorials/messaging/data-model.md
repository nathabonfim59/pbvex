# Messaging data model

The application separates account identity from application profile data, models contacts and memberships explicitly, and keeps messages and attachment metadata queryable.

## Complete schema

Create `pbvex/schema.ts`:

```ts
import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  profiles: defineTable({
    authUser: v.string(),
    handle: v.string(),
    displayName: v.string(),
    avatarStorageId: v.optional(v.string()),
  })
    .index('by_auth_user', ['authUser'])
    .index('by_handle', ['handle']),

  contacts: defineTable({
    owner: v.string(),
    contactProfileId: v.id('profiles'),
    nickname: v.optional(v.string()),
  })
    .index('by_owner', ['owner'])
    .index('by_owner_profile', ['owner', 'contactProfileId']),

  conversations: defineTable({
    createdBy: v.string(),
    title: v.optional(v.string()),
  }),

  conversationMembers: defineTable({
    conversationId: v.id('conversations'),
    user: v.string(),
    profileId: v.id('profiles'),
    role: v.union(v.literal('member'), v.literal('admin')),
  })
    .index('by_conversation_user', ['conversationId', 'user'])
    .index('by_user_conversation', ['user', 'conversationId']),

  messages: defineTable({
    conversationId: v.id('conversations'),
    sender: v.string(),
    senderProfileId: v.id('profiles'),
    body: v.string(),
    editedAt: v.optional(v.number()),
  })
    .index('by_conversation', ['conversationId'])
    .index('by_sender', ['sender']),

  messageAttachments: defineTable({
    messageId: v.id('messages'),
    owner: v.string(),
    storageId: v.string(),
    filename: v.string(),
    contentType: v.string(),
    size: v.number(),
  })
    .index('by_message', ['messageId'])
    .index('by_owner', ['owner']),
});
```

Then generate the data model and server factories:

```bash
pbvex codegen
```

## Why identity and profile are separate

The authentication system owns credentials, verification, tokens, and enabled sign-in methods. The `profiles` table owns application-facing fields such as handle, display name, and avatar.

`authUser` stores `ctx.auth.getUserIdentity().tokenIdentifier`. This provides a stable link without copying credentials or treating an editable email address as identity.

## Why contacts store a profile ID

A contact belongs to one authenticated user and points at another user’s profile:

```text
owner tokenIdentifier -> contacts row -> contactProfileId -> profile
```

`by_owner` lists one user’s contacts. `by_owner_profile` checks whether a specific profile is already present without scanning the table.

## Why conversation membership is its own table

Membership is many-to-many: users can join many conversations, and conversations can contain many users. The two indexes support both directions:

- `by_conversation_user` checks access to a conversation.
- `by_user_conversation` lists the conversations available to a user.

The `role` field makes privileged membership operations explicit.

## Why attachment metadata is separate

Stored bytes are opaque objects. The application needs its own record for filename, content type, display size, owner, and message association. Keeping this metadata in `messageAttachments` also makes authorization start from the owning message and conversation.

The client-provided filename, content type, and size are display metadata; do not use them to bypass the server’s actual upload limits or content checks.

## Index design summary

| Access pattern | Index |
| --- | --- |
| Current identity’s profile | `profiles.by_auth_user` |
| Public handle lookup | `profiles.by_handle` |
| Current user’s contacts | `contacts.by_owner` |
| Prevent duplicate contact | `contacts.by_owner_profile` |
| Check conversation membership | `conversationMembers.by_conversation_user` |
| List user conversations | `conversationMembers.by_user_conversation` |
| Page conversation messages | `messages.by_conversation` |
| Load message attachments | `messageAttachments.by_message` |

Continue with [Authentication and profiles](./authentication-and-profiles.md).
