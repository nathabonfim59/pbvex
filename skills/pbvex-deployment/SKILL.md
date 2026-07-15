---
name: pbvex-deployment
description: Guide and configure a new PBVex deployment from initial topology decisions through backend provisioning, data directory, superuser credentials, TLS and proxy settings, canonical URL, environment bindings, storage, backups, application target configuration, first atomic deploy, smoke tests, and rollback readiness. Use when a user is setting up a local, staging, or production PBVex environment for the first time or wants an interactive deployment walkthrough.
---

# PBVex first deployment

Walk the user through one checkpoint at a time. Inspect the application repository and installed PBVex version before asking for information already available. Keep a visible checklist of completed, pending, and user-owned steps; explain each required choice briefly.

Never ask the user to paste passwords, deployment tokens, encryption keys, SMTP credentials, or provider secrets into chat. Have them place secrets in their shell, service manager, CI secret store, or restricted environment file, and verify only names/presence. Pause for approval before privileged host changes, DNS/firewall changes, certificate issuance, service restarts, or a real deployment activation.

## Discover the target

Establish these facts in small batches:

- Environment name and purpose: local, staging, or production.
- Existing backend or new host; OS/container platform; PBVex version and binary source.
- Private listener, public hostname, TLS/proxy owner, and DNS readiness.
- Dedicated data directory and local or S3-compatible object storage.
- Application repository, package manager, local `pbvex` version, and current `pbvex/pbvex.config.ts`.
- Required component environment bindings, auth methods, SMTP, rate limits, backup destination, and CI deployer.

Treat one server deployment as one application. Use exactly one PBVex process for one dedicated data directory; provision a separate process, hostname, credentials, and backup lifecycle for another application. Distinguish installing/upgrading the binary from deploying an application artifact.

## Provision the backend

For normal local development, prefer `pbvex dev`. The npm `pbvex` package
installs `@pbvex/server`; for a loopback `local` target the command selects and
starts the bundled backend, persists data in `.pbvex/dev/<target>/pb_data`,
health-checks `/api/health`, performs the first deployment with a random
loopback-only deployment credential, and watches `pbvex/**/*.ts`. It does not
create a permanent superuser. Use the dashboard's first-superuser flow only
when dashboard access is needed.

Use `pbvex serve` for a backend-only npm workflow and `pbvex dev --no-backend`
when another process owns the listener. Use `pbvex dev --debug` only when
verbose PocketBase request and SQL output is useful. A standalone GoReleaser archive is an
alternative for Node-free hosting; do not tell npm users to download it as a
prerequisite. If `@pbvex/server` or the detected platform binary is missing,
have the user reinstall `pbvex` or explicitly install the exact matching
`@pbvex/server` version.

For staging or production:

1. Select and verify the release archive and checksum for the host architecture.
2. Install the binary separately from writable state. Create a dedicated non-root service account and private data directory.
3. Create the first PocketBase superuser against the exact same `--dir` used by the service. Prefer the dashboard through an SSH tunnel when avoiding command-line password exposure; if the CLI is required, have the user enter the command in a controlled shell and never compose, echo, or retain the real password in chat or automation logs.
4. Run PBVex under the platform service manager with a stable working directory, restart policy, restricted environment file, and explicit data directory.
5. Bind the backend to a private interface. Terminate TLS at a trusted proxy or use supported PocketBase HTTPS, preserving realtime streaming and forwarded request context.
6. Set PocketBase's canonical Application URL to the external HTTP(S) origin and configure trusted client-IP proxy headers.
7. Restrict dashboard and superuser access, then configure MFA/OTP after SMTP works. Configure rate limits and test mail delivery when the application uses auth email or templates.
8. Choose storage limits/backend and establish off-host database and object-storage backups with a restore test.

Use `docs/self-hosting.md` and `docs/guides/going-to-production.md` in a PBVex source checkout for current flags and service/proxy examples. Otherwise run `pbvex --help` and `pbvex serve --help` for the installed version rather than copying version-sensitive flags from memory.

## Configure the application target

Keep the globally installed CLI and the application's local `pbvex` dependency on the same version. Define a side-effect-free target without secrets:

```ts
export default {
  project: 'my-app',
  defaultTarget: 'production',
  targets: {
    production: { url: 'https://app.example.com', metadata: {} },
  },
};
```

Use target metadata only as build metadata. It does not inject runtime configuration. Root functions cannot read server environment variables; provision component `envVar` bindings in the backend process for every target and restart after rotation.

Obtain a short-lived PocketBase superuser token locally. Prefer `PBVEX_<TARGET>_TOKEN` in the current shell or CI secret store; use gitignored `.pbvex/credentials.json` only when appropriate. Never put the token in `pbvex.config.ts`, application code, client bundles, logs, or the deployment artifact. Application record tokens cannot deploy.

## Preflight and deploy

Inspect the diff and identify schema, component mount, storage, scheduler, auth, and environment changes. Back up before the first production deployment and before schema/component changes.

Run against the selected target:

```bash
pbvex codegen -t <target>
pbvex typecheck -t <target>
pbvex build -t <target>
PBVEX_<TARGET>_TOKEN='<set-locally>' pbvex deploy -t <target>
```

Do not place a real token in a committed command or transcript. `deploy` rebuilds, uploads, validates, and atomically activates the artifact. If validation or activation fails, preserve the previous active deployment, inspect the structured error, correct it, and redeploy.

## Verify and hand off

After activation, verify the health endpoint plus every capability the release depends on:

- one generated query and mutation with the intended anonymous/authenticated identities;
- one realtime subscription through the public proxy;
- relevant scheduled/cron work;
- upload and signed download when storage is used;
- HTTP actions, outbound providers, email, and component environment bindings when applicable;
- dashboard access restrictions, logs, alerts, backup execution, and restore ownership.

Record the environment name, PBVex version, public URL, data/storage locations, service owner, secret names (never values), backup/restore procedure, smoke results, and deployment ID. Explain that application rollback uses the authenticated deployment API and does not undo external side effects, restore deleted data, or downgrade the binary. For a bad binary upgrade, restore the matching binary and data/object backup.

Finish only when the user has either completed the deployment and smoke checks or has an explicit handoff list of remaining external actions and verification commands.
