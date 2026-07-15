[pbvex](../../index.md) / [server](../index.md) / LowerBoundIndexRangeBuilder

# Interface: LowerBoundIndexRangeBuilder\<TableName, DataModel, IndexFieldName\>

## Extends

- [`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFieldName`\>

## Extended by

- [`IndexRangeBuilder`](IndexRangeBuilder.md)

## Type Parameters

### TableName

`TableName` *extends* `string`

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md)

### IndexFieldName

`IndexFieldName` *extends* `string`

## Methods

### gt()

> **gt**(`fieldName`, `value`): [`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFieldName`\>

#### Parameters

##### fieldName

`IndexFieldName`

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFieldName`\>

#### Returns

[`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFieldName`\>

***

### gte()

> **gte**(`fieldName`, `value`): [`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFieldName`\>

#### Parameters

##### fieldName

`IndexFieldName`

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFieldName`\>

#### Returns

[`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFieldName`\>

***

### lt()

> **lt**(`fieldName`, `value`): [`IndexRange`](../classes/IndexRange.md)

#### Parameters

##### fieldName

`IndexFieldName`

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFieldName`\>

#### Returns

[`IndexRange`](../classes/IndexRange.md)

#### Inherited from

[`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md).[`lt`](UpperBoundIndexRangeBuilder.md#lt)

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

#### Inherited from

[`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md).[`lte`](UpperBoundIndexRangeBuilder.md#lte)
