[pbvex](../../index.md) / [server](../index.md) / MigrationDefinition

# Interface: MigrationDefinition\<From, To, Table\>

## Type Parameters

### From

`From` *extends* [`ObjectValidator`](../../values/type-aliases/ObjectValidator.md)\<`any`, `any`\> = [`ObjectValidator`](../../values/type-aliases/ObjectValidator.md)\<`any`, `any`\>

### To

`To` *extends* [`ObjectValidator`](../../values/type-aliases/ObjectValidator.md)\<`any`, `any`\> = [`ObjectValidator`](../../values/type-aliases/ObjectValidator.md)\<`any`, `any`\>

### Table

`Table` *extends* `string` = `string`

## Properties

### down

> `readonly` **down**: (`newDoc`, `ctx`) => `MigrationOutput`\<`ValidatorInput`\<`From`\>\>

#### Parameters

##### newDoc

`MigrationDocument`\<`ValidatorOutput`\<`To`\>, `Table`\>

##### ctx

[`MigrationContext`](MigrationContext.md)

#### Returns

`MigrationOutput`\<`ValidatorInput`\<`From`\>\>

***

### from

> `readonly` **from**: `From`

***

### id

> `readonly` **id**: `string`

***

### kind

> `readonly` **kind**: `"pbvex.migration"`

***

### mode

> `readonly` **mode**: `"transactional"`

***

### table

> `readonly` **table**: `Table`

***

### to

> `readonly` **to**: `To`

***

### up

> `readonly` **up**: (`oldDoc`, `ctx`) => `MigrationOutput`\<`ValidatorInput`\<`To`\>\>

#### Parameters

##### oldDoc

`MigrationDocument`\<`ValidatorOutput`\<`From`\>, `Table`\>

##### ctx

[`MigrationContext`](MigrationContext.md)

#### Returns

`MigrationOutput`\<`ValidatorInput`\<`To`\>\>
