# @pbvex/client

Core browser-neutral client SDK for PBVex.

## Usage

```typescript
import { Client, PBVexClient } from '@pbvex/client';

const client = new Client('http://localhost:8090');

await client.auth.collection('users').authWithPassword('me@example.com', 'password');

const result = await client.query(api.messages.list, { userId: '123' });
```

## API

- `Client` / `PBVexClient` — HTTP client for `query`, `mutation`, `action`, and realtime `watch`.
- `client.auth.collection(name)` — native PocketBase password, OTP, OAuth2, MFA, refresh, verification, password-reset, email-change, and impersonation flows.
- `client.authStore` — persistent browser auth state with injectable memory or custom storage.
- `setAuth(token | provider)` / `clearAuth()` — configure Bearer tokens per-call or from a provider.
- `FetchRealtimeTransport` — default SSE-based realtime transport with chunked parsing, envelope validation, initial query, deduped fanout, and bounded reconnect.
- `PBVexError` — structured error thrown from `StructuredError` responses.

Exported types include `FunctionReference`, `ArgsOf`, `ReturnOf`, `QueryResult`, `ConnectionState`, `Unsubscribe`, `WatchOptions`, `WatchCallbacks`, and `RealtimeTransport`.
