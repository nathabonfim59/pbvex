[pbvex](../../index.md) / [values](../index.md) / v

# Variable: v

> `const` **v**: `object`

## Type Declaration

### any

> **any**: () => [`Validator`](../type-aliases/Validator.md)\<`any`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`any`\>

### array

> **array**: \<`T`\>(`item`) => [`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\>[], `Exclude`\<`InputOf`\<`T`\>, `undefined`\>[]\>

#### Type Parameters

##### T

`T` *extends* [`Validator`](../type-aliases/Validator.md)\<`any`, `any`\>

#### Parameters

##### item

`T`

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\>[], `Exclude`\<`InputOf`\<`T`\>, `undefined`\>[]\>

### bigint

> **bigint**: () => [`Validator`](../type-aliases/Validator.md)\<`bigint`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`bigint`\>

### boolean

> **boolean**: () => [`Validator`](../type-aliases/Validator.md)\<`boolean`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`boolean`\>

### bytes

> **bytes**: () => [`Validator`](../type-aliases/Validator.md)\<`ArrayBuffer`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`ArrayBuffer`\>

### defaulted

> **defaulted**: \<`T`\>(`validator`, `defaultValue`) => [`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\>, `InputOf`\<`T`\> \| `undefined`\>

#### Type Parameters

##### T

`T` *extends* [`Validator`](../type-aliases/Validator.md)\<`any`, `any`\>

#### Parameters

##### validator

`T`

##### defaultValue

`OutputOf`\<`T`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\>, `InputOf`\<`T`\> \| `undefined`\>

### delayed

> **delayed**: \<`T`\>(`factory`) => [`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\>, `InputOf`\<`T`\>\>

#### Type Parameters

##### T

`T` *extends* [`Validator`](../type-aliases/Validator.md)\<`any`, `any`\>

#### Parameters

##### factory

() => `T`

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\>, `InputOf`\<`T`\>\>

### float64

> **float64**: () => [`Validator`](../type-aliases/Validator.md)\<`number`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`number`\>

### id

> **id**: \<`TableName`\>(`tableName`) => [`Validator`](../type-aliases/Validator.md)\<[`GenericId`](../type-aliases/GenericId.md)\<`TableName`\>\>

#### Type Parameters

##### TableName

`TableName` *extends* `string` = `string`

#### Parameters

##### tableName

`TableName`

#### Returns

[`Validator`](../type-aliases/Validator.md)\<[`GenericId`](../type-aliases/GenericId.md)\<`TableName`\>\>

### image

> **image**: (`options`) => [`Validator`](../type-aliases/Validator.md)\<[`StorageId`](../../index/type-aliases/StorageId.md)\>

#### Parameters

##### options?

[`ImageValidatorOptions`](../interfaces/ImageValidatorOptions.md) = `{}`

#### Returns

[`Validator`](../type-aliases/Validator.md)\<[`StorageId`](../../index/type-aliases/StorageId.md)\>

### int64

> **int64**: () => [`Validator`](../type-aliases/Validator.md)\<`bigint`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`bigint`\>

### literal

> **literal**: \<`T`\>(`value`) => [`Validator`](../type-aliases/Validator.md)\<`T`\>

#### Type Parameters

##### T

`T` *extends* `string` \| `number` \| `bigint` \| `boolean`

#### Parameters

##### value

`T`

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`T`\>

### null

> **null**: () => [`Validator`](../type-aliases/Validator.md)\<`null`\> = `nullType`

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`null`\>

### number

> **number**: () => [`Validator`](../type-aliases/Validator.md)\<`number`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`number`\>

### object

> **object**: \<`T`\>(`shape`) => [`ObjectValidatorFor`](../type-aliases/ObjectValidatorFor.md)\<`T`\>

#### Type Parameters

##### T

`T` *extends* `ValidatorShape`

#### Parameters

##### shape

`T`

#### Returns

[`ObjectValidatorFor`](../type-aliases/ObjectValidatorFor.md)\<`T`\>

### optional

> **optional**: \<`T`\>(`validator`) => [`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\> \| `undefined`, `InputOf`\<`T`\> \| `undefined`\>

#### Type Parameters

##### T

`T` *extends* [`Validator`](../type-aliases/Validator.md)\<`any`, `any`\>

#### Parameters

##### validator

`T`

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\> \| `undefined`, `InputOf`\<`T`\> \| `undefined`\>

### record

> **record**: \<`KOut`, `KIn`, `Value`\>(`key`, `value`) => [`Validator`](../type-aliases/Validator.md)\<`Record`\<`KOut`, `OutputOf`\<`Value`\>\>, `Record`\<`KIn`, `Exclude`\<`InputOf`\<`Value`\>, `undefined`\>\>\>

#### Type Parameters

##### KOut

`KOut` *extends* `string`

##### KIn

`KIn` *extends* `string`

##### Value

`Value` *extends* [`Validator`](../type-aliases/Validator.md)\<`any`, `any`\>

#### Parameters

##### key

[`Validator`](../type-aliases/Validator.md)\<`KOut`, `KIn`\>

##### value

`Value`

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`Record`\<`KOut`, `OutputOf`\<`Value`\>\>, `Record`\<`KIn`, `Exclude`\<`InputOf`\<`Value`\>, `undefined`\>\>\>

### recursive

> **recursive**: \<`T`\>(`name`, `factory`) => [`Validator`](../type-aliases/Validator.md)\<`T`\>

Declares a named, serializable recursive validator. The descriptor is
`{type:'recursive', name, validator}` where `validator` is the full inner
descriptor; cycle points inside it emit `{type:'ref', name}`. The backend
resolves refs against the enclosing recursive declaration, so recursive
types are genuinely executable (document insert/patch validation, not just
manifest acceptance).

#### Type Parameters

##### T

`T`

#### Parameters

##### name

`string`

##### factory

() => [`Validator`](../type-aliases/Validator.md)\<`T`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`T`\>

### string

> **string**: () => [`Validator`](../type-aliases/Validator.md)\<`string`\>

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`string`\>

### union

> **union**: \<`T`\>(...`validators`) => [`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\[`number`\]\>, `InputOf`\<`T`\[`number`\]\>\>

#### Type Parameters

##### T

`T` *extends* readonly [`Validator`](../type-aliases/Validator.md)\<`any`, `any`\>[]

#### Parameters

##### validators

...`T`

#### Returns

[`Validator`](../type-aliases/Validator.md)\<`OutputOf`\<`T`\[`number`\]\>, `InputOf`\<`T`\[`number`\]\>\>
