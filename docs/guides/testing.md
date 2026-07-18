# Testing

PBVex has strong package and backend test coverage, but it does not currently ship a dedicated application-function test runner that invokes `query` or `mutation` handlers directly. Test application behavior at the boundary that fits it: pure TypeScript unit tests, generated-type checks, and deployed integration tests.

## Recommended layers

| Layer | What to test | Existing project support |
| --- | --- | --- |
| Unit | Pure validators, mapping helpers, and code that does not need `ctx` | Use your normal TypeScript test runner. |
| Type/build | Schema, exports, generated references, and import/runtime restrictions | `pbvex typecheck` and `pbvex build --check`. |
| Package | CLI, bundler, codegen, protocol, and SDK behavior | Workspace tests use Vitest. |
| Backend | Activation, runtime/database bridge, auth, realtime, scheduler, and storage | Go tests under `backend/`. |
| Integration | Your actual functions, schema migrations, auth, and client calls | Start a disposable PBVex binary/data directory, deploy, and call it through the SDK or HTTP API. |

For a PBVex migration, test `up` by deploying over a representative copy of the previous data, then call the authenticated deployment rollback API to exercise `down` and verify the old document shape. Include cases that invoke defaults and `ctx.fail`, and test near the 10,000-document/64-MiB activation ceilings when relevant. `pbvex migrations plan` verifies structural chains only; it cannot replace activation against representative data. Test PocketBase host migrations separately on backend startup because PBVex deployment rollback does not reverse them.

## Test an application through a deployed backend

Use a temporary data directory and a test-only superuser. After deployment, exercise public functions through the same generated references used in production:

```ts
import { Client } from '@pbvex/client';
import { api } from '../pbvex/_generated/api.js';

const client = new Client(process.env.PBVEX_TEST_URL ?? 'http://127.0.0.1:8090');

await client.mutation(api.messages.send, { channel: 'test', body: 'one' });
const rows = await client.query(api.messages.list, { channel: 'test' });

if (!rows.includes('one')) throw new Error('message was not returned');
```

In a test setup, start `pbvex --dir <temporary-directory> serve --http <test-address>`, create a superuser, deploy using `PBVEX_TOKEN`, and tear down the process and directory afterward. This verifies bundling, manifest validation, activation, database behavior, and the wire protocol together. It also avoids depending on undocumented handler internals.

There is no documented public helper for constructing a function `ctx`, nor a built-in in-memory emulator. Keep domain logic in ordinary modules where it can be unit-tested, and reserve context/database assertions for deployed integration tests.

## Test realtime and clients

For core-client code, inject `fetch` or a `RealtimeTransport` into `Client` rather than using a live server for every UI test. The repository's SDK tests use mocks that synchronously emit `QueryResult` updates.

React hooks can be tested with Vitest, `renderHook`, a `PBVexProvider`, and a mock realtime transport; see [Testing React hooks](./react/testing.md). Test Svelte 5 rune utilities by mounting a small component harness with a mock `Client`, driving rune state, and asserting cleanup after unmount; see [Testing Svelte runes](./svelte/testing.md). These are client-behavior tests, not substitutes for a deployed function test.

## Repository and CI commands

Run the checks that match a change:

```bash
pnpm build
pnpm test
pbvex typecheck
pbvex build --check

cd backend
go test ./...
go vet ./...
```

For release-level verification, [the release guide](../releasing.md) also specifies formatting, race tests, packaging smoke tests, and artifact validation. The repository contains backend end-to-end tests for CLI bundles, authentication, routing, realtime, scheduler, and capabilities, but there is no single application-test command that provisions a binary and deploys an arbitrary user project automatically. Add that orchestration in your application CI when integration coverage matters.
