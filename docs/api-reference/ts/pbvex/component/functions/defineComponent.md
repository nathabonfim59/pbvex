[pbvex](../../index.md) / [component](../index.md) / defineComponent

# Function: defineComponent()

> **defineComponent**\<`Args`, `Env`, `InArgs`\>(`options`): [`TypedComponent`](../interfaces/TypedComponent.md)\<`Args`, `Env`, `InArgs`\>

defineComponent creates a typed component definition. OutArgs is inferred
from the args validator's output type; InArgs from the input type (which
marks optional/defaulted fields). The Env type parameter captures the
literal env key set for compile-time key checking.

## Type Parameters

### Args

`Args` = `undefined`

### Env

`Env` *extends* `Record`\<`string`, [`EnvEntry`](../type-aliases/EnvEntry.md)\> \| `undefined` = `undefined`

### InArgs

`InArgs` = `Args`

## Parameters

### options

#### args?

[`Validator`](../../values/type-aliases/Validator.md)\<`Args`, `InArgs`\>

#### dependencies?

[`TypedComponent`](../interfaces/TypedComponent.md)\<`undefined`, `undefined`, `undefined`\>[]

#### env?

`Env`

#### modulePaths?

`string`[]

#### schema?

[`SchemaDefinition`](../../index/interfaces/SchemaDefinition.md)\<`Record`\<`string`, [`TableDefinition`](../../index/interfaces/TableDefinition.md)\<`Record`\<`string`, [`Validator`](../../values/type-aliases/Validator.md)\<`any`\>\>\>\>\>

## Returns

[`TypedComponent`](../interfaces/TypedComponent.md)\<`Args`, `Env`, `InArgs`\>
