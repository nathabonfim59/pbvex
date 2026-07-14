[pbvex](../../index.md) / [server](../index.md) / DatabaseWriter

# Interface: DatabaseWriter\<DataModel\>

## Extends

- [`DatabaseReader`](DatabaseReader.md)\<`DataModel`\>

## Type Parameters

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md)

## Methods

### delete()

> **delete**\<`TableName`\>(`id`): `Promise`\<`void`\>

#### Type Parameters

##### TableName

`TableName` *extends* `string`

#### Parameters

##### id

[`Id`](../type-aliases/Id.md)\<`TableName`\>

#### Returns

`Promise`\<`void`\>

***

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

#### Inherited from

[`DatabaseReader`](DatabaseReader.md).[`get`](DatabaseReader.md#get)

***

### insert()

> **insert**\<`TableName`\>(`table`, `value`): `Promise`\<[`Id`](../type-aliases/Id.md)\<`TableName`\>\>

#### Type Parameters

##### TableName

`TableName` *extends* `string`

#### Parameters

##### table

`TableName`

##### value

[`InsertByName`](../type-aliases/InsertByName.md)\<`DataModel`, `TableName`\>

#### Returns

`Promise`\<[`Id`](../type-aliases/Id.md)\<`TableName`\>\>

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

#### Inherited from

[`DatabaseReader`](DatabaseReader.md).[`normalizeId`](DatabaseReader.md#normalizeid)

***

### patch()

> **patch**\<`TableName`\>(`id`, `value`): `Promise`\<`void`\>

#### Type Parameters

##### TableName

`TableName` *extends* `string`

#### Parameters

##### id

[`Id`](../type-aliases/Id.md)\<`TableName`\>

##### value

[`PatchValue`](../type-aliases/PatchValue.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>\>

#### Returns

`Promise`\<`void`\>

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

#### Inherited from

[`DatabaseReader`](DatabaseReader.md).[`query`](DatabaseReader.md#query)

***

### replace()

> **replace**\<`TableName`\>(`id`, `value`): `Promise`\<`void`\>

#### Type Parameters

##### TableName

`TableName` *extends* `string`

#### Parameters

##### id

[`Id`](../type-aliases/Id.md)\<`TableName`\>

##### value

[`InsertByName`](../type-aliases/InsertByName.md)\<`DataModel`, `TableName`\>

#### Returns

`Promise`\<`void`\>
