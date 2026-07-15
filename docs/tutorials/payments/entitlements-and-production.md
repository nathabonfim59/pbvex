# Entitlements and production

Payment records do not protect features by themselves. Every premium backend entry point must load the authenticated owner's current entitlement before performing protected work.

## Centralize the premium check

```ts
// pbvex/lib/billing.ts
import type { MutationCtx, QueryCtx } from '../_generated/server';

export async function requirePremium(
  ctx: QueryCtx | MutationCtx,
  owner: string,
) {
  const account = await ctx.db
    .query('billingAccounts')
    .withIndex('by_owner', (q) => q.eq('owner', owner))
    .unique();

  if (!account || account.plan !== 'premium' || account.status !== 'active') {
    throw new Error('premium required');
  }
  return account;
}
```

The caller passes `tokenIdentifier` from authenticated server context. It must not accept an owner asserted by the browser.

## Extension: premium messaging attachments

The [messaging tutorial](../messaging/) allows authenticated conversation members to upload attachments. To make uploads premium, add the entitlement check to its `attachments.createUpload` mutation:

```ts
handler: async (ctx) => {
  const user = await requireIdentity(ctx.auth);
  await requirePremium(ctx, user.tokenIdentifier);
  return ctx.storage.generateUploadUrl();
},
```

Apply the helper to every backend function that creates or mutates a protected resource. Hiding an upload button in the browser is useful presentation, but it is not authorization.

## Expose current plan state

A query can drive plan badges and feature controls:

```ts
// pbvex/payments.ts
import { query } from './_generated/server';
import { v } from 'pbvex/values';
import { requireIdentity } from './lib/identity';

export const getMyPlan = query({
  args: {},
  returns: v.union(v.literal('free'), v.literal('premium')),
  handler: async (ctx) => {
    const user = await requireIdentity(ctx.auth);
    const account = await ctx.db
      .query('billingAccounts')
      .withIndex('by_owner', (q) =>
        q.eq('owner', user.tokenIdentifier),
      )
      .unique();

    return account?.status === 'active' && account.plan === 'premium'
      ? 'premium'
      : 'free';
  },
});
```

Watch `api.payments.getMyPlan` in the client to update the UI after the webhook commits. Continue enforcing `requirePremium` on the backend even when the current UI state says `premium`.

## Adapt the FakePay contract for production

FakePay represents an external sandbox API, not code hosted by PBVex. Replace its fictional URLs and payloads with your chosen provider's documented contract. A production integration should:

- create checkout sessions through a server-side provider API;
- keep API keys and webhook secrets in a declared [component environment binding](../../guides/environment-variables.md), never in a root function or browser;
- verify the provider signature against the unmodified raw request body before parsing it;
- map only recognized event types and fields into a narrow internal mutation;
- retain the event-ID index and transactional idempotency pattern;
- handle cancellation, expiration, refunds, and out-of-order subscription events;
- use separate secrets and provider accounts for development, staging, and production.

Root HTTP actions do not receive `ctx.env`. Keep provider-secret operations in a component action and invoke its generated `internal.components...` reference from the root webhook action, as this tutorial does. Prefer the provider's official local webhook verifier when it supports PBVex's bundled JavaScript runtime; otherwise use a provider-documented verification API or a trusted proxy boundary.

The tutorial also simplifies entitlement history to one current billing row. A subscription product will usually preserve provider customer/subscription IDs, period boundaries, cancellation state, and event timestamps so stale events cannot overwrite newer state.

See [Outbound HTTP](../../guides/outbound-http.md), [HTTP actions](../../guides/http-actions.md), [Environment variables and secrets](../../guides/environment-variables.md), and [Authorization](../../guides/authorization.md) for the underlying boundaries.
