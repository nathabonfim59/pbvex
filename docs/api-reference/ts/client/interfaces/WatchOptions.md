[@pbvex/client](../index.md) / WatchOptions

# Interface: WatchOptions\<T\>

## Extends

- [`WatchCallbacks`](WatchCallbacks.md)\<`T`\>

## Type Parameters

### T

`T`

## Properties

### ~~initialReconnectDelayMs?~~

> `optional` **initialReconnectDelayMs?**: `number`

#### Deprecated

Ignored: every query shares the transport's session stream and its reconnect policy.

***

### ~~maxReconnectDelayMs?~~

> `optional` **maxReconnectDelayMs?**: `number`

#### Deprecated

Ignored: every query shares the transport's session stream and its reconnect policy.

***

### ~~maxReconnects?~~

> `optional` **maxReconnects?**: `number`

#### Deprecated

Ignored: every query shares the transport's session stream and its reconnect policy.

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
