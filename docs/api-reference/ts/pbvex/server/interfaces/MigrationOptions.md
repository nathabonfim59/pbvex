[pbvex](../../index.md) / [server](../index.md) / MigrationOptions

# Interface: MigrationOptions\<From, To, Table\>

## Type Parameters

### From

`From` *extends* [`ObjectValidator`](../../values/type-aliases/ObjectValidator.md)\<`any`, `any`\>

### To

`To` *extends* [`ObjectValidator`](../../values/type-aliases/ObjectValidator.md)\<`any`, `any`\>

### Table

`Table` *extends* `string`

## Properties

### down

> **down**: (`newDoc`, `ctx`) => `MigrationOutput`\<`ValidatorInput`\<`From`\>\>

#### Parameters

##### newDoc

`MigrationDocument`\<`ValidatorOutput`\<`To`\>, `Table`\>

##### ctx

[`MigrationContext`](MigrationContext.md)

#### Returns

`MigrationOutput`\<`ValidatorInput`\<`From`\>\>

***

### from

> **from**: `From`

***

### id

> **id**: `string`

***

### mode

> **mode**: `"transactional"`

***

### table

> **table**: `Table`

***

### to

> **to**: `To`

***

### up

> **up**: (`oldDoc`, `ctx`) => `MigrationOutput`\<`ValidatorInput`\<`To`\>\>

#### Parameters

##### oldDoc

`MigrationDocument`\<`ValidatorOutput`\<`From`\>, `Table`\>

##### ctx

[`MigrationContext`](MigrationContext.md)

#### Returns

`MigrationOutput`\<`ValidatorInput`\<`To`\>\>
