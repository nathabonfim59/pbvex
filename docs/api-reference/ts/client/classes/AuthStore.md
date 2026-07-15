[@pbvex/client](../index.md) / AuthStore

# Class: AuthStore\<T\>

## Extended by

- [`LocalAuthStore`](LocalAuthStore.md)

## Type Parameters

### T

`T` *extends* [`AuthRecord`](../type-aliases/AuthRecord.md) = [`AuthRecord`](../type-aliases/AuthRecord.md)

## Constructors

### Constructor

> **new AuthStore**\<`T`\>(): `AuthStore`\<`T`\>

#### Returns

`AuthStore`\<`T`\>

## Accessors

### isSuperuser

#### Get Signature

> **get** **isSuperuser**(): `boolean`

##### Returns

`boolean`

***

### isValid

#### Get Signature

> **get** **isValid**(): `boolean`

##### Returns

`boolean`

***

### record

#### Get Signature

> **get** **record**(): `T` \| `null`

##### Returns

`T` \| `null`

***

### token

#### Get Signature

> **get** **token**(): `string`

##### Returns

`string`

## Methods

### clear()

> **clear**(): `void`

#### Returns

`void`

***

### onChange()

> **onChange**(`listener`, `fireImmediately?`): () => `void`

#### Parameters

##### listener

[`AuthChangeListener`](../type-aliases/AuthChangeListener.md)\<`T`\>

##### fireImmediately?

`boolean` = `false`

#### Returns

() => `void`

***

### save()

> **save**(`token`, `record?`): `void`

#### Parameters

##### token

`string`

##### record?

`T` \| `null`

#### Returns

`void`
