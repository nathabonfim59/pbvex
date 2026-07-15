[@pbvex/client](../index.md) / LocalAuthStore

# Class: LocalAuthStore\<T\>

## Extends

- [`AuthStore`](AuthStore.md)\<`T`\>

## Type Parameters

### T

`T` *extends* [`AuthRecord`](../type-aliases/AuthRecord.md) = [`AuthRecord`](../type-aliases/AuthRecord.md)

## Constructors

### Constructor

> **new LocalAuthStore**\<`T`\>(`options?`): `LocalAuthStore`\<`T`\>

#### Parameters

##### options?

[`LocalAuthStoreOptions`](../interfaces/LocalAuthStoreOptions.md) = `{}`

#### Returns

`LocalAuthStore`\<`T`\>

#### Overrides

[`AuthStore`](AuthStore.md).[`constructor`](AuthStore.md#constructor)

## Accessors

### isSuperuser

#### Get Signature

> **get** **isSuperuser**(): `boolean`

##### Returns

`boolean`

#### Inherited from

[`AuthStore`](AuthStore.md).[`isSuperuser`](AuthStore.md#issuperuser)

***

### isValid

#### Get Signature

> **get** **isValid**(): `boolean`

##### Returns

`boolean`

#### Inherited from

[`AuthStore`](AuthStore.md).[`isValid`](AuthStore.md#isvalid)

***

### record

#### Get Signature

> **get** **record**(): `T` \| `null`

##### Returns

`T` \| `null`

#### Inherited from

[`AuthStore`](AuthStore.md).[`record`](AuthStore.md#record)

***

### token

#### Get Signature

> **get** **token**(): `string`

##### Returns

`string`

#### Inherited from

[`AuthStore`](AuthStore.md).[`token`](AuthStore.md#token)

## Methods

### clear()

> **clear**(): `void`

#### Returns

`void`

#### Inherited from

[`AuthStore`](AuthStore.md).[`clear`](AuthStore.md#clear)

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

#### Inherited from

[`AuthStore`](AuthStore.md).[`onChange`](AuthStore.md#onchange)

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

#### Overrides

[`AuthStore`](AuthStore.md).[`save`](AuthStore.md#save)
