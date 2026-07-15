[pbvex](../../index.md) / [values](../index.md) / Validator

# Type Alias: Validator\<Out, In\>

> **Validator**\<`Out`, `In`\> = `object`

## Type Parameters

### Out

`Out` = `any`

### In

`In` = `Out`

## Properties

### \_\_inputType?

> `readonly` `optional` **\_\_inputType?**: `In`

***

### \_\_type

> `readonly` **\_\_type**: `Out`

***

### isValidatorBrand

> `readonly` **isValidatorBrand**: `true`

***

### kind

> `readonly` **kind**: [`ValidatorKind`](ValidatorKind.md)

***

### optional

> `readonly` **optional**: `boolean`

## Methods

### toJSON()

> **toJSON**(): `unknown`

#### Returns

`unknown`

***

### validate()

> **validate**(`value`): `Out`

#### Parameters

##### value

`unknown`

#### Returns

`Out`
