[@pbvex/client](../index.md) / ClientOptions

# Interface: ClientOptions

## Properties

### auth?

> `optional` **auth?**: `string` \| [`AuthProvider`](../type-aliases/AuthProvider.md)

***

### authStore?

> `optional` **authStore?**: [`AuthStore`](../classes/AuthStore.md)\<[`AuthRecord`](../type-aliases/AuthRecord.md)\>

***

### baseUrl?

> `optional` **baseUrl?**: `string`

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

### limits?

> `optional` **limits?**: `ClientLimits`

***

### realtimeMultiplex?

> `optional` **realtimeMultiplex?**: `boolean`

Carry all live queries over one realtime connection (default `true`).
Set `false` only for servers that predate realtime sessions: each query
then holds its own connection, and browsers allow ~6 per origin over
HTTP/1.1.

***

### realtimePath?

> `optional` **realtimePath?**: `string`

***

### realtimeTransport?

> `optional` **realtimeTransport?**: [`RealtimeTransport`](RealtimeTransport.md)

***

### timeoutMs?

> `optional` **timeoutMs?**: `number`
