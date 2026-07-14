# @pbvex/sdk-core

Core browser-neutral client SDK for PBVex.

## Usage

```typescript
import { Client, PBVexClient } from '@pbvex/sdk-core';

const client = new Client('http://localhost:8090');

client.setAuth('my-token');

const result = await client.query(api.messages.list, { userId: '123' });
```

## API

- `Client` / `PBVexClient` — HTTP client for `query`, `mutation`, `action`, and realtime `watch`.
- `setAuth(token | provider)` / `clearAuth()` — configure Bearer tokens per-call or from a provider.
- `FetchRealtimeTransport` — default SSE-based realtime transport with chunked parsing, envelope validation, initial query, deduped fanout, and bounded reconnect.
- `PBVexError` — structured error thrown from `StructuredError` responses.

Exported types include `FunctionReference`, `ArgsOf`, `ReturnOf`, `QueryResult`, `ConnectionState`, `Unsubscribe`, `WatchOptions`, `WatchCallbacks`, and `RealtimeTransport`.
