# Guides

Use these guides after the [quickstart](../quickstart.md) to build an application, connect a client, and run PBVex in production. Read [Backend primitives](../concepts/backend-primitives.md) for the difference between queries, mutations, actions, HTTP actions, scheduling, and cron jobs. For the architectural overview, read [How PBVex works](../concepts/how-it-works.md).

For feature-by-feature walkthroughs, build contacts, conversations, realtime messages, authorization, and attachments in the [Messaging app tutorial](../tutorials/messaging/), or build checkout, idempotent webhooks, and premium entitlements in the [FakePayment tutorial](../tutorials/payments/).

## Build your backend

- [Schema and database](./schema-and-database.md) — define tables, indexes, and database operations.
- [Querying data and designing indexes](./querying-and-indexes.md) — build bounded queries and efficient access paths.
- [Relationships and joins](./relationships-and-joins.md) — model typed references, hydrate related records, and choose when to denormalize.
- [Data types and validation](./data-types-and-validation.md) — validate arguments, returns, and stored values.
- [Backend functions](./functions.md) — write queries, mutations, and actions.
- [HTTP actions](./http-actions.md) — expose public HTTP endpoints.
- [Components](./components.md) — package and mount reusable backend features.
- [Project organization](./project-organization.md) — arrange modules and components while understanding generated paths and reserved files.
- [Outbound HTTP requests](./outbound-http.md) — call external APIs from actions with bounded requests and responses.

## Add platform capabilities

- [Authentication](./auth.md) — use password, OTP, OAuth2, MFA, and authenticated function calls.
- [Authorization](./authorization.md) — protect application data with ownership, membership, and role checks.
- [Application email templates](./email-templates.md) — create typed templates and send them from functions.
- [File storage](./storage.md) — upload, inspect, download, and delete files.
- [Scheduling work](./scheduling.md) — enqueue durable one-shot work for a delay or exact instant.
- [Cron jobs](./cron-jobs.md) — declare recurring application-wide schedules backed by PocketBase cron.
- [Environment variables and secrets](./environment-variables.md) — declare and provision typed configuration.

## Use cases and tutorials

- [Build an instant-messaging app](../tutorials/messaging/) — combine authentication, profiles, contacts, permissions, realtime messages, and attachments across a complete application.
- [Build a fake payment flow](../tutorials/payments/) — generate checkout URLs, process provider events idempotently, and activate premium entitlements.

## Connect an application

- [Client SDK](./client/index.md) — configure `@pbvex/client`, call functions, and subscribe to realtime updates.
- [React](./react/index.md) — use the provider and hooks in React applications.
- [Svelte](./svelte/index.md) — use the Svelte 5 client and reactive query helpers.
- [Examples](../examples/client/index.md) — start from complete client and framework slices.

## Deploy and operate

- [Deployment](./deployment.md) — build, activate, inspect, and roll back application code.
- [Self-hosting PBVex](../self-hosting.md) — install the server, access PocketBase administration, and manage persistent state.
- [Going to production](./going-to-production.md) — configure systemd, Nginx, TLS, mail, security, and backups.
- [Testing](./testing.md) — test applications, clients, and the repository.
- [Limits and boundaries](./limits.md) — understand runtime, protocol, and single-node constraints.
