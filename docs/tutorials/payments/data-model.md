# Payment data model

Keep checkout intent, received provider events, and application entitlements in separate tables. This prevents a browser redirect from granting access and gives webhook retries a durable idempotency key.

Add these tables to `pbvex/schema.ts` alongside any existing application tables:

```ts
import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  paymentCheckoutSessions: defineTable({
    owner: v.string(),
    plan: v.literal('premium'),
    status: v.union(v.literal('pending'), v.literal('completed')),
    providerCheckoutId: v.optional(v.string()),
  }).index('by_owner', ['owner']),

  billingAccounts: defineTable({
    owner: v.string(),
    plan: v.union(v.literal('free'), v.literal('premium')),
    status: v.union(v.literal('inactive'), v.literal('active')),
    checkoutSessionId: v.id('paymentCheckoutSessions'),
    activatedAt: v.number(),
  }).index('by_owner', ['owner']),

  paymentEvents: defineTable({
    providerEventId: v.string(),
    checkoutSessionId: v.id('paymentCheckoutSessions'),
    type: v.literal('checkout.completed'),
    receivedAt: v.number(),
  }).index('by_provider_event', ['providerEventId']),
});
```

Then regenerate the typed data model and function references:

```bash
pbvex codegen
```

## Table responsibilities

| Table | Responsibility | Important invariant |
| --- | --- | --- |
| `paymentCheckoutSessions` | Records what an authenticated owner intends to buy and the external checkout that fulfills it | The server derives `owner`; the client never supplies it |
| `paymentEvents` | Records accepted provider deliveries | `providerEventId` is checked before applying an event |
| `billingAccounts` | Stores the entitlement used by application functions | Only trusted webhook processing activates it |

`billingAccounts.by_owner` loads the current entitlement. `paymentEvents.by_provider_event` makes repeated webhook delivery safe. `paymentCheckoutSessions.by_owner` supports account history and cleanup of abandoned sessions.

`providerCheckoutId` is optional until the external create-checkout request succeeds. The webhook must match both this provider ID and the local checkout reference before granting access.

Continue with [Generate checkout](./checkout.md).
