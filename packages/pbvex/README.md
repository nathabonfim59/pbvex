# pbvex

The PBVex CLI and TypeScript server-authoring package.

## Install and initialize

```bash
npm install --global pbvex
npm install --save-dev pbvex typescript
pbvex init
```

Keep the global CLI and local `pbvex` dependency on the same version. The CLI
is global for direct command access; the local package provides the
`pbvex/server`, `pbvex/values`, and `pbvex/component` imports used by the app.

`init` creates `pbvex/pbvex.config.ts`, a schema, example functions, and
generated file placeholders. It minimally merges required scripts and
dependencies into an existing `package.json`, preserves an existing
`tsconfig.json`, and appends missing PBVex entries to `.gitignore`. It preflights
PBVex-owned scaffold paths and refuses to overwrite them; `pbvex init --force`
explicitly replaces only those managed scaffold files.

## Commands

- `pbvex init`: create a project scaffold.
- `pbvex codegen`: generate `pbvex/_generated/{api,dataModel,server}.ts`.
- `pbvex typecheck`: regenerate types and run `tsc --noEmit`.
- `pbvex build`: write `.pbvex/dist/artifact.json` and build metadata.
- `pbvex build --check`: validate without writing deployment output.
- `pbvex deploy`: build, upload, and atomically activate a deployment.
- `pbvex dev`: watch `pbvex/**/*.ts`, regenerate, and redeploy.

## Configuration and credentials

`pbvex/pbvex.config.ts` is a JSON-like, side-effect-free module:

```ts
export default {
  project: 'my-app',
  defaultTarget: 'local',
  targets: {
    local: { url: 'http://127.0.0.1:8090', metadata: {} },
    production: { url: 'https://app.example.com', metadata: {} },
  },
};
```

Deployment token resolution order is:

1. `--token`.
2. `PBVEX_<TARGET>_TOKEN`.
3. `PBVEX_TOKEN`.
4. `.pbvex/credentials.json` at `<target>.token`, then top-level `token`.

For example:

```json
{
  "local": { "token": "..." },
  "production": { "token": "..." }
}
```

Deployment endpoints require a PocketBase superuser token. Application calls
may be anonymous or carry an application auth-record token.

## Authoring

```ts
import { mutation, query } from 'pbvex/server';
import { v } from 'pbvex/values';

export const list = query({
  args: { channel: v.string() },
  returns: v.array(v.string()),
  handler: async (ctx, args) => {
    const messages = await ctx.db
      .query('messages')
      .filter((q) => q.eq(q.field('channel'), args.channel))
      .collect();
    return messages.map((message) => message.body);
  },
});

export const send = mutation({
  args: { channel: v.string(), body: v.string() },
  returns: v.id('messages'),
  handler: async (ctx, args) => ctx.db.insert('messages', args),
});
```

The package supports queries, mutations, actions, internal functions, HTTP
actions, bounded outbound HTTP, database indexes and pagination, authentication, scheduling, storage,
and component definitions. Generated references distinguish public/internal
visibility and whether arguments may be omitted.

Readable millisecond constants are available for one-shot scheduling:

```ts
import { DAY_MS, MINUTE_MS } from 'pbvex/server';

await ctx.scheduler.runAfter(5 * MINUTE_MS, internal.reminders.deliver, args);
await ctx.scheduler.runAfter(3 * DAY_MS, internal.trials.expire, args);
```

Recurring jobs use PocketBase cron expressions in `pbvex/crons.ts`:

```ts
import { cronJobs } from 'pbvex/server';
import { internal } from './_generated/api';

const crons = cronJobs();
crons.cron('nightly-cleanup', '0 2 * * *', internal.maintenance.cleanup);
export default crons;
```

Cron targets and arguments remain type-safe generated references. Each cron
tick enqueues a durable PBVex scheduler job.

Component primitives are exported from `pbvex/server` for function modules and
from the dedicated `pbvex/component` subpath for tooling that only needs the
component definition types and builders.

Validators include `v.string`, `v.number`, `v.float64`, `v.int64`,
`v.boolean`, `v.id`, `v.literal`, `v.object`, `v.array`, `v.record`, `v.union`,
`v.optional`, `v.defaulted`, `v.bytes`, `v.any`, and `v.null`. `v.delayed` is a
construction-time helper and cannot be serialized into a deployable descriptor.

## Imports and runtime boundary

Function modules may import:

- `pbvex/server` and `pbvex/values`;
- relative TypeScript modules within the project.

Node built-ins, arbitrary npm packages, CommonJS `require`, dynamic imports,
and asset imports are rejected. Deployed functions execute inside the Go
binary's Goja sandbox, not a Node.js process.

## Deployment artifact

`.pbvex/dist/artifact.json` is the exact `DeploymentUploadRequest` sent to
`POST /api/pbvex/deployments`:

```json
{
  "manifest": {
    "protocolVersion": "v1",
    "deploymentId": "...",
    "functions": [],
    "schema": { "tables": [] }
  },
  "bundle": "<base64 executable JavaScript>",
  "sha256": "<lowercase SHA-256>",
  "size": 1234
}
```

After upload, the CLI calls
`POST /api/pbvex/deployments/{id}/activate` with `{ "atomic": true }`.
