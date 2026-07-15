[@pbvex/client](../index.md) / AuthClient

# Class: AuthClient

## Constructors

### Constructor

> **new AuthClient**(`request`, `store`, `oauth`): `AuthClient`

#### Parameters

##### request

[`AuthRequest`](../type-aliases/AuthRequest.md)

##### store

[`AuthStore`](AuthStore.md)

##### oauth

(`collection`, `options`) => `Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<[`AuthRecord`](../type-aliases/AuthRecord.md)\>\>

#### Returns

`AuthClient`

## Methods

### collection()

> **collection**\<`T`\>(`name?`): [`AuthCollection`](AuthCollection.md)\<`T`\>

#### Type Parameters

##### T

`T` *extends* [`AuthRecord`](../type-aliases/AuthRecord.md) = [`AuthRecord`](../type-aliases/AuthRecord.md)

#### Parameters

##### name?

`string` = `'users'`

#### Returns

[`AuthCollection`](AuthCollection.md)\<`T`\>
