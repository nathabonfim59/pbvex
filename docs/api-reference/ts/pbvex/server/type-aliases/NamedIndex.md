[pbvex](../../index.md) / [server](../index.md) / NamedIndex

# Type Alias: NamedIndex\<DataModel, TableName, IndexName\>

> **NamedIndex**\<`DataModel`, `TableName`, `IndexName`\> = `NonNullable`\<[`NamedTableInfo`](NamedTableInfo.md)\<`DataModel`, `TableName`\>\[`"indexes"`\]\>\[`IndexName`\]

## Type Parameters

### DataModel

`DataModel` *extends* [`GenericDataModel`](GenericDataModel.md)

### TableName

`TableName` *extends* [`TableNamesInDataModel`](TableNamesInDataModel.md)\<`DataModel`\>

### IndexName

`IndexName` *extends* [`IndexNamesForTable`](IndexNamesForTable.md)\<`DataModel`, `TableName`\>
