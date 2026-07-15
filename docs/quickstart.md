# Quickstart

This walkthrough creates a local PBVex backend, a TypeScript application, and a typed client call. PBVex v1 runs one backend process against one data directory.

## Prerequisites

- Node.js 18 or later with npm for the CLI, bundled local backend, authoring packages, and client code.

## Install the agent skill (optional)

PBVex ships project-specific instructions for Codex, Claude Code, Cursor, and other Agent Skills-compatible coding agents. Install the umbrella skill in your application repository before asking an agent to build with PBVex:

```bash
npx skills add nathabonfim59/pbvex --skill pbvex
```

List or install the focused backend, functions, client, React, Svelte, components, and operations skills from the [Agent Skills guide](./getting-started/agent-skills.md). Skills guide your coding agent; they do not install the npm packages used below.

## Initialize the TypeScript application

```bash
mkdir my-pbvex-app
cd my-pbvex-app
npm init --yes
npm install --save-dev pbvex
npm install @pbvex/client
npx pbvex init
```

The local `pbvex` package supplies the CLI, the `pbvex/server` and `pbvex/values` authoring modules, and the bundled `@pbvex/server` backend dependency. A matching global CLI remains optional.

`init` creates `pbvex/pbvex.config.ts`, schema and function examples, generated-file placeholders, and scripts. In an interactive terminal it asks whether to add scripts and defaults to yes; use `--no-scripts` to opt out. It refuses to overwrite existing PBVex paths unless you pass `--force`.

Set the local target in `pbvex/pbvex.config.ts` if needed:

```ts
export default {
  project: 'my-pbvex-app',
  defaultTarget: 'local',
  targets: {
    local: { url: 'http://127.0.0.1:8090', metadata: {} },
  },
};
```

Start the managed local backend and deploy the scaffold:

```bash
npm run pbvex:dev
```

For a loopback `local` target, this command starts the bundled Go backend,
stores persistent development state under `.pbvex/dev/local/pb_data`, waits
for its health endpoint, performs the first atomic deployment with an
in-memory loopback-only development credential, and watches
`pbvex/**/*.ts`. It does not create a permanent PocketBase superuser. Open
`http://127.0.0.1:8090/_/` and use PocketBase's first-superuser flow only if
you need the dashboard. Managed development enables that loopback dashboard by
default; pass `pbvex dev --no-admin-ui` to disable it.

## Define a schema and functions

Replace the scaffold with a small messages table and two public functions.

The `list` export below is a **query**, so it can only read data and clients may subscribe to its result. The `send` export is a **mutation**, so its database write runs transactionally. See [Backend primitives](./concepts/backend-primitives.md) for queries, mutations, actions, HTTP actions, scheduled jobs, and cron jobs.

```ts
// pbvex/schema.ts
import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  messages: defineTable({
    channel: v.string(),
    body: v.string(),
  }).index('by_channel', ['channel']),
});
```

```ts
// pbvex/messages.ts
import { mutation, query } from './_generated/server';
import { v } from 'pbvex/values';

export const list = query({
  args: { channel: v.string() },
  returns: v.array(v.string()),
  handler: async (ctx, args) => {
    const rows = await ctx.db
      .query('messages')
      .withIndex('by_channel', (q) => q.eq('channel', args.channel))
      .collect();
    return rows.map((row) => row.body);
  },
});

export const send = mutation({
  args: { channel: v.string(), body: v.string() },
  returns: v.id('messages'),
  handler: (ctx, args) => ctx.db.insert('messages', args),
});
```

## Generate, build, and deploy

`pbvex dev` generates references, builds, and deploys whenever schema or
function exports change. The individual commands remain available for CI and
manual deployment:

```bash
pbvex codegen
pbvex typecheck
pbvex build
```

The build writes `.pbvex/dist/artifact.json` and build metadata. Deploying to
an independently managed backend requires a PocketBase superuser token:

```bash
PBVEX_TOKEN='<superuser-token>' pbvex deploy --url http://127.0.0.1:8090
```

The CLI uploads the artifact and requests atomic activation. A failed activation leaves the previous deployment active.

## Connect a client and verify it

Use generated references rather than handwritten function paths:

```ts
// src/client.ts
import { Client } from '@pbvex/client';
import { api } from '../pbvex/_generated/api.js';

const client = new Client('http://127.0.0.1:8090');

await client.mutation(api.messages.send, {
  channel: 'general',
  body: 'Hello PBVex',
});

const messages = await client.query(api.messages.list, { channel: 'general' });
console.log(messages); // ['Hello PBVex']
```

Run that file through your application's normal TypeScript build/runtime. You can also confirm the server is reachable with:

```bash
curl -f http://127.0.0.1:8090/api/health
```

## Next steps

- Build a complete application with the [Messaging app tutorial](./tutorials/messaging/), including authentication, contacts, conversation authorization, realtime messages, and attachments.
- Add checkout, webhooks, and premium entitlements with the standalone [FakePayment tutorial](./tutorials/payments/).
- Learn access paths and pagination in [Querying data and designing indexes](./guides/querying-and-indexes.md), model references with [Relationships and joins](./guides/relationships-and-joins.md), then review every supported validator in [Data types and validation](./guides/data-types-and-validation.md).
- Add authentication with [Authentication](./guides/auth.md), and realtime UI updates with the [client guides](./guides/client/index.md).
- Configure application secrets through explicit component bindings with [Environment variables and secrets](./guides/environment-variables.md).
- For a deployed server, the one-application deployment model, admin dashboard, backups, TLS, and storage configuration, read the [self-hosting guide](./self-hosting.md).
