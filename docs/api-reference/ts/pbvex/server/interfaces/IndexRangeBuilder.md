[pbvex](../../index.md) / [server](../index.md) / IndexRangeBuilder

# Interface: IndexRangeBuilder\<TableName, DataModel, IndexFields, FieldNum\>

## Extends

- [`LowerBoundIndexRangeBuilder`](LowerBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFields`\[`FieldNum`\]\>

## Type Parameters

### TableName

`TableName` *extends* `string`

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md)

### IndexFields

`IndexFields` *extends* readonly `string`[]

### FieldNum

`FieldNum` *extends* `number` = `0`

## Methods

### eq()

> **eq**(`fieldName`, `value`): `PlusOne`\<`FieldNum`\> *extends* `IndexFields`\[`"length"`\] ? [`IndexRange`](../classes/IndexRange.md) : `IndexRangeBuilder`\<`TableName`, `DataModel`, `IndexFields`, `PlusOne`\<`FieldNum`\>\>

#### Parameters

##### fieldName

`IndexFields`\[`FieldNum`\]

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFields`\[`FieldNum`\]\>

#### Returns

`PlusOne`\<`FieldNum`\> *extends* `IndexFields`\[`"length"`\] ? [`IndexRange`](../classes/IndexRange.md) : `IndexRangeBuilder`\<`TableName`, `DataModel`, `IndexFields`, `PlusOne`\<`FieldNum`\>\>

***

### gt()

> **gt**(`fieldName`, `value`): [`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFields`\[`FieldNum`\]\>

#### Parameters

##### fieldName

`IndexFields`\[`FieldNum`\]

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFields`\[`FieldNum`\]\>

#### Returns

[`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFields`\[`FieldNum`\]\>

#### Inherited from

[`LowerBoundIndexRangeBuilder`](LowerBoundIndexRangeBuilder.md).[`gt`](LowerBoundIndexRangeBuilder.md#gt)

***

### gte()

> **gte**(`fieldName`, `value`): [`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFields`\[`FieldNum`\]\>

#### Parameters

##### fieldName

`IndexFields`\[`FieldNum`\]

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFields`\[`FieldNum`\]\>

#### Returns

[`UpperBoundIndexRangeBuilder`](UpperBoundIndexRangeBuilder.md)\<`TableName`, `DataModel`, `IndexFields`\[`FieldNum`\]\>

#### Inherited from

[`LowerBoundIndexRangeBuilder`](LowerBoundIndexRangeBuilder.md).[`gte`](LowerBoundIndexRangeBuilder.md#gte)

***

### lt()

> **lt**(`fieldName`, `value`): [`IndexRange`](../classes/IndexRange.md)

#### Parameters

##### fieldName

`IndexFields`\[`FieldNum`\]

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFields`\[`FieldNum`\]\>

#### Returns

[`IndexRange`](../classes/IndexRange.md)

#### Inherited from

[`LowerBoundIndexRangeBuilder`](LowerBoundIndexRangeBuilder.md).[`lt`](LowerBoundIndexRangeBuilder.md#lt)

***

### lte()

> **lte**(`fieldName`, `value`): [`IndexRange`](../classes/IndexRange.md)

#### Parameters

##### fieldName

`IndexFields`\[`FieldNum`\]

##### value

[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `IndexFields`\[`FieldNum`\]\>

#### Returns

[`IndexRange`](../classes/IndexRange.md)

#### Inherited from

[`LowerBoundIndexRangeBuilder`](LowerBoundIndexRangeBuilder.md).[`lte`](LowerBoundIndexRangeBuilder.md#lte)
