[@pbvex/sdk-core](../index.md) / RealtimeTransport

# Interface: RealtimeTransport

## Properties

### connectionState

> `readonly` **connectionState**: [`ConnectionState`](../type-aliases/ConnectionState.md)

***

### refreshAuth?

> `optional` **refreshAuth?**: () => `void`

#### Returns

`void`

## Methods

### close()

> **close**(): `void`

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

[`WatchOptions`](WatchOptions.md)\<`Return`\>

#### Returns

[`Unsubscribe`](../type-aliases/Unsubscribe.md)
