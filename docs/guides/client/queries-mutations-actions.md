# Queries, mutations, and actions

`Client` exposes three call methods: `query`, `mutation`, and `action`. Each method accepts either a generated `FunctionReference` or a string path, plus optional arguments and `CallOptions`.

## FunctionReference calls

```ts
import { Client } from '@pbvex/client';
import { api } from '../pbvex/_generated/api.js';

const client = new Client('http://localhost:8090');

const messages = await client.query(api.messages.list, { channel: 'general' });
const sent = await client.mutation(api.messages.send, { body: 'hello' });
const notified = await client.action(api.messages.notify, { messageId: sent.id });
```

The type of `api.messages.list` is `FunctionReference<'query', { channel: string }, Message[], 'public'>`. The client infers the args and return type.

## String path calls

For dynamic or pre-typed usage, pass a string path:

```ts
const result = await client.query('messages:list', { channel: 'general' });
const created = await client.mutation('messages:send', { body: 'hello' });
const notified = await client.action('messages:notify', { messageId: created.id });
```

String calls are not type-checked by the generated references.

## Optional arguments

Generated references with `void`, `undefined`, `{}`, or all-optional object args can be called without arguments:

```ts
// health:ping has no args
const ok = await client.query(api.health.ping);

// messages:recent has { channel?: string }
const recent = await client.query(api.messages.recent);
const recentGeneral = await client.query(api.messages.recent, { channel: 'general' });
```

`__noArgs: true` on a reference disambiguates the single-slot case: the sole argument is treated as `CallOptions`, not args.

```ts
// health:ping is declared with __noArgs
await client.query(api.health.ping, { timeoutMs: 1000 });
```

## CallOptions

```ts
interface CallOptions {
  timeoutMs?: number;
  auth?: string | AuthProvider;
  signal?: AbortSignal;
  /** @deprecated Use `signal`. */
  abort?: AbortSignal;
}
```

Examples:

```ts
const controller = new AbortController();
const promise = client.query(api.messages.list, { channel: 'general' }, {
  timeoutMs: 5000,
  signal: controller.signal,
});

// cancel the request
controller.abort();
```

## Return values

Successful responses contain a `result` field. The client decodes the wire value codec and returns the typed value. `undefined` return values are normalized to `null`.

## Request/response limits

- The call body is `POST` to `/api/pbvex/call` with `Content-Type: application/json`.
- Args are encoded with the PBVex wire codec: `bigint`, `ArrayBuffer`, `Id`, arrays, and plain objects are supported.
- If the encoded args exceed `maxFunctionArgsBytes`, the call throws before the network request.
- If the response body exceeds `maxReturnValueBytes + 4096`, it is rejected.

## Cancellation and timeout

- `signal` is honored per request. Aborting throws an `AbortError` or `DOMException` when available.
- `timeoutMs` is per request, including auth resolution and body reading.
- If the timeout fires, the promise rejects with `Request timeout after ${timeoutMs}ms`.

```ts
const controller = new AbortController();
setTimeout(() => controller.abort(), 1000);

try {
  await client.query(api.messages.list, { channel: 'general' }, { signal: controller.signal });
} catch (error) {
  if (error instanceof Error && error.name === 'AbortError') {
    console.log('aborted');
  }
}
```
