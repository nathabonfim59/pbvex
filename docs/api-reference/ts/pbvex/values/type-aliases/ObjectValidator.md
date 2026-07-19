[pbvex](../../index.md) / [values](../index.md) / ObjectValidator

# Type Alias: ObjectValidator\<Out, In, Shape\>

> **ObjectValidator**\<`Out`, `In`, `Shape`\> = [`Validator`](Validator.md)\<`Out`, `In`\> & `object`

## Type Declaration

### kind

> `readonly` **kind**: `"object"`

### extend()

> **extend**\<`Fields`\>(`fields`): [`ObjectValidatorFor`](ObjectValidatorFor.md)\<`MergeShapes`\<`Shape`, `Fields`\>\>

#### Type Parameters

##### Fields

`Fields` *extends* `ValidatorShape`

#### Parameters

##### fields

`Fields`

#### Returns

[`ObjectValidatorFor`](ObjectValidatorFor.md)\<`MergeShapes`\<`Shape`, `Fields`\>\>

### omit()

> **omit**\<`Keys`\>(...`keys`): [`ObjectValidatorFor`](ObjectValidatorFor.md)\<`Omit`\<`Shape`, `Keys`\[`number`\]\>\>

#### Type Parameters

##### Keys

`Keys` *extends* readonly keyof `Shape` & `string`[]

#### Parameters

##### keys

...`Keys`

#### Returns

[`ObjectValidatorFor`](ObjectValidatorFor.md)\<`Omit`\<`Shape`, `Keys`\[`number`\]\>\>

### partial()

> **partial**(): [`ObjectValidatorFor`](ObjectValidatorFor.md)\<`PartialShape`\<`Shape`\>\>

#### Returns

[`ObjectValidatorFor`](ObjectValidatorFor.md)\<`PartialShape`\<`Shape`\>\>

### pick()

> **pick**\<`Keys`\>(...`keys`): [`ObjectValidatorFor`](ObjectValidatorFor.md)\<`Pick`\<`Shape`, `Keys`\[`number`\]\>\>

#### Type Parameters

##### Keys

`Keys` *extends* readonly keyof `Shape` & `string`[]

#### Parameters

##### keys

...`Keys`

#### Returns

[`ObjectValidatorFor`](ObjectValidatorFor.md)\<`Pick`\<`Shape`, `Keys`\[`number`\]\>\>

## Type Parameters

### Out

`Out` *extends* `Record`\<`string`, `any`\> = `Record`\<`string`, `any`\>

### In

`In` *extends* `Record`\<`string`, `any`\> = `Out`

### Shape

`Shape` *extends* `ValidatorShape` = `ValidatorShape`
