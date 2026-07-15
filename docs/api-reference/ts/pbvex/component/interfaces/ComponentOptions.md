[pbvex](../../index.md) / [component](../index.md) / ComponentOptions

# Interface: ComponentOptions

## Properties

### args?

> `optional` **args?**: [`ComponentArgValidator`](../type-aliases/ComponentArgValidator.md)

***

### dependencies?

> `optional` **dependencies?**: [`ComponentDefinitionWithKind`](ComponentDefinitionWithKind.md)[]

***

### env?

> `optional` **env?**: `Record`\<`string`, [`EnvEntry`](../type-aliases/EnvEntry.md)\>

***

### modulePaths?

> `optional` **modulePaths?**: `string`[]

***

### schema?

> `optional` **schema?**: [`SchemaDefinition`](../../index/interfaces/SchemaDefinition.md)\<`Record`\<`string`, [`TableDefinition`](../../index/interfaces/TableDefinition.md)\<`Record`\<`string`, [`Validator`](../../values/type-aliases/Validator.md)\<`any`\>\>\>\>\>
