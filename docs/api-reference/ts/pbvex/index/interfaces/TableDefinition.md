[pbvex](../../index.md) / [index](../index.md) / TableDefinition

# Interface: TableDefinition\<Fields\>

## Type Parameters

### Fields

`Fields` *extends* `Record`\<`string`, [`Validator`](../../values/type-aliases/Validator.md)\<`any`\>\> = `Record`\<`string`, [`Validator`](../../values/type-aliases/Validator.md)\<`any`\>\>

## Properties

### fields

> `readonly` **fields**: `Readonly`\<`Fields`\>

***

### indexes?

> `readonly` `optional` **indexes?**: readonly [`IndexDefinition`](IndexDefinition.md)[]

***

### kind

> `readonly` **kind**: `"table"`

***

### tableName

> `readonly` **tableName**: `string`

## Methods

### index()

> **index**\<`KS`\>(`name`, `fields`): `TableDefinition`\<`Fields`\>

#### Type Parameters

##### KS

`KS` *extends* readonly `IndexableFieldPaths`\<`Fields`\>[]

#### Parameters

##### name

`string`

##### fields

`KS`

#### Returns

`TableDefinition`\<`Fields`\>

***

### toJSON()

> **toJSON**(): `TableDefinitionJson`

#### Returns

`TableDefinitionJson`
