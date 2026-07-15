---
name: pbvex-operations
description: Plan or execute safe PBVex deployment, testing, documentation verification, release, security, limit, backup, and single-binary operations. Use for CI/CD, production configuration, incident recovery, capacity boundaries, credentials, documentation maintenance, or release verification.
---

# PBVex operations

Application release automation should install the `pbvex` CLI globally with npm, keep its version aligned with the application's local `pbvex` dependency, and invoke `pbvex` directly. The PBVex source monorepo itself continues to use pnpm workspaces for contributor checks.

Separate application deployment from binary operation. The PBVex binary embeds PocketBase and needs no Node.js or checkout at runtime; run one process for one data directory. Do not turn it into a multi-node/shared-data-directory deployment or add a Node sidecar requirement.

```bash
pbvex codegen -t <target>
pbvex typecheck -t <target>
pbvex build -t <target>
PBVEX_<TARGET>_TOKEN='<superuser-token>' pbvex deploy -t <target>
curl -f http://127.0.0.1:8090/api/health
```

Keep targets in side-effect-free `pbvex/pbvex.config.ts`. Supply a short-lived PocketBase superuser deployment token through secret management, environment, or gitignored credentials—not source, browser code, a function bundle, or a client. Application record tokens are unrelated and cannot deploy.

Application environment bindings are component-only and resolve from the PBVex backend process, not from target metadata or deployment token variables. Provision required values independently for each target with the service manager, container platform, or secret manager. After rotation, restart the process so it receives the new environment and run a smoke function that never returns or logs secret material. See `docs/guides/environment-variables.md`.

## Activation and recovery

Deployment activation validates/compiles the whole artifact, applies compatible schema/component work transactionally, then atomically changes the active deployment. Failure retains the prior release. Keep `.pbvex/dist/artifact.json` as a release record, not a secret store. Test a representative generated call, realtime subscription, scheduled job, and storage path after release.

Back up the data directory before binary upgrades/downgrades or schema-changing releases; include external object storage with database state. Application rollback is an authenticated deployment API operation and is not a binary/data rollback. For a bad binary upgrade, restore the matching binary and backup.

## Boundaries, security, and validation

Configure TLS/reverse proxy and PocketBase’s canonical application URL. Treat HTTP actions/webhooks as hostile input; verify signatures, authenticate/authorize, and avoid leaking secrets. Do not weaken validators, deployment checks, CORS controls, signed URL policy, or storage limits.

Check documented runtime limits before capacity-dependent designs: bounded bundle/value/request sizes, single-node realtime/SSE constraints, scheduler concurrency/retries, storage TTL/size/type limits, schema/index limits, and Goja (not Node) runtime restrictions. Use pagination and bounded payloads rather than relying on undocumented thresholds.

Use proportional checks:

```bash
pnpm build && pnpm test
pbvex build --check
(cd backend && go test ./... && go vet ./...)
pnpm docs:verify
```

Never manually edit generated `pbvex/_generated/` or `docs/api-reference/`; run their owning generators. Consult `docs/self-hosting.md`, `docs/guides/deployment.md`, `docs/guides/limits.md`, and `docs/releasing.md` before changing operational behavior.

## Maintain repository documentation

Update authored guides, examples/tutorials, and the owning package README whenever public behavior changes. Regenerate API pages with `pnpm docs:api`; do not patch generated Markdown. Run `pnpm docs:verify` to check source/API staleness, internal links, and the VitePress production build. Keep `docs/self-hosting.md` as the operator reference and `docs/guides/going-to-production.md` as the concrete production walkthrough.
