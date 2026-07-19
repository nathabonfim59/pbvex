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

> `readonly` **tables**: `Readonly`\<`BoundTables`\<`Tables`\> & `Record`\<`string`, [`SchemaTableDefinition`](../type-aliases/SchemaTableDefinition.md)\<`string`\> \| `undefined`\>\>

## Methods

### getTable()

> **getTable**\<`Name`\>(`name`): `BoundTable`\<`Tables`\[`Name`\], `Name`\>

#### Type Parameters

##### Name

`Name` *extends* `string`

#### Parameters

##### name

`Name`

#### Returns

`BoundTable`\<`Tables`\[`Name`\], `Name`\>

***

### toJSON()

> **toJSON**(): `SchemaDefinitionJson`

#### Returns

`SchemaDefinitionJson`
