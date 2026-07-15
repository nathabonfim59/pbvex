# Deployment

PBVex deployment uploads a TypeScript-derived artifact to an already running PBVex binary. It is distinct from installing or upgrading that binary.

## Managed local development

The npm `pbvex` package depends on `@pbvex/server`, which contains the
supported backend binaries. For a loopback `local` target, one command owns
the local server and deployment loop:

```bash
pbvex dev
```

It starts the matching bundled backend, persists data under
`.pbvex/dev/local/pb_data`, waits for `/api/health`, performs the initial
atomic deployment with a random process-local credential, and watches
`pbvex/**/*.ts`. That credential is accepted only for deployment routes, only
when the backend process explicitly receives the development token, and only
from a loopback request. It is not a
PocketBase superuser and cannot administer jobs or the dashboard.

Use `pbvex dev --no-backend` when another process owns the local server. Use
`pbvex dev --debug` when verbose PocketBase request and SQL logging is needed.
Managed development enables the loopback dashboard by default; use
`pbvex dev --no-admin-ui` to omit it. `pbvex serve` runs the bundled server
without the TypeScript watcher and disables the dashboard unless
`serve --admin-ui` is passed. Remote targets never cause `pbvex dev` to start
a local server and continue to require a superuser deployment token.

## Targets and credentials

Define named server targets in `pbvex/pbvex.config.ts`:

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

The config is intentionally JSON-like and side-effect-free. `metadata` is target metadata carried by configuration; it is not a mechanism for injecting runtime secrets.

For `pbvex deploy -t production`, the token resolution order is:

1. `--token`.
2. `PBVEX_PRODUCTION_TOKEN` (the target name is uppercased).
3. `PBVEX_TOKEN`.
4. `.pbvex/credentials.json`, first at `production.token`, then its top-level `token`.

Except for the scoped credential created internally by managed local
`pbvex dev`, the token must be a PocketBase superuser token. It authorizes
deployment upload, list, activation, rollback, and scheduler administration;
application requests use their own optional auth-record tokens. Keep
credentials out of `pbvex.config.ts` and source control.

## Build and activate

```bash
pbvex codegen -t production
pbvex typecheck -t production
pbvex build -t production
PBVEX_PRODUCTION_TOKEN='<superuser-token>' pbvex deploy -t production
```

`build` generates references and writes `.pbvex/dist/artifact.json` plus `build-metadata.json`. The artifact contains the v1 manifest, base64 executable bundle, lowercase SHA-256, and decoded byte count. `deploy` bundles again, uploads to `/api/pbvex/deployments`, and activates the returned ID with `{ "atomic": true }`.

Activation verifies/compiles the complete bundle and applies schema and component materialization transactionally before switching the active deployment. If it fails, the existing active deployment remains active. Keep the build artifact as a release record, but do not treat it as a secret store.

## Roll back an application release

The CLI deploy command does not expose a rollback subcommand. A superuser can roll back the current active deployment with the server API:

```bash
curl -X POST http://127.0.0.1:8090/api/pbvex/deployments/<active-id>/rollback \
  -H 'Authorization: Bearer <superuser-token>' \
  -H 'Content-Type: application/json'
```

Rollback restores the active deployment's recorded previous deployment atomically. It is not a binary downgrade and it does not restore deleted external data. If no previous deployment is recorded, the operation cannot select one for you. Back up before schema-changing releases and verify the representative call, subscription, scheduled job, and storage path after either direction of rollout.

## Application environment variables

PBVex does not manage environment-variable values as deployment target configuration. Root functions have no environment API. A component may declare a literal string with `{ type: 'value', value: '...' }` or bind a PBVex server process variable with `{ type: 'envVar', name: '...' }`; only component code receives those strings through `ctx.env`.

The artifact records the binding name, not its server value. Provision every required variable separately for each target backend and restart the process after rotation. The deployment token variables authenticate the CLI only and are not application configuration. See [Environment variables and secrets](./environment-variables.md) for the complete contract and [Components](./components.md) for component identity and isolation.

## Production rollout and recovery

Before the first production release, follow the [going to production
guide](./going-to-production.md) to install the single-application backend,
configure TLS and Nginx, harden dashboard access, and establish backups.

1. Install and health-check the binary, configure its persistent data directory, TLS/reverse proxy, canonical application URL, and backups.
2. Build and type-check the application in CI. Deploy with a short-lived superuser token supplied by secret management.
3. Run a smoke call using generated client references and check realtime, scheduler, and storage behavior relevant to the release.
4. If application activation fails, inspect the structured deployment error; the previous deployment is still active. Correct and redeploy, or roll back the currently active release if it was activated successfully but is unhealthy.
5. If a binary upgrade is unhealthy, stop it and restore the matching binary plus data/object-storage backup. Application rollback is not sufficient for an incompatible binary/data downgrade.

The [self-hosting guide](../self-hosting.md) covers binary installation, the one-application-per-server model, data, the admin dashboard, storage, upgrades, and recovery. The [release guide](../releasing.md) covers producing and verifying PBVex release artifacts.
