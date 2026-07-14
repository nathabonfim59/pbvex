# Install the client SDK

The client SDK is published as `@pbvex/sdk-core`. It is browser-neutral and only requires a `fetch` implementation.

## Packages

```bash
npm install @pbvex/sdk-core
```

```bash
pnpm add @pbvex/sdk-core
```

```bash
yarn add @pbvex/sdk-core
```

## Requirements

- Node.js 18+ or a modern browser.
- A `fetch` implementation. In Node 18+ `globalThis.fetch` is used by default. In older environments, pass `fetch` in `ClientOptions`.
- `crypto.subtle` is required for realtime subscription id hashing.
- TypeScript 5.0+ with `moduleResolution: 'NodeNext'` is recommended for generated `FunctionReference` types.

## Generate API references

In a PBVex project, run codegen to produce typed references:

```bash
pnpm exec pbvex codegen
```

This writes `pbvex/_generated/api.ts`:

```ts
import { api } from '../pbvex/_generated/api.js';
import { Client } from '@pbvex/sdk-core';

const client = new Client('http://localhost:8090');
const messages = await client.query(api.messages.list, { channel: 'general' });
```

## Peer relationship

`@pbvex/sdk-core` depends on `@pbvex/protocol` for the shared wire contract. You do not normally need to install `@pbvex/protocol` directly unless you are implementing a custom transport or authoring tooling.
