# Components

A component packages a schema, optional mount arguments/environment bindings, and the functions that use them. `defineComponentFns` binds component arguments and environment to function context. An app mounts components by name.

```ts
// pbvex/components/counter/component.ts
import { defineComponent, defineComponentFns, defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export const counter = defineComponent({
  modulePaths: ['functions.ts'],
  args: v.object({ label: v.string(), start: v.defaulted(v.number(), 0) }),
  env: { WEBHOOK_SECRET: { type: 'envVar', name: 'COUNTER_WEBHOOK_SECRET' } },
  schema: defineSchema({ counters: defineTable({ value: v.number() }) }),
});

export const fns = defineComponentFns(counter);
```

```ts
// pbvex/components/counter/functions.ts
import { fns } from './component';

export const readLabel = fns.query({
  handler: async (ctx) => ({ label: ctx.args.label, secretPresent: ctx.env.WEBHOOK_SECRET.length > 0 }),
});
```

```ts
// pbvex/app.ts
import { defineApp, mount } from 'pbvex/server';
import { counter } from './components/counter/component';

export default defineApp({
  components: [mount(counter, 'primary', { args: { label: 'Primary' } })],
});
```

`args` uses a validator. Defaulted/optional fields can be omitted at mount time, while required fields cannot. `env` values are strings at runtime: `{ type: 'value', value: '...' }` binds a literal and `{ type: 'envVar', name: '...' }` binds an environment variable. `dependencies` declares component dependencies. If `modulePaths` is omitted, the build discovers modules under the component’s directory; specify it when the component needs an explicit module set.

## References and namespaces

Run `pbvex codegen` after changes. Component functions are emitted in generated `api.components…` / `internal.components…` namespaces based on their module paths. Use those generated references for typed client and nested calls; do not recreate function paths by string.

Each mount path derives a stable namespace. Mounting the same definition twice creates isolated tables and IDs; an ID from one mount is invalid in another. Component contexts expose `ctx.args` and `ctx.env` in addition to the ordinary query/mutation/action capabilities. `defineComponentFns` provides query, mutation, action and internal variants; it deliberately has no HTTP-action factory.

## Lifecycle and compatibility

Component definitions are content-addressed from their declared modules, schema, arguments, environment declarations, dependencies, and bundle. Deployment activation authenticates mount arguments, creates/migrates owned tables and indexes transactionally, and fails without replacing the active deployment if validation or migration fails.

Keeping the same mount path across a compatible component upgrade preserves its namespace/data. Renaming a mount selects a different namespace. Removing a mount or table leaves owned data dormant so rollback or a later remount at the same canonical path can restore it; it is not automatically deleted or adopted by another component.

The graph is bounded (at most 1,024 component definitions and dependency depth 32) and must be acyclic. Components do not provide cross-mount database access, arbitrary runtime module loading, component HTTP routes, or a general data-export/migration API. Treat component schema and argument changes as deployment compatibility changes and test upgrades against a backup.
