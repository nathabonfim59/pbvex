[pbvex](../../index.md) / [server](../index.md) / ActionCtx

# Interface: ActionCtx\<DataModel\>

## Extends

- [`FunctionContext`](FunctionContext.md)\<`DataModel`\>

## Extended by

- [`HttpActionCtx`](HttpActionCtx.md)

## Type Parameters

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md) = [`GenericDataModel`](../type-aliases/GenericDataModel.md)

## Properties

### auth

> **auth**: [`AuthContext`](AuthContext.md)

#### Overrides

[`FunctionContext`](FunctionContext.md).[`auth`](FunctionContext.md#auth)

***

### db?

> `optional` **db?**: `undefined`

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

#### Overrides

[`FunctionContext`](FunctionContext.md).[`runAction`](FunctionContext.md#runaction)

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

#### Overrides

[`FunctionContext`](FunctionContext.md).[`runMutation`](FunctionContext.md#runmutation)

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

#### Overrides

[`FunctionContext`](FunctionContext.md).[`runQuery`](FunctionContext.md#runquery)
