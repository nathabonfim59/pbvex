[pbvex](../../index.md) / [server](../index.md) / Query

# Interface: Query\<TableName, DataModel\>

## Extends

- [`OrderedQuery`](OrderedQuery.md)\<`TableName`, `DataModel`\>

## Extended by

- [`QueryInitializer`](QueryInitializer.md)

## Type Parameters

### TableName

`TableName` *extends* `string`

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md)

## Methods

### collect()

> **collect**(): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]\>

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]\>

#### Inherited from

[`OrderedQuery`](OrderedQuery.md).[`collect`](OrderedQuery.md#collect)

***

### filter()

> **filter**(`predicate`): `this`

#### Parameters

##### predicate

(`q`) => [`Expression`](../classes/Expression.md)\<`boolean`\>

#### Returns

`this`

#### Inherited from

[`OrderedQuery`](OrderedQuery.md).[`filter`](OrderedQuery.md#filter)

***

### first()

> **first**(): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Inherited from

[`OrderedQuery`](OrderedQuery.md).[`first`](OrderedQuery.md#first)

***

### order()

> **order**(`direction`): [`OrderedQuery`](OrderedQuery.md)\<`TableName`, `DataModel`\>

#### Parameters

##### direction

`"asc"` \| `"desc"`

#### Returns

[`OrderedQuery`](OrderedQuery.md)\<`TableName`, `DataModel`\>

***

### paginate()

> **paginate**(`opts`): `Promise`\<[`PaginationResult`](PaginationResult.md)\<`TableName`, `DataModel`\>\>

#### Parameters

##### opts

[`PaginationOptions`](PaginationOptions.md)

#### Returns

`Promise`\<[`PaginationResult`](PaginationResult.md)\<`TableName`, `DataModel`\>\>

#### Inherited from

[`OrderedQuery`](OrderedQuery.md).[`paginate`](OrderedQuery.md#paginate)

***

### take()

> **take**(`n`): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]\>

#### Parameters

##### n

`number`

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]\>

#### Inherited from

[`OrderedQuery`](OrderedQuery.md).[`take`](OrderedQuery.md#take)

***

### unique()

> **unique**(): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Inherited from

[`OrderedQuery`](OrderedQuery.md).[`unique`](OrderedQuery.md#unique)
