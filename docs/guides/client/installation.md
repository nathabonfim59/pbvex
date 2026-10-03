# Install the client SDK

The client SDK is published as `@pbvex/client`. It is browser-neutral and only requires a `fetch` implementation.

## Install

```bash
npm install --global pbvex
npm install --save-dev pbvex
npm install @pbvex/client
```

The CLI is installed globally so `pbvex codegen` is available directly. Keep its version aligned with the local `pbvex` authoring dependency, which supplies the server and validator imports used by a PBVex application.

## Requirements

- Node.js 18+ or a modern browser.
- A `fetch` implementation. In Node 18+ `globalThis.fetch` is used by default. In older environments, pass `fetch` in `ClientOptions`.
- `crypto.subtle` is required for realtime subscription id hashing.
- TypeScript 5.0+ with `moduleResolution: 'NodeNext'` is recommended for generated `FunctionReference` types.

## Generate API references

In a PBVex project, run codegen to produce typed references:

```bash
pbvex codegen
```

This writes `pbvex/_generated/api.ts`:

```ts
import { api } from '#pbvex/_generated/api';
import { Client } from '@pbvex/client';

const client = new Client('http://localhost:8090');
const messages = await client.query(api.messages.list, { channel: 'general' });
```

## Import generated references

`pbvex init` adds a Node subpath import to `package.json`:

```json
{ "imports": { "#pbvex/*": "./pbvex/*.ts" } }
```

Import generated code with `#pbvex/_generated/api`. It works in app, server, and test code under Vite, Vitest, esbuild, and TypeScript with `moduleResolution: "bundler"`, `node16`, or `nodenext`. For an existing project, add the same entry by hand; keep the `.ts` suffix, because an extensionless target does not resolve.

Do not import `pbvex/_generated/api`. The npm package is also named `pbvex`, so the bare specifier resolves to `node_modules/pbvex`, which does not export `./_generated/*`. Type checking can still pass while the bundler fails with `"./_generated/api" is not exported under the conditions ...`.

## Peer relationship

`@pbvex/client` depends on `@pbvex/protocol` for the shared wire contract. You do not normally need to install `@pbvex/protocol` directly unless you are implementing a custom transport or authoring tooling.
