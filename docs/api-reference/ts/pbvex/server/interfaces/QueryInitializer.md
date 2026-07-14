[pbvex](../../index.md) / [server](../index.md) / QueryInitializer

# Interface: QueryInitializer\<TableName, DataModel\>

## Extends

- [`Query`](Query.md)\<`TableName`, `DataModel`\>

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

[`Query`](Query.md).[`collect`](Query.md#collect)

***

### filter()

> **filter**(`predicate`): `this`

#### Parameters

##### predicate

(`q`) => [`Expression`](../classes/Expression.md)\<`boolean`\>

#### Returns

`this`

#### Inherited from

[`Query`](Query.md).[`filter`](Query.md#filter)

***

### first()

> **first**(): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Inherited from

[`Query`](Query.md).[`first`](Query.md#first)

***

### fullTableScan()

> **fullTableScan**(): [`Query`](Query.md)\<`TableName`, `DataModel`\>

#### Returns

[`Query`](Query.md)\<`TableName`, `DataModel`\>

***

### order()

> **order**(`direction`): [`OrderedQuery`](OrderedQuery.md)\<`TableName`, `DataModel`\>

#### Parameters

##### direction

`"asc"` \| `"desc"`

#### Returns

[`OrderedQuery`](OrderedQuery.md)\<`TableName`, `DataModel`\>

#### Inherited from

[`Query`](Query.md).[`order`](Query.md#order)

***

### paginate()

> **paginate**(`opts`): `Promise`\<[`PaginationResult`](PaginationResult.md)\<`TableName`, `DataModel`\>\>

#### Parameters

##### opts

[`PaginationOptions`](PaginationOptions.md)

#### Returns

`Promise`\<[`PaginationResult`](PaginationResult.md)\<`TableName`, `DataModel`\>\>

#### Inherited from

[`Query`](Query.md).[`paginate`](Query.md#paginate)

***

### take()

> **take**(`n`): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]\>

#### Parameters

##### n

`number`

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]\>

#### Inherited from

[`Query`](Query.md).[`take`](Query.md#take)

***

### unique()

> **unique**(): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Inherited from

[`Query`](Query.md).[`unique`](Query.md#unique)

***

### withIndex()

> **withIndex**\<`IndexName`\>(`indexName`, `range?`): [`Query`](Query.md)\<`TableName`, `DataModel`\>

#### Type Parameters

##### IndexName

`IndexName` *extends* `string`

#### Parameters

##### indexName

`IndexName`

##### range?

(`q`) => [`IndexRange`](../classes/IndexRange.md)

#### Returns

[`Query`](Query.md)\<`TableName`, `DataModel`\>
