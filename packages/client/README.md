# @pbvex/client

Core browser-neutral client SDK for PBVex.

## Usage

```typescript
import { Client, PBVexClient } from '@pbvex/client';
import { api } from './pbvex/_generated/api.js';

const client = new Client('http://localhost:8090');
const password = 'correct horse battery staple';

await client.auth.collection('users').create({
  email: 'me@example.com',
  password,
  passwordConfirm: password,
});
await client.auth.collection('users').authWithPassword('me@example.com', password);

const result = await client.query(api.messages.list, { userId: '123' });
```

## API

- `Client` / `PBVexClient` — HTTP client for `query`, `mutation`, `action`, and realtime `watch`.
- `client.auth.collection(name)` — native PocketBase record creation, password, OTP, OAuth2, MFA, refresh, verification, password-reset, email-change, and impersonation flows.
- `client.authStore` — persistent browser auth state with injectable memory or custom storage.
- `setAuth(token | provider)` / `clearAuth()` — configure Bearer tokens per-call or from a provider.
- `FetchRealtimeTransport` — default SSE-based realtime transport with chunked parsing, envelope validation, initial streamed result, deduped fanout, and bounded reconnect.
- `PBVexError` — structured error thrown from `StructuredError` responses.

Exported types include `FunctionReference`, `ArgsOf`, `ReturnOf`, `QueryResult`, `ConnectionState`, `Unsubscribe`, `WatchOptions`, `WatchCallbacks`, and `RealtimeTransport`.
