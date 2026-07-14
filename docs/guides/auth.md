# Authentication

PBVex distinguishes application-record tokens from deployment credentials. Application tokens identify a PocketBase auth record for public calls, realtime, storage downloads, and HTTP actions. PocketBase superuser tokens authorize deployment upload/activation/rollback and scheduler administration; they are never application identities.

## Client token lifecycle

Pass an application token to the client constructor or update it after sign-in. A provider may return a token synchronously or asynchronously.

```ts
import { PBVexClient } from '@pbvex/sdk-core';

const client = new PBVexClient('https://api.example.test');
client.setAuth(() => localStorage.getItem('pb_token') ?? '');

// After sign-out:
client.clearAuth();
```

The client sends a resolved token as `Authorization: Bearer <token>` for calls and refreshes its realtime transport’s auth when configured. Per-call auth can override the configured source. Clearing auth removes the default; it does not revoke a PocketBase token.

## Identity in a function

Every function context has `ctx.auth.getUserIdentity()`. It resolves to `null` for an unauthenticated request; do not assume a public function has a user.

```ts
import { mutation } from './_generated/server';
import { v } from 'pbvex/values';

export const createPrivateMessage = mutation({
  args: { body: v.string() },
  handler: async (ctx, { body }) => {
    const user = await ctx.auth.getUserIdentity();
    if (!user) throw new Error('unauthenticated');
    return ctx.db.insert('messages', { body, owner: user.tokenIdentifier });
  },
});
```

An identity includes stable `subject`, `issuer`, and `tokenIdentifier`, plus selected optional profile claims such as `email` and `name`. Store `tokenIdentifier` when you need a durable ownership key. It is a portable representation, not the underlying PocketBase record and not a claim that the user may access a particular document.

## Authorization boundaries

Public means a client can reach the function; it does not grant access to data. Validate identity, ownership, tenant membership, and input on every sensitive entry point. Internal functions are unavailable to public call/realtime endpoints, but should still validate arguments and caller assumptions because backend code can invoke them.

Nested calls preserve the originating identity and request metadata. A scheduled job is a later background invocation, not a live user request: do not use it as proof of an authenticated session; pass and re-check the durable authorization data your job needs. HTTP actions receive the same optional app identity, while webhook signatures are a separate authentication mechanism.

Never place a superuser deployment token in a web client, bundle, or `pbvex.config.ts`. Keep it in deployment environment/credentials as described in [the operator guide](../operator.md#bootstrap-deployment-credentials). A valid application token cannot deploy code, and a superuser token is not exposed through `ctx.auth` as an app user.
