[pbvex](../../index.md) / [server](../index.md) / OrderedQuery

# Interface: OrderedQuery\<TableName, DataModel\>

## Extended by

- [`Query`](Query.md)

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

***

### filter()

> **filter**(`predicate`): `this`

#### Parameters

##### predicate

(`q`) => [`Expression`](../classes/Expression.md)\<`boolean`\>

#### Returns

`this`

***

### first()

> **first**(): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

***

### paginate()

> **paginate**(`opts`): `Promise`\<[`PaginationResult`](PaginationResult.md)\<`TableName`, `DataModel`\>\>

#### Parameters

##### opts

[`PaginationOptions`](PaginationOptions.md)

#### Returns

`Promise`\<[`PaginationResult`](PaginationResult.md)\<`TableName`, `DataModel`\>\>

***

### take()

> **take**(`n`): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]\>

#### Parameters

##### n

`number`

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]\>

***

### unique()

> **unique**(): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>
