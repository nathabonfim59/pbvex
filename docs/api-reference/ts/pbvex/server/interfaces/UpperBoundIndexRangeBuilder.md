[pbvex](../../index.md) / [server](../index.md) / UpperBoundIndexRangeBuilder

# Interface: UpperBoundIndexRangeBuilder\<TableName, DataModel, IndexFieldName\>

## Extends

- [`IndexRange`](../classes/IndexRange.md)

## Extended by

- [`LowerBoundIndexRangeBuilder`](LowerBoundIndexRangeBuilder.md)

## Type Parameters

### TableName

`TableName` *extends* `string`

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md)

### IndexFieldName

`IndexFieldName` *extends* `string`

## Methods

### lt()

> **lt**(`fieldName`, `value`): [`IndexRange`](../classes/IndexRange.md)

#### Parameters

##### fieldName

`IndexFieldName`

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFieldName`\>

#### Returns

[`IndexRange`](../classes/IndexRange.md)

***

### lte()

> **lte**(`fieldName`, `value`): [`IndexRange`](../classes/IndexRange.md)

#### Parameters

##### fieldName

`IndexFieldName`

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFieldName`\>

#### Returns

[`IndexRange`](../classes/IndexRange.md)
