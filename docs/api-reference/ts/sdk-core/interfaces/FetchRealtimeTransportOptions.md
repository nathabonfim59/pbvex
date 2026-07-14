[@pbvex/sdk-core](../index.md) / FetchRealtimeTransportOptions

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

### realtimePath?

> `optional` **realtimePath?**: `string`

***

### timeoutMs?

> `optional` **timeoutMs?**: `number`
