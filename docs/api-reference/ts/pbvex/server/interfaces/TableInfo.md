[pbvex](../../index.md) / [server](../index.md) / TableInfo

# Interface: TableInfo\<Document, Indexes, Insert\>

## Type Parameters

### Document

`Document` *extends* [`GenericDocument`](GenericDocument.md) = [`GenericDocument`](GenericDocument.md)

### Indexes

`Indexes` *extends* `Record`\<`string`, [`IndexInfo`](IndexInfo.md)\> \| `never` = `Record`\<`string`, [`IndexInfo`](IndexInfo.md)\>

### Insert

`Insert` *extends* `Record`\<`string`, `any`\> = [`WithoutSystemFields`](../type-aliases/WithoutSystemFields.md)\<`Document`\>

## Properties

### document

> **document**: `Document`

***

### indexes?

> `optional` **indexes?**: `Indexes`

***

### insert

> **insert**: `Insert`
