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

### realtimePath?

> `optional` **realtimePath?**: `string`

***

### realtimeTransport?

> `optional` **realtimeTransport?**: [`RealtimeTransport`](RealtimeTransport.md)

***

### timeoutMs?

> `optional` **timeoutMs?**: `number`
