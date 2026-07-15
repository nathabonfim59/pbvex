[pbvex](../../index.md) / [server](../index.md) / IndexNamesForTable

# Type Alias: IndexNamesForTable\<DataModel, TableName\>

> **IndexNamesForTable**\<`DataModel`, `TableName`\> = `NonNullable`\<[`NamedTableInfo`](NamedTableInfo.md)\<`DataModel`, `TableName`\>\[`"indexes"`\]\> *extends* `never` ? `never` : keyof `NonNullable`\<[`NamedTableInfo`](NamedTableInfo.md)\<`DataModel`, `TableName`\>\[`"indexes"`\]\> & `string`

## Type Parameters

### DataModel

`DataModel` *extends* [`GenericDataModel`](GenericDataModel.md)

### TableName

`TableName` *extends* [`TableNamesInDataModel`](TableNamesInDataModel.md)\<`DataModel`\>
