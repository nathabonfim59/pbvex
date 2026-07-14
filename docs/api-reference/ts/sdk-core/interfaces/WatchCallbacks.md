[@pbvex/sdk-core](../index.md) / WatchCallbacks

# Interface: WatchCallbacks\<T\>

## Extended by

- [`WatchOptions`](WatchOptions.md)

## Type Parameters

### T

`T`

## Properties

### onConnectionStateChange?

> `optional` **onConnectionStateChange?**: (`state`) => `void`

#### Parameters

##### state

[`ConnectionState`](../type-aliases/ConnectionState.md)

#### Returns

`void`

***

### onError?

> `optional` **onError?**: (`error`) => `void`

#### Parameters

##### error

`Error`

#### Returns

`void`

***

### onUpdate

> **onUpdate**: (`result`) => `void`

#### Parameters

##### result

[`QueryResult`](QueryResult.md)\<`T`\>

#### Returns

`void`
