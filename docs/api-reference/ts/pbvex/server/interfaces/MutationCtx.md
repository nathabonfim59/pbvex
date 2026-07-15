[pbvex](../../index.md) / [server](../index.md) / MutationCtx

# Interface: MutationCtx\<DataModel\>

## Extends

- [`FunctionContext`](FunctionContext.md)\<`DataModel`\>

## Type Parameters

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md) = [`GenericDataModel`](../type-aliases/GenericDataModel.md)

## Properties

### auth

> **auth**: [`AuthContext`](AuthContext.md)

#### Overrides

[`FunctionContext`](FunctionContext.md).[`auth`](FunctionContext.md#auth)

***

### db

> **db**: [`DatabaseWriter`](DatabaseWriter.md)\<`DataModel`\>

#### Overrides

[`FunctionContext`](FunctionContext.md).[`db`](FunctionContext.md#db)

***

### http?

> `optional` **http?**: [`HttpContext`](HttpContext.md)

#### Inherited from

[`FunctionContext`](FunctionContext.md).[`http`](FunctionContext.md#http)

***

### scheduler

> **scheduler**: [`SchedulerContext`](SchedulerContext.md)

#### Overrides

[`FunctionContext`](FunctionContext.md).[`scheduler`](FunctionContext.md#scheduler)

***

### storage

> **storage**: [`StorageContext`](StorageContext.md)

#### Overrides

[`FunctionContext`](FunctionContext.md).[`storage`](FunctionContext.md#storage)

## Methods

### runAction()?

> `optional` **runAction**\<`Ref`\>(`ref`, ...`args`): `Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

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

[`FunctionContext`](FunctionContext.md).[`runAction`](FunctionContext.md#runaction)

***

### runMutation()?

> `optional` **runMutation**\<`Ref`\>(`ref`, ...`args`): `Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

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

[`FunctionContext`](FunctionContext.md).[`runMutation`](FunctionContext.md#runmutation)

***

### runQuery()?

> `optional` **runQuery**\<`Ref`\>(`ref`, ...`args`): `Promise`\<[`FunctionReturnType`](../../index/type-aliases/FunctionReturnType.md)\<`Ref`\>\>

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

[`FunctionContext`](FunctionContext.md).[`runQuery`](FunctionContext.md#runquery)
