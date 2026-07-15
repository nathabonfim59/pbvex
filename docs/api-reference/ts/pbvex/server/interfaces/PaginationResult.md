[pbvex](../../index.md) / [server](../index.md) / PaginationResult

# Interface: PaginationResult\<TableName, DataModel\>

## Type Parameters

### TableName

`TableName` *extends* `string`

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md)

## Properties

### continueCursor

> **continueCursor**: `string`

***

### isDone

> **isDone**: `boolean`

***

### page

> **page**: [`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]
