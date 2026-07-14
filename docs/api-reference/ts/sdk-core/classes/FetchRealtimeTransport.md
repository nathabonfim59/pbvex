[@pbvex/sdk-core](../index.md) / FetchRealtimeTransport

# Class: FetchRealtimeTransport

## Implements

- [`RealtimeTransport`](../interfaces/RealtimeTransport.md)

## Constructors

### Constructor

> **new FetchRealtimeTransport**(`options`): `FetchRealtimeTransport`

#### Parameters

##### options

[`FetchRealtimeTransportOptions`](../interfaces/FetchRealtimeTransportOptions.md)

#### Returns

`FetchRealtimeTransport`

## Properties

### baseUrl

> `readonly` **baseUrl**: `string`

***

### fetchFn

> `readonly` **fetchFn**: (`input`, `init?`) => `Promise`\<`Response`\>

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

> `readonly` `optional` **getAuthToken?**: () => `string` \| `Promise`\<`string` \| `undefined`\> \| `undefined`

#### Returns

`string` \| `Promise`\<`string` \| `undefined`\> \| `undefined`

***

### initialReconnectDelayMs

> `readonly` **initialReconnectDelayMs**: `number`

***

### maxFunctionArgsBytes

> `readonly` **maxFunctionArgsBytes**: `number`

***

### maxRealtimeBodyBytes

> `readonly` **maxRealtimeBodyBytes**: `number`

***

### maxReconnectDelayMs

> `readonly` **maxReconnectDelayMs**: `number`

***

### maxReconnects

> `readonly` **maxReconnects**: `number`

***

### maxResponseBodyBytes

> `readonly` **maxResponseBodyBytes**: `number`

***

### maxReturnValueBytes

> `readonly` **maxReturnValueBytes**: `number`

***

### maxSseEventDataLength

> `readonly` **maxSseEventDataLength**: `number`

***

### maxSseLineLength

> `readonly` **maxSseLineLength**: `number`

***

### realtimePath

> `readonly` **realtimePath**: `string`

***

### timeoutMs

> `readonly` **timeoutMs**: `number`

## Accessors

### connectionState

#### Get Signature

> **get** **connectionState**(): [`ConnectionState`](../type-aliases/ConnectionState.md)

##### Returns

[`ConnectionState`](../type-aliases/ConnectionState.md)

#### Implementation of

[`RealtimeTransport`](../interfaces/RealtimeTransport.md).[`connectionState`](../interfaces/RealtimeTransport.md#connectionstate)

## Methods

### close()

> **close**(): `void`

#### Returns

`void`

#### Implementation of

[`RealtimeTransport`](../interfaces/RealtimeTransport.md).[`close`](../interfaces/RealtimeTransport.md#close)

***

### computeSubscriptionId()

> **computeSubscriptionId**(`path`, `canonicalArgs`): `Promise`\<`string`\>

#### Parameters

##### path

`string`

##### canonicalArgs

`string`

#### Returns

`Promise`\<`string`\>

***

### refreshAuth()

> **refreshAuth**(): `void`

#### Returns

`void`

#### Implementation of

[`RealtimeTransport`](../interfaces/RealtimeTransport.md).[`refreshAuth`](../interfaces/RealtimeTransport.md#refreshauth)

***

### removeSubscription()

> **removeSubscription**(`subscriptionKey`): `void`

#### Parameters

##### subscriptionKey

`string`

#### Returns

`void`

***

### watch()

> **watch**\<`Args`, `Return`\>(`path`, `args`, `options`): [`Unsubscribe`](../type-aliases/Unsubscribe.md)

#### Type Parameters

##### Args

`Args`

##### Return

`Return`

#### Parameters

##### path

`string`

##### args

`Args`

##### options

[`WatchOptions`](../interfaces/WatchOptions.md)\<`Return`\>

#### Returns

[`Unsubscribe`](../type-aliases/Unsubscribe.md)

#### Implementation of

[`RealtimeTransport`](../interfaces/RealtimeTransport.md).[`watch`](../interfaces/RealtimeTransport.md#watch)
