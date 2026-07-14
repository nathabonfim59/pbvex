# PBVex

PBVex provides a Convex-shaped TypeScript authoring and client experience on a
single-node backend built with PocketBase as a Go framework.

## What is included

- `backend/cmd/pbvex`: one self-contained Go executable with the PocketBase
  admin UI, deployment runtime, database bridge, realtime subscriptions,
  scheduler, storage, authentication, HTTP actions, and components.
- `pbvex`: the project CLI and server-side authoring package.
- `@pbvex/sdk-core`: typed calls, authentication, and realtime subscriptions.
- `@pbvex/sdk-react`: React provider and hooks.
- `@pbvex/sdk-svelte`: Svelte context helpers and reactive stores.
- `@pbvex/protocol`: the shared deployment and wire-value contract.

The published authoring package exposes `pbvex/server`, `pbvex/values`, and
`pbvex/component`; the last subpath carries the typed component definition and
mount helpers used by build tooling and advanced authoring integrations.

PBVex v1 is designed for one backend process. It does not provide multi-node
consensus or distributed scheduler coordination.

## Run the backend

```bash
cd backend
go build -o pbvex ./cmd/pbvex
./pbvex serve --http 127.0.0.1:8090
```

The executable defaults to `./pb_data` in its working directory and does not
need Node.js, pnpm, a repository checkout, or a sidecar at runtime. Release
archives and checksums are produced by GoReleaser.

Create the first PocketBase superuser before deploying application code:

```bash
./pbvex superuser create admin@example.com 'replace-this-password'
```

See [the operator guide](docs/operator.md) for token creation, persistence,
reverse proxy, storage, upgrades, and recovery.

## Create an application

```bash
pnpm add -D pbvex typescript
pnpm exec pbvex init
pnpm exec pbvex codegen
pnpm exec pbvex build
PBVEX_TOKEN='<superuser-token>' pnpm exec pbvex deploy
```

`pbvex init` refuses to replace existing scaffold paths. Use `--force` only
when intentionally replacing them.

Client applications use the generated `pbvex/_generated/api.ts` references:

```ts
import { Client } from '@pbvex/sdk-core';
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
- [Core SDK](packages/sdk-core/README.md)
- [React SDK](packages/sdk-react/README.md)
- [Svelte SDK](packages/sdk-svelte/README.md)
- [Protocol v1 ADR](docs/adr/001-protocol-v1.md)
- [Backend operator guide](docs/operator.md)
- [Release and registry setup](docs/releasing.md)
