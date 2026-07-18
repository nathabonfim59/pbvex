[pbvex](../../index.md) / [server](../index.md) / defineMigration

# Function: defineMigration()

> **defineMigration**\<`From`, `To`, `Table`\>(`options`): [`MigrationDefinition`](../interfaces/MigrationDefinition.md)\<`From`, `To`, `Table`\>

Defines a pure synchronous, reversible transactional document migration.

## Type Parameters

### From

`From` *extends* [`ObjectValidator`](../../values/type-aliases/ObjectValidator.md)\<`any`, `any`\>

### To

`To` *extends* [`ObjectValidator`](../../values/type-aliases/ObjectValidator.md)\<`any`, `any`\>

### Table

`Table` *extends* `string`

## Parameters

### options

[`MigrationOptions`](../interfaces/MigrationOptions.md)\<`From`, `To`, `Table`\>

## Returns

[`MigrationDefinition`](../interfaces/MigrationDefinition.md)\<`From`, `To`, `Table`\>
