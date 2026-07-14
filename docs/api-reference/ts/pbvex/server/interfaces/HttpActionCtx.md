[pbvex](../../index.md) / [server](../index.md) / HttpActionCtx

# Interface: HttpActionCtx\<DataModel\>

## Extends

- [`ActionCtx`](ActionCtx.md)\<`DataModel`\>

## Type Parameters

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md) = [`GenericDataModel`](../type-aliases/GenericDataModel.md)

## Properties

### auth

> **auth**: [`AuthContext`](AuthContext.md)

#### Inherited from

[`ActionCtx`](ActionCtx.md).[`auth`](ActionCtx.md#auth)

***

### db?

> `optional` **db?**: `undefined`

#### Inherited from

[`ActionCtx`](ActionCtx.md).[`db`](ActionCtx.md#db)

***

### http?

> `optional` **http?**: [`HttpContext`](HttpContext.md)

#### Inherited from

[`ActionCtx`](ActionCtx.md).[`http`](ActionCtx.md#http)

***

### scheduler

> **scheduler**: [`SchedulerContext`](SchedulerContext.md)

#### Inherited from

[`ActionCtx`](ActionCtx.md).[`scheduler`](ActionCtx.md#scheduler)

***

### storage

> **storage**: [`StorageContext`](StorageContext.md)

#### Inherited from

[`ActionCtx`](ActionCtx.md).[`storage`](ActionCtx.md#storage)

## Methods

### run()

> **run**\<`Ref`\>(`ref`, ...`args`): `Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

#### Type Parameters

##### Ref

`Ref` *extends* [`FunctionReference`](../../index/type-aliases/FunctionReference.md)\<`"mutation"` \| `"action"` \| `"query"`, `any`, `any`, `any`\>

#### Parameters

##### ref

`Ref`

##### args

...[`OptionalRestArgs`](../../index/type-aliases/OptionalRestArgs.md)\<`Ref`\>

#### Returns

`Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

#### Inherited from

[`ActionCtx`](ActionCtx.md).[`run`](ActionCtx.md#run)

***

### runAction()

> **runAction**\<`Ref`\>(`ref`, ...`args`): `Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

#### Type Parameters

##### Ref

`Ref` *extends* [`FunctionReference`](../../index/type-aliases/FunctionReference.md)\<`"action"`, `any`, `any`, `any`\>

#### Parameters

##### ref

`Ref`

##### args

...[`OptionalRestArgs`](../../index/type-aliases/OptionalRestArgs.md)\<`Ref`\>

#### Returns

`Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

#### Inherited from

[`ActionCtx`](ActionCtx.md).[`runAction`](ActionCtx.md#runaction)

***

### runMutation()

> **runMutation**\<`Ref`\>(`ref`, ...`args`): `Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

#### Type Parameters

##### Ref

`Ref` *extends* [`FunctionReference`](../../index/type-aliases/FunctionReference.md)\<`"mutation"`, `any`, `any`, `any`\>

#### Parameters

##### ref

`Ref`

##### args

...[`OptionalRestArgs`](../../index/type-aliases/OptionalRestArgs.md)\<`Ref`\>

#### Returns

`Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

#### Inherited from

[`ActionCtx`](ActionCtx.md).[`runMutation`](ActionCtx.md#runmutation)

***

### runQuery()

> **runQuery**\<`Ref`\>(`ref`, ...`args`): `Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

#### Type Parameters

##### Ref

`Ref` *extends* [`FunctionReference`](../../index/type-aliases/FunctionReference.md)\<`"query"`, `any`, `any`, `any`\>

#### Parameters

##### ref

`Ref`

##### args

...[`OptionalRestArgs`](../../index/type-aliases/OptionalRestArgs.md)\<`Ref`\>

#### Returns

`Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

#### Inherited from

[`ActionCtx`](ActionCtx.md).[`runQuery`](ActionCtx.md#runquery)
