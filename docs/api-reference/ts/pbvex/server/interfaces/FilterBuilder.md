[pbvex](../../index.md) / [server](../index.md) / FilterBuilder

# Interface: FilterBuilder\<TableName, DataModel\>

## Type Parameters

### TableName

`TableName` *extends* `string`

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md)

## Methods

### add()

> **add**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`T`\>

#### Type Parameters

##### T

`T` *extends* [`NumericValue`](../type-aliases/NumericValue.md)

#### Parameters

##### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

#### Returns

[`Expression`](../classes/Expression.md)\<`T`\>

***

### and()

> **and**(...`exprs`): [`Expression`](../classes/Expression.md)\<`boolean`\>

#### Parameters

##### exprs

...[`Expression`](../classes/Expression.md)\<`boolean`\>[]

#### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

***

### div()

> **div**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`T`\>

#### Type Parameters

##### T

`T` *extends* [`NumericValue`](../type-aliases/NumericValue.md)

#### Parameters

##### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

#### Returns

[`Expression`](../classes/Expression.md)\<`T`\>

***

### eq()

#### Call Signature

> **eq**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`Expression`](../classes/Expression.md)\<`T`\>

###### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

#### Call Signature

> **eq**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

###### r

[`Expression`](../classes/Expression.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

***

### field()

> **field**\<`FieldPath`\>(`fieldPath`): [`Expression`](../classes/Expression.md)\<[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `FieldPath`\>\>

#### Type Parameters

##### FieldPath

`FieldPath` *extends* `"_id"` \| `"_creationTime"`

#### Parameters

##### fieldPath

`FieldPath`

#### Returns

[`Expression`](../classes/Expression.md)\<[`FieldTypeFromFieldPath`](../type-aliases/FieldTypeFromFieldPath.md)\<[`DocumentByName`](../type-aliases/DocumentByName.md)\<`DataModel`, `TableName`\>, `FieldPath`\>\>

***

### gt()

#### Call Signature

> **gt**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`Expression`](../classes/Expression.md)\<`T`\>

###### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

#### Call Signature

> **gt**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

###### r

[`Expression`](../classes/Expression.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

***

### gte()

#### Call Signature

> **gte**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`Expression`](../classes/Expression.md)\<`T`\>

###### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

#### Call Signature

> **gte**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

###### r

[`Expression`](../classes/Expression.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

***

### lt()

#### Call Signature

> **lt**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`Expression`](../classes/Expression.md)\<`T`\>

###### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

#### Call Signature

> **lt**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

###### r

[`Expression`](../classes/Expression.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

***

### lte()

#### Call Signature

> **lte**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`Expression`](../classes/Expression.md)\<`T`\>

###### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

#### Call Signature

> **lte**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

###### r

[`Expression`](../classes/Expression.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

***

### mod()

> **mod**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`T`\>

#### Type Parameters

##### T

`T` *extends* [`NumericValue`](../type-aliases/NumericValue.md)

#### Parameters

##### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

#### Returns

[`Expression`](../classes/Expression.md)\<`T`\>

***

### mul()

> **mul**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`T`\>

#### Type Parameters

##### T

`T` *extends* [`NumericValue`](../type-aliases/NumericValue.md)

#### Parameters

##### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

#### Returns

[`Expression`](../classes/Expression.md)\<`T`\>

***

### neg()

> **neg**\<`T`\>(`x`): [`Expression`](../classes/Expression.md)\<`T`\>

#### Type Parameters

##### T

`T` *extends* [`NumericValue`](../type-aliases/NumericValue.md)

#### Parameters

##### x

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

#### Returns

[`Expression`](../classes/Expression.md)\<`T`\>

***

### neq()

#### Call Signature

> **neq**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`Expression`](../classes/Expression.md)\<`T`\>

###### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

#### Call Signature

> **neq**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`boolean`\>

##### Type Parameters

###### T

`T` *extends* `unknown`

##### Parameters

###### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

###### r

[`Expression`](../classes/Expression.md)\<`T`\>

##### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

***

### not()

> **not**(`x`): [`Expression`](../classes/Expression.md)\<`boolean`\>

#### Parameters

##### x

[`Expression`](../classes/Expression.md)\<`boolean`\>

#### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

***

### or()

> **or**(...`exprs`): [`Expression`](../classes/Expression.md)\<`boolean`\>

#### Parameters

##### exprs

...[`Expression`](../classes/Expression.md)\<`boolean`\>[]

#### Returns

[`Expression`](../classes/Expression.md)\<`boolean`\>

***

### sub()

> **sub**\<`T`\>(`l`, `r`): [`Expression`](../classes/Expression.md)\<`T`\>

#### Type Parameters

##### T

`T` *extends* [`NumericValue`](../type-aliases/NumericValue.md)

#### Parameters

##### l

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

##### r

[`ExpressionOrValue`](../type-aliases/ExpressionOrValue.md)\<`T`\>

#### Returns

[`Expression`](../classes/Expression.md)\<`T`\>
