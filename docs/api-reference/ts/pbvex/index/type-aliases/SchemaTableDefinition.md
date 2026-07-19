[pbvex](../../index.md) / [index](../index.md) / SchemaTableDefinition

# Type Alias: SchemaTableDefinition\<TableName, Fields\>

> **SchemaTableDefinition**\<`TableName`, `Fields`\> = [`TableDefinition`](../interfaces/TableDefinition.md)\<`Fields`\> & `object`

## Type Declaration

### documentValidator

> `readonly` **documentValidator**: [`ObjectValidatorFor`](../../values/type-aliases/ObjectValidatorFor.md)\<`DocumentValidatorFields`\<`Fields`, `TableName`\>\>

### tableName

> `readonly` **tableName**: `TableName`

## Type Parameters

### TableName

`TableName` *extends* `string`

### Fields

`Fields` *extends* `Record`\<`string`, [`Validator`](../../values/type-aliases/Validator.md)\<`any`\>\> = `Record`\<`string`, [`Validator`](../../values/type-aliases/Validator.md)\<`any`\>\>
