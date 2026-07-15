# Quickstart

This walkthrough creates a local PBVex backend, a TypeScript application, and a typed client call. PBVex v1 runs one backend process against one data directory.

## Prerequisites

- Node.js 18 or later with npm for the global CLI, authoring packages, and client code.
- A PBVex server binary for your operating system. Download the matching archive and `checksums.txt` from the project release, verify the archive, and extract `pbvex` (`pbvex.exe` on Windows).

## Install the agent skill (optional)

PBVex ships project-specific instructions for Codex, Claude Code, Cursor, and other Agent Skills-compatible coding agents. Install the umbrella skill in your application repository before asking an agent to build with PBVex:

```bash
npx skills add nathabonfim59/pbvex --skill pbvex
```

List or install the focused backend, functions, client, React, Svelte, components, and operations skills from the [Agent Skills guide](./getting-started/agent-skills.md). Skills guide your coding agent; they do not install the PBVex binary or npm packages used below.

Start the binary from the directory that should hold the data directory:

```bash
mkdir -p ~/pbvex-local
cd ~/pbvex-local
/path/to/pbvex serve --http 127.0.0.1:8090
```

The default data directory is `./pb_data`. In a second terminal, create the first PocketBase superuser:

```bash
cd ~/pbvex-local
/path/to/pbvex superuser create admin@example.com 'replace-this-password'
```

Get a deployment token from the running server. Keep it out of source control.

```bash
curl -sS http://127.0.0.1:8090/api/collections/_superusers/auth-with-password \
  -H 'Content-Type: application/json' \
  -d '{"identity":"admin@example.com","password":"replace-this-password"}'
```

Copy the response's `token` value. A superuser token authorizes deployment; it is not an application-user token.

## Initialize the TypeScript application

```bash
mkdir my-pbvex-app
cd my-pbvex-app
npm init --yes
npm install --global pbvex
npm install --save-dev pbvex typescript
npm install @pbvex/client
pbvex init
```

Keep the global CLI and local `pbvex` dependency on the same version. The global installation provides the direct `pbvex` command; the local dependency provides the `pbvex/server` and `pbvex/values` modules imported by application code.

`init` creates `pbvex/pbvex.config.ts`, schema and function examples, generated-file placeholders, and scripts. It refuses to overwrite existing PBVex paths unless you pass `--force`.

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

Generate references whenever schema or function exports change, then type-check and build the deployable artifact:

```bash
pbvex codegen
pbvex typecheck
pbvex build
```

The build writes `.pbvex/dist/artifact.json` and build metadata. Deploy it with the token obtained above:

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
