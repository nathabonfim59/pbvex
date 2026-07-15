# Process payment webhooks

Configure FakePay's sandbox dashboard to deliver events to:

```text
https://api.example.com/api/pbvex/webhooks/fakepay
```

The external provider—not the browser—calls this URL after checkout. Providers retry when responses time out or fail, so signature verification and idempotency are both required.

## Verify the provider request

This fictional FakePay exposes an authenticated verification endpoint that accepts the unmodified payload and signature and returns a normalized verified event. Keep that provider call inside the credential-bearing component:

```ts
// pbvex/integrations/fakePay/adapter.ts
export const verifyWebhook = fakePayFns.internalAction({
  args: {
    rawBody: v.string(),
    signature: v.string(),
  },
  returns: v.object({
    providerEventId: v.string(),
    type: v.literal('checkout.completed'),
    providerCheckoutId: v.string(),
    clientReferenceId: v.string(),
  }),
  handler: async (ctx, { rawBody, signature }) => {
    const response = await ctx.http.send({
      url: `${ctx.env.API_BASE_URL}/v1/webhooks/verify`,
      method: 'POST',
      headers: {
        authorization: `Bearer ${ctx.env.API_KEY}`,
        'content-type': 'application/json',
      },
      body: JSON.stringify({ payload: rawBody, signature }),
      timeoutMs: 5_000,
    });

    const event = response.json;
    if (
      response.statusCode !== 200 ||
      !event || typeof event !== 'object' ||
      typeof (event as any).id !== 'string' ||
      (event as any).type !== 'checkout.completed' ||
      typeof (event as any).checkoutId !== 'string' ||
      typeof (event as any).clientReferenceId !== 'string'
    ) {
      throw new Error('FakePay webhook verification failed');
    }

    return {
      providerEventId: (event as any).id,
      type: 'checkout.completed',
      providerCheckoutId: (event as any).checkoutId,
      clientReferenceId: (event as any).clientReferenceId,
    };
  },
});
```

This verification API is part of the tutorial's fake external provider contract. For Stripe or another real provider, follow its official signature procedure—usually an SDK that verifies a timestamped signature locally against the raw body and a webhook signing secret. Never treat successful JSON parsing as authenticity.

## Apply completion transactionally

Treat the verified provider event ID as an idempotency key. Match both the local client reference and the provider checkout ID before changing the entitlement:

```ts
// pbvex/payments.ts
export const applyCompletedCheckout = internalMutation({
  args: {
    providerEventId: v.string(),
    checkoutSessionId: v.id('paymentCheckoutSessions'),
    providerCheckoutId: v.string(),
  },
  returns: v.object({ duplicate: v.boolean() }),
  handler: async (ctx, event) => {
    const prior = await ctx.db
      .query('paymentEvents')
      .withIndex('by_provider_event', (q) =>
        q.eq('providerEventId', event.providerEventId),
      )
      .unique();
    if (prior) return { duplicate: true };

    const session = await ctx.db.get(event.checkoutSessionId);
    if (!session || session.status !== 'pending') {
      throw new Error('invalid checkout session');
    }
    if (
      session.providerCheckoutId &&
      session.providerCheckoutId !== event.providerCheckoutId
    ) {
      throw new Error('provider checkout mismatch');
    }

    const now = Date.now();
    const account = await ctx.db
      .query('billingAccounts')
      .withIndex('by_owner', (q) => q.eq('owner', session.owner))
      .unique();

    if (account) {
      await ctx.db.patch(account._id, {
        plan: 'premium',
        status: 'active',
        checkoutSessionId: session._id,
        activatedAt: now,
      });
    } else {
      await ctx.db.insert('billingAccounts', {
        owner: session.owner,
        plan: 'premium',
        status: 'active',
        checkoutSessionId: session._id,
        activatedAt: now,
      });
    }

    await ctx.db.patch(session._id, {
      status: 'completed',
      providerCheckoutId: event.providerCheckoutId,
    });
    await ctx.db.insert('paymentEvents', {
      providerEventId: event.providerEventId,
      checkoutSessionId: session._id,
      type: 'checkout.completed',
      receivedAt: now,
    });
    return { duplicate: false };
  },
});
```

If the earlier create-checkout action timed out before attaching the provider ID, the verified webhook safely reconciles it. If an ID was already attached, it must match.

## Expose the webhook HTTP action

Read the body once, preserve it unchanged for verification, then pass only the normalized verified fields to the database mutation:

```ts
// pbvex/webhooks.ts
import { httpAction } from './_generated/server';
import { internal } from './_generated/api';
import { Response } from 'pbvex/server';

export const fakePay = httpAction({
  route: { method: 'POST', path: 'webhooks/fakepay' },
  handler: async (ctx, request) => {
    const signature = request.headers.get('fakepay-signature');
    if (!signature) return new Response('unauthorized', { status: 401 });

    const rawBody = await request.text();
    if (rawBody.length > 16_384) {
      return new Response('payload too large', { status: 413 });
    }

    try {
      const event = await ctx.runAction(
        internal.components.fakePay.adapter.verifyWebhook,
        { rawBody, signature },
      );
      await ctx.runMutation(internal.payments.applyCompletedCheckout, {
        providerEventId: event.providerEventId,
        checkoutSessionId: event.clientReferenceId,
        providerCheckoutId: event.providerCheckoutId,
      });
      return new Response(null, { status: 204 });
    } catch {
      return new Response('webhook rejected', { status: 401 });
    }
  },
});
```

The internal mutation's `v.id('paymentCheckoutSessions')` validator checks the verified client reference at runtime. Duplicate events receive `204`, telling FakePay to stop retrying. Keep error responses generic and never log the API key, signature, or full payment payload.

Continue with [Entitlements and production](./entitlements-and-production.md).
