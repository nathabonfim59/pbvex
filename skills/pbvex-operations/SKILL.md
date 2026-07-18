---
name: pbvex-operations
description: Plan or execute safe PBVex deployment, testing, documentation verification, release, security, limit, backup, and single-binary operations. Use for CI/CD, production configuration, incident recovery, capacity boundaries, credentials, documentation maintenance, or release verification.
---

# PBVex operations

Application release automation should install the `pbvex` CLI with npm and
keep its version aligned with the application's local dependency. The package
has an exact dependency on `@pbvex/server`; `pbvex serve` and managed local
`pbvex dev` select its bundled platform binary. The PBVex source monorepo
itself continues to use pnpm workspaces for contributor checks.

Separate application deployment from binary operation. The PBVex binary embeds PocketBase and needs no Node.js or checkout at runtime; run one process for one data directory. Do not turn it into a multi-node/shared-data-directory deployment or add a Node sidecar requirement.

The six published packages move in lockstep: `@pbvex/protocol`, `@pbvex/server`, `pbvex`, `@pbvex/client`, `@pbvex/react`, and `@pbvex/svelte`. Release validation must build/stage every server binary before ordered publication. Standalone GitHub archives remain the Node-free distribution.

```bash
TARGET=staging
npx pbvex migrations plan -t "$TARGET"
npx pbvex codegen -t "$TARGET"
npx pbvex typecheck -t "$TARGET"
npx pbvex build -t "$TARGET"
PBVEX_STAGING_TOKEN='secret-from-your-manager' npx pbvex deploy -t "$TARGET"
curl -f 'https://pbvex.internal.example/api/health'
```

Keep targets in side-effect-free `pbvex/pbvex.config.ts`. Supply a short-lived PocketBase superuser deployment token through secret management, environment, or gitignored credentials—not source, browser code, a function bundle, or a client. Application record tokens are unrelated and cannot deploy.

Application environment bindings are component-only and resolve from the PBVex backend process, not from target metadata or deployment token variables. Provision required values independently for each target with the service manager, container platform, or secret manager. After rotation, restart the process so it receives the new environment and run a smoke function that never returns or logs secret material. See `docs/guides/environment-variables.md`.

## Activation and recovery

Deployment activation validates and compiles the whole artifact before opening the database transaction. That transaction runs bundled `pbvex/migrations/*.ts` `up` handlers, applies schema/component work, records migration history, and changes the active deployment. Failure retains the prior release and documents. Application rollback runs required `down` handlers in reverse order before restoring the previous deployment; a failed down leaves the current release active. Keep `.pbvex/dist/artifact.json` as a release record, not a secret store. Test both migration directions against representative backup data plus a generated call, realtime subscription, scheduled job, and storage path.

`pbvex migrations plan` is a structural active-to-local comparison, not a data scan or row/byte estimate; use `--active-artifact path/to/artifact.json` for offline review. Activation enforces non-configurable hard ceilings of 10,000 processed documents and 64 MiB encoded work and returns structured warnings at 80% utilization. The current deploy CLI prints only the deployment ID, so API-level automation must inspect activation warnings explicitly. There is no force bypass or maintenance mode. Back up and reduce/re-stage work that cannot fit.

Keep PocketBase host migrations operationally separate. Create them with a concrete name such as `pbvex migrations pocketbase create add_users_auth`, review both `up` and `down`, run `pbvex typecheck`, back up, install the directory beside the process, and restart so PocketBase tracks/runs them before serving. The npm wrappers accept `--pocketbaseMigrationsDir`; the direct backend uses `--migrationsDir`. PBVex deployment rollback never reverses host migrations.

Back up before binary upgrades/downgrades or schema-changing releases. Never copy live SQLite files: use PocketBase backup or stop the process. Capture the data directory, signing/settings state, and external object storage consistently; preserve the settings-encryption key separately in protected secret/recovery storage. Restore into an isolated directory with the matching binary and run call/realtime/job/storage smoke tests.

The npm CLI has no deployment-list, rollback, log, or job-administration subcommands. Use superuser-authenticated APIs deliberately: identify the active ID from `GET /api/pbvex/deployments`, retain activation/release records for context, and roll back with `POST /api/pbvex/deployments/{activeId}/rollback`; the server selects its recorded rollback target. Inspect jobs with `GET /api/pbvex/jobs` or `/{id}`, cancel with `POST /{id}/cancel`, and retry eligible idempotent work with `POST /{id}/retry`. Inspect/trigger recurring definitions through PocketBase crons.

For incidents, check service/process logs, `/api/health`, TLS/proxy and SSE buffering, structured error/request ID, active deployment, jobs, environment bindings, then database/object consistency. Roll back an application deployment only after confirming IDs and backup readiness; restore matching binary/data for binary or state corruption. Do not leave verbose development logging enabled in production.

## Boundaries, security, and validation

Configure TLS/reverse proxy and PocketBase’s canonical application URL. Treat HTTP actions/webhooks as hostile input; verify signatures, authenticate/authorize, and avoid leaking secrets. Do not weaken validators, deployment checks, CORS controls, storage URL policy, or storage limits. Public storage URLs are bearer URLs: align CDN query/cache behavior and cache TTL with deletion expectations.

Check documented runtime limits before capacity-dependent designs: bounded bundle/value/request sizes, single-node realtime/SSE constraints, scheduler concurrency/retries, storage TTL/size/type and image decode/variant limits, schema/index limits, and Goja (not Node) runtime restrictions. Back up database metadata and local/S3 objects together. Use `pbvex-storage` for storage-specific release and incident checks.

Use proportional checks while iterating. Before a repository PR, follow the contributor gates:

```bash
pnpm lint && pnpm build && pnpm test && pnpm pack:smoke
git diff --check
(cd backend && test -z "$(gofmt -l .)" && go vet ./... && go test -count=1 ./... && CGO_ENABLED=1 go test -race -count=1 ./... && go build ./cmd/pbvex)
pnpm docs:verify
```

Never manually edit generated `pbvex/_generated/` or `docs/api-reference/`; run their owning generators. Consult `docs/self-hosting.md`, `docs/guides/deployment.md`, `docs/guides/limits.md`, and `docs/releasing.md` before changing operational behavior.

Maintainer releases follow `docs/releasing.md`: run `./scripts/release-validate.sh` for disposable validation, then create a fresh GoReleaser snapshot before `node scripts/stage-server-binaries.mjs`, and run `VALIDATE_TAG_TEST=1 ./scripts/validate-tag.sh`. Use a signed immutable version tag and protected OIDC publication. Publish in dependency order, verify GitHub checksums/attestations and npm provenance, then run the versioned CLI in a clean consumer. A partial publication requires a new patch version—never move a tag or republish an immutable version.

## Maintain repository documentation

Update authored guides, examples/tutorials, and the owning package README whenever public behavior changes. Regenerate API pages with `pnpm docs:api`; do not patch generated Markdown. Run `pnpm docs:verify` to check source/API staleness, internal links, and the VitePress production build. Keep `docs/self-hosting.md` as the operator reference and `docs/guides/going-to-production.md` as the concrete production walkthrough.
