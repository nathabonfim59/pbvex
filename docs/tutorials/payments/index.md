# Build a fake payment flow

This tutorial integrates a PBVex application with **FakePay**, a fictional external provider whose API resembles a hosted Stripe-style checkout. An authenticated user asks the application to create checkout, PBVex calls FakePay's sandbox API, and the browser visits the provider-hosted URL. FakePay later calls the application's webhook, which activates a premium entitlement transactionally.

FakePay stands in for a provider's sandbox SDK/API; it is not implemented inside this project. Replace its example base URL, request fields, and verification endpoint with the contract documented by the provider you actually use.

## What you will build

```text
authenticated app                 external FakePay                PBVex webhook
      |                                    |                             |
      | payments.generateCheckout          |                             |
      |<----- provider checkout URL --------|                             |
      |------------ open hosted URL ------>|                             |
      |--------- complete checkout --------|                             |
      |                                    |-- checkout.completed ------>|
      |                                    |                             | deduplicate event
      |                                    |                             | activate premium
      |<---------------- result ------------|<------------ 204 -----------|
```

The browser result is presentation only. Premium becomes active only when the webhook transaction commits.

## Tutorial map

1. [Payment data model](./data-model.md) — model checkout sessions, provider events, and entitlements.
2. [Generate checkout](./checkout.md) — create local intent, call the provider API, and return its hosted checkout URL.
3. [Process payment webhooks](./webhooks.md) — validate, deduplicate, and apply completion events transactionally.
4. [Entitlements and production](./entitlements-and-production.md) — gate premium features and identify what must change for a real provider.

## Prerequisites

Complete the [Quickstart](../../quickstart.md) and [Authentication guide](../../guides/auth.md). The examples use a `requireIdentity` helper that returns `ctx.auth.getUserIdentity()` or throws when there is no signed-in application user.

The finished backend adds these files:

```text
pbvex/
├── schema.ts
├── payments.ts
├── webhooks.ts
├── app.ts
├── integrations/
│   └── fakePay/
│       ├── component.ts
│       └── adapter.ts
└── lib/
    ├── identity.ts
    └── billing.ts
```

This tutorial is application-independent. Its final chapter uses premium file attachments as a concrete example and links to the [messaging tutorial](../messaging/) if you want the complete feature. Provider credentials live in a component environment binding, and only that component can call FakePay's authenticated API.

Start with the [payment data model](./data-model.md).
