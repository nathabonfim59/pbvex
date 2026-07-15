[pbvex](../../index.md) / [component](../index.md) / TypedComponent

# Interface: TypedComponent\<OutArgs, Env, InArgs\>

TypedComponent carries type parameters for compile-time inference.
OutArgs is the resolved type the handler sees (after defaults applied).
InArgs is the mount-input type (optional/defaulted fields may be omitted).
The phantom properties are set to null at runtime but typed for inference.

## Extends

- [`ComponentDefinitionWithKind`](ComponentDefinitionWithKind.md)

## Type Parameters

### OutArgs

`OutArgs` = `undefined`

### Env

`Env` *extends* `Record`\<`string`, [`EnvEntry`](../type-aliases/EnvEntry.md)\> \| `undefined` = `undefined`

### InArgs

`InArgs` = `OutArgs`

## Properties

### args?

> `readonly` `optional` **args?**: `JSONValue`

#### Inherited from

`ComponentDefinitionWithKind.args`

***

### componentId

> `readonly` **componentId**: `string`

#### Inherited from

`ComponentDefinitionWithKind.componentId`

***

### dependencies?

> `optional` **dependencies?**: [`ComponentDefinitionWithKind`](ComponentDefinitionWithKind.md)[]

#### Inherited from

[`ComponentDefinitionWithKind`](ComponentDefinitionWithKind.md).[`dependencies`](ComponentDefinitionWithKind.md#dependencies)

***

### env?

> `readonly` `optional` **env?**: `Record`\<`string`, `Readonly`\<\{ `name?`: `string`; `type`: `"value"` \| `"envVar"`; `value?`: `string`; \}\>\>

#### Inherited from

`ComponentDefinitionWithKind.env`

***

### kind

> **kind**: `"component"`

#### Inherited from

`TypedComponent`.[`kind`](#kind)

***

### modulePaths

> `readonly` **modulePaths**: `string`[]

#### Inherited from

`ComponentDefinitionWithKind.modulePaths`

***

### schema?

> `readonly` `optional` **schema?**: `Readonly`\<\{ `tables`: `Readonly`\<\{ `fields`: `Record`\<`string`, `JSONValue`\>; `indexes?`: `Readonly`\<\{ `fields`: ...[]; `name`: `string`; \}\>[]; `tableName`: `string`; \}\>[]; \}\>

#### Inherited from

`ComponentDefinitionWithKind.schema`

***

### sourceModulePath?

> `optional` **sourceModulePath?**: `string`

#### Inherited from

[`ComponentDefinitionWithKind`](ComponentDefinitionWithKind.md).[`sourceModulePath`](ComponentDefinitionWithKind.md#sourcemodulepath)
