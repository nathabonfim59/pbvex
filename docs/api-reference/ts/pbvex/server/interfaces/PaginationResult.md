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

Opaque cursor for the next page; the runtime emits an empty string when `isDone` is true.

***

### isDone

> **isDone**: `boolean`

True when pagination is complete and there is no next page.

***

### page

> **page**: [`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>[]

Documents in this page.
