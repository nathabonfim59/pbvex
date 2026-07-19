# Migrations

PBVex migrations are the default way to transform documents when a
`pbvex/schema.ts` change is not compatible with existing data. They are typed
TypeScript modules under `pbvex/migrations/*.ts`, are bundled into the PBVex
deployment artifact, and run as part of deployment activation.

Direct PocketBase migrations are a separate, advanced host mechanism. Use them
only for PocketBase state outside the PBVex schema, such as auth collections,
auth rules, and custom auth fields.

## Plan a PBVex schema change

Compare the local candidate artifact with the active deployment before writing
a migration:

```bash
pbvex migrations plan
```

The plan reports source and target deployment/schema hashes, structural table,
field, and index changes, and migration definitions whose schema hashes form a
chain from the active schema to the local schema. It does not query documents,
run migration handlers, or estimate affected rows or encoded bytes. Treat it as
a structural plan, not a capacity preflight.

By default the command reads the active deployment from the selected target.
For an offline or immutable release review, provide a previously built,
validated deployment artifact as the source:

```bash
pbvex migrations plan --active-artifact ./releases/previous-artifact.json
```

The same `--active-artifact <path>` option is available to `migrations create`.
Without it, creation tries the selected target and remains usable if that target
is offline, but the generated file then warns that its `from` validator must be
reviewed against the real prior schema.

## Create a PBVex migration

Change `pbvex/schema.ts` to the desired target schema, then scaffold a migration
for the affected table:

```bash
pbvex migrations create add_account_status --table accounts
```

The command writes a timestamped TypeScript file under `pbvex/migrations/`. It
derives `from` from the active deployment when available and `to` from the local
schema. Review both validators and replace the scaffolded casts with real
transformations.

```ts
// pbvex/migrations/1784371200_add_account_status.ts
import { defineMigration } from 'pbvex/server';
import { v } from 'pbvex/values';

const from = v.object({
  name: v.string(),
});

const to = v.object({
  name: v.string(),
  status: v.defaulted(
    v.union(v.literal('active'), v.literal('disabled')),
    'active',
  ),
});

export default defineMigration({
  id: '1784371200_add_account_status',
  table: 'accounts',
  mode: 'transactional',
  from,
  to,
  up: (oldDoc, ctx) => {
    const name = oldDoc.name.trim();
    if (!name) ctx.fail('Account name cannot be empty');
    return { name }; // Target normalization persists the default status.
  },
  down: (newDoc) => ({
    name: newDoc.name,
  }),
});
```

`from` and `to` must be object validators for one root `pbvex/schema.ts` table.
PBVex migrations do not target component tables, PocketBase collections, or
multiple tables. Input documents include typed read-only `_id` and
`_creationTime`; output must contain only user fields and cannot replace either
system field.

Both `up` and `down` are required. They are synchronous, deterministic document
transformations. Their pure context contains only `migrationId`, the stable
`activationTime`, and `fail(message)`. It has no database, network, storage,
email, scheduler, environment, or arbitrary side-effect capability. Keep shared
helpers pure and within the migration module tree.

Changing a persisted field from `v.string()` to `v.image()` requires a document migration that validates each existing canonical storage ID. This syntax check does not verify object existence, image bytes, ownership, or upload provenance, and the type change does not reprocess old generic uploads or invent an image policy for them. Only files uploaded through a schema-bound image upload URL have trusted image metadata and predefined thumbnail variants. PBVex v1 has no storage reprocessing/backfill API; use an application-managed reupload for older objects when variants are required.

## Activation and rollback lifecycle

`pbvex build`, `pbvex deploy`, and managed `pbvex dev` discover migration
definitions under `pbvex/migrations/` and include their descriptors and code in
the deployment artifact. During activation, the backend:

1. Verifies the candidate bundle, migration definitions, schema hashes, and
   migration history.
2. Resolves the migration chain from the active table schema to the candidate
   table schema.
3. Runs each `up` transformation, applies validator defaults, validates the
   target document, rebuilds order/index projections, materializes the schema,
   records migration history, and switches the active deployment in one
   database transaction.
4. Leaves the previous deployment and all documents unchanged if any step
   fails.

The backend stores each migration ID with its checksum and source/target schema
hashes. Never edit or reuse an applied migration ID: deployment is rejected if
history contains that ID with different content. Add a new migration that
continues the schema-hash chain instead.

Rolling back an active PBVex deployment runs the applicable `down` handlers in
reverse order, validates and materializes the previous schema, records the down
history, and switches deployments in one transaction. A failed `down` leaves
the current deployment and documents unchanged. Keep old deployment artifacts
and test both directions against production-shaped backup data.

## Transactional limits

All schema activation work, including compatible default/index materialization
without an explicit migration, shares fixed hard limits of 10,000 processed
documents and 64 MiB of encoded migration work. Encoded work charges source,
handler output, normalized documents, and retained order/index projections; it
is not the database size or the artifact size.

Activation preflights row counts and source bytes before writing, then enforces
the complete counters while the transaction runs. Crossing either hard limit
aborts and rolls back activation. At 80 percent utilization, activation returns
a structured `transactional_migration_utilization` warning with row and byte
counters. There is no application flag to raise or bypass these limits, and
maintenance/resumable migration mode is not implemented. Reduce the migration
or dataset, split the release where schema chains permit it, or transform the
data through a separately reviewed operational process before activation.

Because `pbvex migrations plan` is structural-only, it deliberately prints no
invented row or byte estimates. Capacity testing requires deploying to a copy of
representative data and observing the activation result.

## Advanced: PocketBase host migrations

PocketBase host migrations have a different command, directory, runtime, and
lifecycle:

```bash
pbvex migrations pocketbase create create_users_auth
```

The nested command creates a JavaScript migration under
`pbvex/pocketbaseMigrations/`, a dedicated `tsconfig.json`, and version-matched
declarations at `pbvex/_generated/pocketbase.d.ts`:

```js
/// <reference path="../_generated/pocketbase.d.ts" />

migrate((app) => {
  const collection = new Collection({
    type: 'auth',
    name: 'users',
    createRule: '',
  });
  return app.save(collection);
}, (app) => {
  return app.delete(app.findCollectionByNameOrId('users'));
});
```

Do not edit the generated declaration file. `pbvex typecheck` checks the host
migration project separately. These files run in PocketBase's JavaScript VM,
not Node.js or the PBVex function runtime, so use declared PocketBase globals
and do not import application runtime code.

Host migration files are not included in `.pbvex/dist/artifact.json`. Managed
`pbvex dev` and `pbvex serve` load `pbvex/pocketbaseMigrations/` at backend
startup; pending filenames are tracked in PocketBase's `_migrations` table and
run before requests are served. Adding a file to an already running process
requires a restart.

Use another host migration directory only as an explicit advanced override:

```bash
pbvex dev --pocketbaseMigrationsDir ./custom/migrations
pbvex serve --pocketbaseMigrationsDir ./custom/migrations
```

There is no automatic `pb_migrations/` fallback. When invoking the backend
binary directly, use its `--migrationsDir <path>` flag and the intended
`--dir <data-path>`; `--pocketbaseMigrationsDir` belongs to the npm CLI wrapper.
The backend also exposes PocketBase's `--automigrate` behavior.

Do not define the same table or collection in both migration systems. Back up
before host schema changes, deploy host files before restarting, and review
their real down callbacks. PBVex deployment rollback runs PBVex `down` handlers
only; it never rolls back PocketBase host migrations. Host rollback remains a
separate PocketBase/operator workflow.
