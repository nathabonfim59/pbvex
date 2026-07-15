# PBVex

PBVex provides a Convex-shaped TypeScript authoring and client experience on a
single-node backend built with PocketBase as a Go framework.

## What is included

- `backend/cmd/pbvex`: one self-contained Go executable with the PocketBase
  admin UI, deployment runtime, database bridge, realtime subscriptions,
  durable and recurring cron scheduling, storage, authentication, application email templates, inbound HTTP actions, bounded outbound HTTP,
  and components.
- `pbvex`: the project CLI and server-side authoring package.
- `@pbvex/server`: the complete set of bundled backend binaries used by the
  CLI, also installable directly for server-only npm usage.
- `@pbvex/client`: typed calls, authentication, and realtime subscriptions.
- `@pbvex/react`: React provider and hooks.
- `@pbvex/svelte`: Svelte 5 context helpers and rune-backed query state.
- `@pbvex/protocol`: the shared deployment and wire-value contract.

The published authoring package exposes `pbvex/server`, `pbvex/values`, and
`pbvex/component`; the last subpath carries the typed component definition and
mount helpers used by build tooling and advanced authoring integrations.

PBVex v1 is designed for one backend process. It does not provide multi-node
consensus or distributed scheduler coordination.

## Create and run an application

```bash
npm install --save-dev pbvex
npm install @pbvex/client
npx pbvex init
npm run pbvex:dev
```

`pbvex dev` starts the backend bundled by `@pbvex/server`, keeps local data in
`.pbvex/dev/local/pb_data`, performs the first deployment without creating a
permanent superuser, and watches `pbvex/**/*.ts` for rebuilds. The dashboard is
available at `http://127.0.0.1:8090/_/`; create a superuser there only when you
need dashboard access.

Run only the bundled backend when needed:

```bash
npx pbvex serve --http 127.0.0.1:8090
```

See [the self-hosting guide](docs/self-hosting.md) for token creation, persistence,
standalone release binaries, reverse proxy, storage, upgrades, and recovery.

Actions can send deployment-owned templates from `pbvex/emails/` through the
PocketBase mailer; see [Application email templates](docs/guides/email-templates.md).
They can also call external providers through `ctx.http.send`; see
[Outbound HTTP requests](docs/guides/outbound-http.md).

The local package provides both the CLI and imports such as `pbvex/server` and
`pbvex/values`. A global installation remains optional.

`pbvex init` refuses to replace existing scaffold paths. Use `--force` only
when intentionally replacing them.

Client applications use the generated `pbvex/_generated/api.ts` references:

```ts
import { Client } from '@pbvex/client';
import { api } from './pbvex/_generated/api.js';

const client = new Client('http://127.0.0.1:8090');
const messages = await client.query(api.messages.get, { channel: 'general' });
```

## Development

```bash
pnpm install
pnpm build
pnpm test
pnpm pack:smoke
cd backend && go test ./...
```

The release workflow also runs Go vet, formatting checks, full backend race
tests, GoReleaser archive validation, and a no-Node binary smoke test. See the
[release guide](docs/releasing.md) for npm setup, GitHub hardening, tagging, and
artifact verification.

## Documentation

- [Authoring CLI and server APIs](packages/pbvex/README.md)
- [Core SDK](packages/client/README.md)
- [React SDK](packages/react/README.md)
- [Svelte SDK](packages/svelte/README.md)
- [Protocol v1 ADR](docs/adr/001-protocol-v1.md)
- [Self-hosting guide](docs/self-hosting.md)
- [Release and registry setup](docs/releasing.md)
