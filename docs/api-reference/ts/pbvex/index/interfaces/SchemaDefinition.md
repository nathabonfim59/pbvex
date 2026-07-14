[pbvex](../../index.md) / [index](../index.md) / SchemaDefinition

# Interface: SchemaDefinition\<Tables\>

## Type Parameters

### Tables

`Tables` *extends* `Record`\<`string`, [`TableDefinition`](TableDefinition.md)\> = `Record`\<`string`, [`TableDefinition`](TableDefinition.md)\>

## Properties

### kind

> `readonly` **kind**: `"schema"`

***

### tableNames

> `readonly` **tableNames**: readonly keyof `Tables` & `string`[]

***

### tables

> `readonly` **tables**: `Readonly`\<`Tables` & `Record`\<`string`, [`TableDefinition`](TableDefinition.md) \| `undefined`\>\>

## Methods

### getTable()

> **getTable**\<`Name`\>(`name`): `Tables`\[`Name`\]

#### Type Parameters

##### Name

`Name` *extends* `string`

#### Parameters

##### name

`Name`

#### Returns

`Tables`\[`Name`\]

***

### toJSON()

> **toJSON**(): `SchemaDefinitionJson`

#### Returns

`SchemaDefinitionJson`
