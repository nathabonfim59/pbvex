# Authentication and profiles

The messaging app uses the configured `users` auth collection for sign-in and a PBVex `profiles` table for application-facing identity.

## Sign in from the client

Create one shared client instance:

```ts
import { PBVexClient } from '@pbvex/client';

export const client = new PBVexClient('http://127.0.0.1:8090');
export const users = client.auth.collection('users');
```

Authenticate with any enabled method. For password authentication:

```ts
await users.authWithPassword(email, password);

console.log(client.authStore.isValid);
console.log(client.authStore.record?.id);
```

For OAuth2:

```ts
await users.authWithOAuth2({ provider: 'google' });
```

Successful authentication updates `client.authStore`. Normal function calls and realtime subscriptions automatically use that token.

For this tutorial, provision password accounts through your chosen account-creation flow or use an OAuth2 provider that creates application users. The general [Authentication guide](../../guides/auth.md) covers OTP, MFA, verification, reset, and token lifecycle.

## Require identity in backend functions

Create a small reusable helper:

```ts
// pbvex/lib/identity.ts
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

This helper proves the request has an application identity. It does not decide whether that identity may access a conversation or message.

## Create the application profile

After first sign-in, call a mutation that creates the profile only when one does not already exist:

```ts
// pbvex/profiles.ts
import { mutation, query } from './_generated/server';
import { ApplicationError } from 'pbvex/server';
import { v } from 'pbvex/values';
import { requireIdentity } from './lib/identity';
import {
  maybePublicProfileValidator,
} from './lib/validators';

export const ensure = mutation({
  args: {
    handle: v.string(),
    displayName: v.string(),
  },
  returns: v.id('profiles'),
  handler: async (ctx, { handle, displayName }) => {
    const user = await requireIdentity(ctx.auth);
    const normalizedHandle = handle.trim().toLowerCase();

    if (!/^[a-z0-9_]{3,24}$/.test(normalizedHandle)) {
      throw new ApplicationError('bad_request', {
        field: 'handle',
        reason: 'invalid_format',
      });
    }

    const existing = await ctx.db
      .query('profiles')
      .withIndex('by_auth_user', (q) =>
        q.eq('authUser', user.tokenIdentifier),
      )
      .unique();

    if (existing) return existing._id;

    const handleOwner = await ctx.db
      .query('profiles')
      .withIndex('by_handle', (q) => q.eq('handle', normalizedHandle))
      .unique();

    if (handleOwner) {
      throw new ApplicationError('conflict', {
        field: 'handle',
        reason: 'unavailable',
      });
    }

    return ctx.db.insert('profiles', {
      authUser: user.tokenIdentifier,
      handle: normalizedHandle,
      displayName: displayName.trim(),
    });
  },
});
```

PBVex indexes are not unique constraints. The mutation checks both application invariants and treats repeated onboarding as idempotent. Your production policy should also define how handle changes and rare concurrent claims are resolved.

## Read the current profile

```ts
export const me = query({
  args: {},
  returns: maybePublicProfileValidator,
  handler: async (ctx) => {
    const user = await requireIdentity(ctx.auth);
    const profile = await ctx.db
      .query('profiles')
      .withIndex('by_auth_user', (q) =>
        q.eq('authUser', user.tokenIdentifier),
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

The client can run onboarding after authentication:

```ts
const profile = await client.query(api.profiles.me);

if (!profile) {
  await client.mutation(api.profiles.ensure, {
    handle,
    displayName,
  });
}
```

Do not accept `authUser` from the client. The mutation always derives it from the request token.

Continue with [Contacts](./contacts.md).
