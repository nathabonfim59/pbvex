# Realtime subscriptions

Use `client.watch` to open a Server-Sent Events (SSE) subscription to a query. The default transport is `FetchRealtimeTransport`.

## Basic watch

```ts
import { Client } from '@pbvex/client';
import { api } from '../pbvex/_generated/api.js';

const client = new Client('http://localhost:8090');

const unsubscribe = client.watch(
  api.messages.list,
  { channel: 'general' },
  {
    onUpdate: (result) => {
      if (result.isLoading) {
        console.log('loading');
        return;
      }
      if (result.error) {
        console.error('watch error', result.error);
        return;
      }
      console.log('messages', result.data);
    },
    onError: (error) => {
      console.error('connection error', error);
    },
    onConnectionStateChange: (state) => {
      console.log('state', state);
    },
  },
);

// later
unsubscribe();
client.close();
```

`onUpdate` receives `QueryResult<T>`: `{ data: T | undefined; error: Error | null; isLoading: boolean }`.

## What causes an update

PBVex subscriptions watch a query result, not an individual table or record. The server runs the query once when the subscription starts. After any successful record create, update, or delete, it conservatively reruns every active subscribed query.

The server coalesces bursts of invalidations while a query is already running and compares the canonical result with the last value it sent. `onUpdate` receives another message only when the result changed.

This also covers related records fetched manually inside the query:

```ts
export const listContacts = query({
  handler: async (ctx) => {
    const contacts = await ctx.db
      .query('contacts')
      .withIndex('by_name')
      .take(50);

    return Promise.all(
      contacts.map(async (contact) => ({
        contact,
        company: contact.companyId
          ? await ctx.db.get(contact.companyId)
          : null,
      })),
    );
  },
});
```

If a returned contact or company changes, the subscribed query reruns and emits the new assembled result. An unrelated record change also causes reevaluation, but emits nothing when the result remains identical. PBVex does not yet track table- or record-level query dependencies, so keep realtime queries indexed and bounded. See [Relationships and joins](../relationships-and-joins.md) for server-side hydration guidance.

## Connection state

`ConnectionState` is one of `connecting`, `connected`, `reconnecting`, or `disconnected`.

```ts
const state = client.connectionState;
```

Connection state is `disconnected` when no transport exists. With active subscriptions, the transport reports the aggregate state of all subscriptions: `connected` if any are connected, `connecting` if any are connecting, `reconnecting` if any are reconnecting, otherwise `disconnected`.

## Retry behavior

`FetchRealtimeTransport` uses exponential backoff with these defaults:

- `maxReconnects`: 5
- `initialReconnectDelayMs`: 500
- `maxReconnectDelayMs`: 30000

Per-subscription overrides can be passed in `WatchOptions`:

```ts
client.watch(
  api.messages.list,
  { channel: 'general' },
  {
    onUpdate: (result) => {},
    maxReconnects: 10,
    initialReconnectDelayMs: 1000,
    maxReconnectDelayMs: 60000,
  },
);
```

Reconnection options are owned by the first watcher that creates a subscription. Subsequent watchers on the same subscription share the same connection.

## Max event handling

The SSE parser rejects oversized lines and events:

- `maxSseLineLength` and `maxSseEventDataLength` are derived from `maxReturnValueBytes + 4096`.
- The server may advertise a smaller `maxEventSize` in a `subscribe` control envelope; the parser tightens its limit but never loosens it.
- Oversized events are discarded and reported through `onError`; parsing continues.

## Control envelopes

The SSE stream carries control envelopes:

- `subscribe` — subscription acknowledged; may carry `maxEventSize`.
- `ping` / `pong` — keepalive.
- `unsubscribe` — cleanup.
- `message` — carries the decoded payload.

`subscribe`, `ping`, `pong`, and `unsubscribe` do not reset the reconnect counter; only a decoded `message` does.

## Cleanup

- `unsubscribe()` removes the watcher. The last watcher on a subscription disposes the connection.
- `client.close()` closes all subscriptions and prevents new ones.
- Unmounting a component in React or Svelte should call the returned `Unsubscribe` function.

## Auth refresh

`setAuth` and `clearAuth` refresh auth on live subscriptions by reconnecting the SSE stream with the new token.

```ts
client.setAuth('new-token');
```

`refreshAuth` is also exposed on `RealtimeTransport` implementations and is called automatically by the client.

## Custom transport

`ClientOptions.realtimeTransport` accepts a `RealtimeTransport` implementation:

```ts
interface RealtimeTransport {
  readonly connectionState: ConnectionState;
  refreshAuth?: () => void;
  watch<Args, Return>(path: string, args: Args, options: WatchOptions<Return>): Unsubscribe;
  close(): void;
}
```

`FetchRealtimeTransport` is exported for extension or standalone use.

## Deduplication

Multiple watchers for the same `path` and canonical args share a single SSE connection and a single `Subscription`.
