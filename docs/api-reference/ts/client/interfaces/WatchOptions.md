[@pbvex/client](../index.md) / WatchOptions

# Interface: WatchOptions\<T\>

## Extends

- [`WatchCallbacks`](WatchCallbacks.md)\<`T`\>

## Type Parameters

### T

`T`

## Properties

### initialReconnectDelayMs?

> `optional` **initialReconnectDelayMs?**: `number`

***

### maxReconnectDelayMs?

> `optional` **maxReconnectDelayMs?**: `number`

***

### maxReconnects?

> `optional` **maxReconnects?**: `number`

***

### onConnectionStateChange?

> `optional` **onConnectionStateChange?**: (`state`) => `void`

#### Parameters

##### state

[`ConnectionState`](../type-aliases/ConnectionState.md)

#### Returns

`void`

#### Inherited from

[`WatchCallbacks`](WatchCallbacks.md).[`onConnectionStateChange`](WatchCallbacks.md#onconnectionstatechange)

***

### onError?

> `optional` **onError?**: (`error`) => `void`

#### Parameters

##### error

`Error`

#### Returns

`void`

#### Inherited from

[`WatchCallbacks`](WatchCallbacks.md).[`onError`](WatchCallbacks.md#onerror)

***

### onUpdate

> **onUpdate**: (`result`) => `void`

#### Parameters

##### result

[`QueryResult`](QueryResult.md)\<`T`\>

#### Returns

`void`

#### Inherited from

[`WatchCallbacks`](WatchCallbacks.md).[`onUpdate`](WatchCallbacks.md#onupdate)
