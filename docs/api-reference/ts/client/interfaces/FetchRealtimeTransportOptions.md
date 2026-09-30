[@pbvex/client](../index.md) / FetchRealtimeTransportOptions

# Interface: FetchRealtimeTransportOptions

## Properties

### baseUrl

> **baseUrl**: `string` \| `URL`

***

### fetch?

> `optional` **fetch?**: (`input`, `init?`) => `Promise`\<`Response`\>

[MDN Reference](https://developer.mozilla.org/docs/Web/API/Window/fetch)

#### Parameters

##### input

`URL` \| `RequestInfo`

##### init?

`RequestInit`

#### Returns

`Promise`\<`Response`\>

***

### getAuthToken?

> `optional` **getAuthToken?**: () => `string` \| `Promise`\<`string` \| `undefined`\> \| `undefined`

#### Returns

`string` \| `Promise`\<`string` \| `undefined`\> \| `undefined`

***

### initialReconnectDelayMs?

> `optional` **initialReconnectDelayMs?**: `number`

***

### limits?

> `optional` **limits?**: `ClientLimits`

***

### maxReconnectDelayMs?

> `optional` **maxReconnectDelayMs?**: `number`

***

### maxReconnects?

> `optional` **maxReconnects?**: `number`

***

### multiplex?

> `optional` **multiplex?**: `boolean`

Carry all subscriptions over one session stream (default `true`). With
`false` each distinct query holds its own connection, which browsers cap
at ~6 per origin on HTTP/1.1; use it only against servers older than the
session endpoint. Per-watch reconnect options apply only when `false`;
the session uses the transport's.

***

### realtimePath?

> `optional` **realtimePath?**: `string`

***

### timeoutMs?

> `optional` **timeoutMs?**: `number`
