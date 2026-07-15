# Organize a PBVex project

PBVex discovers TypeScript modules recursively under `pbvex/`, so application functions do not have to live in one flat directory. Organize them by domain, feature, or integration in the way that keeps ownership clear.

```text
pbvex/
├── schema.ts
├── app.ts
├── billing/
│   ├── checkouts.ts
│   └── entitlements.ts
├── messaging/
│   ├── conversations.ts
│   └── messages.ts
├── integrations/
│   └── stripe/
│       ├── component.ts
│       └── adapter.ts
├── crons.ts
├── emails/
│   └── welcome/
└── _generated/
```

Folders are organizational for ordinary root functions. For components, the component definition, relative module paths, and mount name additionally define isolation and generated namespaces.

## Function paths become generated references

For ordinary query, mutation, and action modules, the path below `pbvex/` becomes the generated API path and the export name becomes its final segment:

```ts
// pbvex/billing/checkouts.ts
export const generate = action({ /* ... */ });
```

```ts
await client.action(api.billing.checkouts.generate, {});
```

An internal export from `pbvex/messaging/messages.ts` appears under `internal.messaging.messages`. Moving or renaming a module therefore changes the generated reference used by callers even if its handler code is unchanged. Run `pbvex codegen` after reorganizing files and update every typed caller.

Use lowercase or camel-case path segments that are clear when read as an API. Feature names such as `billing`, `messaging`, and `accounts` generally communicate intent better than generic folders such as `utils` or `misc`.

## Components can live outside `components/`

`pbvex/components/` is a convention, not a required source directory. A provider adapter can live under `pbvex/integrations/stripe/`:

```ts
// pbvex/integrations/stripe/component.ts
import { defineComponent, defineComponentFns } from 'pbvex/server';

export const stripe = defineComponent({
  modulePaths: ['adapter.ts'],
  env: {
    API_KEY: { type: 'envVar', name: 'STRIPE_API_KEY' },
  },
});

export const stripeFns = defineComponentFns(stripe);
```

```ts
// pbvex/integrations/stripe/adapter.ts
import { stripeFns } from './component';

export const createCheckout = stripeFns.internalAction({ /* ... */ });
```

Mounting determines the generated component namespace:

```ts
// pbvex/app.ts
export default defineApp({
  components: [mount(stripe, 'payments')],
});
```

The action is then referenced as:

```ts
internal.components.payments.adapter.createCheckout
```

The `components` segment indicates a mounted component function; it does not require a physical `pbvex/components/` source folder. The mount name also participates in the component's data namespace, so renaming `payments` is not merely cosmetic. See [Components](./components.md) before changing an existing mount.

Component implementation modules must remain within the directory rooted at their exported component definition and must be included by its relative `modulePaths`. When `modulePaths` is omitted, PBVex infers the component's function modules from that directory.

## Conventional and reserved paths

Most `.ts` modules are discovered recursively, but these paths have platform meaning:

| Path | Meaning |
| --- | --- |
| `pbvex/pbvex.config.ts` | CLI project and deployment-target configuration; excluded from the deployed function bundle |
| `pbvex/schema.ts` | Conventional location for the root schema |
| `pbvex/app.ts` | Conventional location for component mounts |
| `pbvex/crons.ts` | Required location for the single default recurring-job definition |
| `pbvex/emails/` | Required root for discovered application email templates |
| `pbvex/_generated/` | Generated data model and function references; excluded from discovery and never edited manually |

Files ending in `.test.ts` and `node_modules` directories are also excluded from deployed module discovery. Application modules may import `pbvex/server`, `pbvex/values`, and relative TypeScript modules accepted by the bundler; arbitrary runtime package and Node built-in imports are not available.

Although schema and app definitions are discovered by their exported values, keeping them at `schema.ts` and `app.ts` makes projects predictable for people, examples, and tooling.

## Suggested layouts

Small applications usually benefit from a flat start:

```text
pbvex/
├── schema.ts
├── tasks.ts
└── users.ts
```

Split by feature when each domain has several modules:

```text
pbvex/
├── schema.ts
├── contacts/
│   ├── queries.ts
│   └── mutations.ts
└── conversations/
    ├── messages.ts
    └── membership.ts
```

Use a component when code needs an isolated schema, mount arguments, explicit environment bindings, reusable packaging, or multiple isolated mounts. A folder alone does not create any of those boundaries.

For example, payment application state can remain in root `billing/` modules while the credential-bearing external adapter lives in `integrations/stripe/` as a component. See [Outbound HTTP requests](./outbound-http.md) and the [FakePayment tutorial](../tutorials/payments/) for that complete pattern.
