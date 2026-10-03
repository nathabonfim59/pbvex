# Realtime subscriptions

Use `client.watch` to open a Server-Sent Events (SSE) subscription to a query. The default transport is `FetchRealtimeTransport`, which carries every live query of a client over one session stream (`POST /api/pbvex/realtime/session`) and adds or removes queries with short control requests. Browsers allow only about six HTTP/1.1 connections per origin, so a page can watch any number of queries without starving its other requests.

## Basic watch

```ts
import { Client } from '@pbvex/client';
import { api } from '#pbvex/_generated/api';

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

Connection state is `disconnected` when no transport exists or nothing is watched. All subscriptions share the session stream, so they move through the same states together.

## Retry behavior

`FetchRealtimeTransport` reopens the session stream with exponential backoff and resubscribes every query. The defaults are:

- `maxReconnects`: 5
- `initialReconnectDelayMs`: 500
- `maxReconnectDelayMs`: 30000

Set them on the transport, not per watch: every query shares the session, so the `maxReconnects`, `initialReconnectDelayMs`, and `maxReconnectDelayMs` fields of `WatchOptions` are deprecated and ignored.

```ts
import { Client, FetchRealtimeTransport } from '@pbvex/client';

const client = new Client('http://localhost:8090', {
  realtimeTransport: new FetchRealtimeTransport({
    baseUrl: 'http://localhost:8090',
    maxReconnects: 10,
    initialReconnectDelayMs: 1000,
    maxReconnectDelayMs: 60000,
  }),
});
```

A decoded `message` resets the attempt counter. When the limit is reached, every watcher gets `Realtime reconnect limit reached` and its subscription is disposed; a later `watch` opens a new session.

## Max event handling

The SSE parser rejects oversized lines and events:

- `maxSseLineLength` and `maxSseEventDataLength` are derived from `maxReturnValueBytes + 4096`.
- The server may advertise a smaller `maxEventSize` in a `subscribe` control envelope; the parser tightens its limit but never loosens it.
- Oversized events are discarded and reported through `onError`; parsing continues.

## Control envelopes

The SSE stream carries control envelopes:

- `session` — first event of a session stream; carries the session id used by control requests.
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

`setAuth` and `clearAuth` refresh auth on live subscriptions by reopening the session stream with the new token. A session is bound to the identity that opened it, so a token change always means a new session.

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

Multiple watchers for the same `path` and canonical args share one server-side subscription on the session stream.

## Advanced: single-query streams without the SDK

Besides sessions, the server keeps a simpler endpoint where one request watches one query: `POST /api/pbvex/realtime` (and a bounded `GET` form). There is no session id, no control request, and no resubscribe step. The response is the same SSE stream a session uses, carrying only that query's events. The SDK does not use it.

It makes sense when you cannot or do not want to embed `@pbvex/client`:

- **Debugging from a terminal.** `curl` a live query and watch results change as you edit data.
- **Scripts and services in other languages.** A Python, Go, or shell process that follows one or two values needs an HTTP client and an SSE line reader, nothing more.
- **Small devices.** Firmware that displays one value (a counter, a status) can hold one plain HTTP stream.
- **Plain HTML pages.** The `GET` form works with the browser's built-in `EventSource`, for a public query on a page without a bundler.

Prefer sessions (the SDK) when a client watches more than a handful of queries, especially in a browser over HTTP/1.1, where each single-query stream takes one of the roughly six connections the browser allows per origin. Each stream also counts against the server's global and per-IP realtime connection limits (see [Limits](../limits.md)).

### Subscription id

Every request carries an `id`: the hex SHA-256 of `v1:<path>:<canonical args>`. Canonical args are the wire-encoded arguments as JSON with object keys sorted and no whitespace; a function without arguments uses `{}`. The server rejects an id that does not match the path and args.

```sh
ARGS='{"channel":"general"}'
ID=$(printf 'v1:messages:list:%s' "$ARGS" | sha256sum | cut -d' ' -f1)
```

Values without a plain JSON form (`Int64`, bytes, and so on) use the wire encoding described in [Data types and validation](../data-types-and-validation.md); for anything beyond strings, numbers, booleans, arrays, and objects, use `encodeValue` and `canonicalJson` from `@pbvex/protocol`.

### POST

```sh
curl -N http://localhost:8090/api/pbvex/realtime \
  -H 'Accept: text/event-stream' \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d "{\"id\":\"$ID\",\"path\":\"messages:list\",\"args\":$ARGS}"
```

`Authorization` is optional; without it, the query runs anonymously. The stream looks like this:

```text
data: {"data":{"id":"<id>","maxEventSize":1052672,"op":"subscribe"}}

data: {"data":{"id":"<id>","op":"message","payload":[{"_id":"...","text":"hi"}]}}

data: {"data":{"id":"<id>","op":"ping"}}
```

Each `message` payload is the full, wire-encoded query result; the server sends one only when the result changes. A payload with `"error": true` is a structured error the query raised while running. Problems found before the stream starts (an unknown function, invalid args, an id that does not match) return a JSON error with a 4xx status instead, such as `404` with `"code":"not_found"`.

### GET and EventSource

```js
const args = JSON.stringify({ channel: 'general' });
const id = await sha256Hex(`v1:messages:list:${args}`);
const url = `/api/pbvex/realtime?id=${id}&path=messages:list&args=${encodeURIComponent(args)}`;

const source = new EventSource(url);
source.onmessage = (event) => {
  const { data } = JSON.parse(event.data);
  if (data.op === 'message') render(data.payload);
};
```

`sha256Hex` stands for any SHA-256 helper, such as one built on `crypto.subtle.digest`. `EventSource` cannot send an `Authorization` header, so this form suits public queries. The server bounds the `args` query parameter, so keep arguments small.

### Reconnecting

The stream ends when a deployment is activated or rolled back, so the next stream runs against the new code and limits. A long-lived client should reconnect with backoff whenever the stream ends, and reconnect with the new token after the user's auth changes. `EventSource` reconnects on its own.
