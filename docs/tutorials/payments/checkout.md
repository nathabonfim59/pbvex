# Generate checkout

PBVex is the merchant application in this flow. It records a local purchase intent, calls an external provider's sandbox API, and returns the provider-hosted checkout URL. PBVex does not render or complete the provider's checkout.

## Isolate provider configuration

Put FakePay's API key and base URL in a component environment declaration:

```ts
// pbvex/integrations/fakePay/component.ts
import { defineComponent, defineComponentFns } from 'pbvex/server';

export const fakePay = defineComponent({
  modulePaths: ['adapter.ts'],
  env: {
    API_KEY: { type: 'envVar', name: 'FAKEPAY_API_KEY' },
    API_BASE_URL: {
      type: 'value',
      value: 'https://sandbox-api.fakepay.example',
    },
    APP_ORIGIN: {
      type: 'value',
      value: 'https://app.example.com',
    },
  },
});

export const fakePayFns = defineComponentFns(fakePay);
```

Mount it in the application:

```ts
// pbvex/app.ts
import { defineApp, mount } from 'pbvex/server';
import { fakePay } from './integrations/fakePay/component';

export default defineApp({
  components: [mount(fakePay, 'fakePay')],
});
```

Provision `FAKEPAY_API_KEY` in the PBVex server process, then run `pbvex codegen`. The key is available only as `ctx.env.API_KEY` inside this component.

## Create checkout through the external API

The component action uses [outbound HTTP](../../guides/outbound-http.md) to call FakePay. The provider receives the local session ID as its client reference and returns its own checkout ID and hosted URL:

```ts
// pbvex/integrations/fakePay/adapter.ts
import { v } from 'pbvex/values';
import { fakePayFns } from './component';

export const createCheckout = fakePayFns.internalAction({
  args: { clientReferenceId: v.string() },
  returns: v.object({
    providerCheckoutId: v.string(),
    checkoutUrl: v.string(),
  }),
  handler: async (ctx, { clientReferenceId }) => {
    const response = await ctx.http.send({
      url: `${ctx.env.API_BASE_URL}/v1/checkouts`,
      method: 'POST',
      headers: {
        authorization: `Bearer ${ctx.env.API_KEY}`,
        'content-type': 'application/json',
        'idempotency-key': `checkout:${clientReferenceId}`,
      },
      body: JSON.stringify({
        price: 'premium_monthly',
        quantity: 1,
        clientReferenceId,
        successUrl: `${ctx.env.APP_ORIGIN}/billing/success`,
        cancelUrl: `${ctx.env.APP_ORIGIN}/billing/cancelled`,
      }),
      timeoutMs: 10_000,
    });

    const body = response.json;
    if (
      response.statusCode !== 201 ||
      !body || typeof body !== 'object' ||
      typeof (body as any).id !== 'string' ||
      typeof (body as any).checkoutUrl !== 'string'
    ) {
      throw new Error(`FakePay checkout failed (${response.statusCode})`);
    }

    return {
      providerCheckoutId: (body as any).id,
      checkoutUrl: (body as any).checkoutUrl,
    };
  },
});
```

The API hostname is fixed by server-controlled configuration. Never let a client choose the outbound URL. The idempotency key lets the application safely reconcile a provider success after a timeout.

## Coordinate local and provider state

Actions cannot write the database directly, so use small internal mutations around the provider call:

```ts
// pbvex/payments.ts
import { action, internalMutation } from './_generated/server';
import { internal } from './_generated/api';
import { v } from 'pbvex/values';
import { requireIdentity } from './lib/identity';

export const createPendingCheckout = internalMutation({
  args: { owner: v.string() },
  returns: v.id('paymentCheckoutSessions'),
  handler: (ctx, { owner }) => ctx.db.insert('paymentCheckoutSessions', {
    owner,
    plan: 'premium',
    status: 'pending',
  }),
});

export const attachProviderCheckout = internalMutation({
  args: {
    checkoutSessionId: v.id('paymentCheckoutSessions'),
    providerCheckoutId: v.string(),
  },
  returns: v.null(),
  handler: async (ctx, args) => {
    const session = await ctx.db.get(args.checkoutSessionId);
    if (!session || session.status !== 'pending') {
      throw new Error('checkout session is not pending');
    }
    await ctx.db.patch(session._id, {
      providerCheckoutId: args.providerCheckoutId,
    });
    return null;
  },
});

export const generateCheckout = action({
  args: {},
  returns: v.object({ checkoutUrl: v.string() }),
  handler: async (ctx) => {
    const user = await requireIdentity(ctx.auth);
    const checkoutSessionId = await ctx.runMutation(
      internal.payments.createPendingCheckout,
      { owner: user.tokenIdentifier },
    );

    const provider = await ctx.runAction(
      internal.components.fakePay.adapter.createCheckout,
      { clientReferenceId: checkoutSessionId },
    );

    await ctx.runMutation(internal.payments.attachProviderCheckout, {
      checkoutSessionId,
      providerCheckoutId: provider.providerCheckoutId,
    });
    return { checkoutUrl: provider.checkoutUrl };
  },
});
```

The provider call and database mutations cannot share one transaction. The stable idempotency key handles the important failure case: FakePay may create checkout even if PBVex times out before storing its ID. A production implementation should add a retry/reconciliation action that repeats the same provider request and attaches the returned checkout ID.

## Redirect from the client

```ts
const { checkoutUrl } = await client.action(
  api.payments.generateCheckout,
);

window.location.assign(checkoutUrl);
```

Allowlist FakePay's checkout origin before redirecting if the provider can return arbitrary URLs. Reaching the success page does not activate premium; it only tells the browser that the hosted checkout finished. The signed provider webhook is authoritative.

Continue with [Process payment webhooks](./webhooks.md).
