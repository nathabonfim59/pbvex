[pbvex](../../index.md) / [server](../index.md) / DatabaseReader

# Interface: DatabaseReader\<DataModel\>

## Extended by

- [`DatabaseWriter`](DatabaseWriter.md)

## Type Parameters

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md)

## Methods

### get()

> **get**\<`TableName`\>(`id`): `Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

#### Type Parameters

##### TableName

`TableName` *extends* `string`

#### Parameters

##### id

[`Id`](../type-aliases/Id.md)\<`TableName`\>

#### Returns

`Promise`\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\> \| `null`\>

***

### normalizeId()

> **normalizeId**\<`TableName`\>(`table`, `id`): [`Id`](../type-aliases/Id.md)\<`TableName`\> \| `null`

#### Type Parameters

##### TableName

`TableName` *extends* `string`

#### Parameters

##### table

`TableName`

##### id

`string`

#### Returns

[`Id`](../type-aliases/Id.md)\<`TableName`\> \| `null`

***

### query()

> **query**\<`TableName`\>(`table`): [`QueryInitializer`](QueryInitializer.md)\<`TableName`, `DataModel`\>

#### Type Parameters

##### TableName

`TableName` *extends* `string`

#### Parameters

##### table

`TableName`

#### Returns

[`QueryInitializer`](QueryInitializer.md)\<`TableName`, `DataModel`\>
