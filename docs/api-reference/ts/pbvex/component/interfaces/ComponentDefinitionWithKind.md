[pbvex](../../index.md) / [component](../index.md) / ComponentDefinitionWithKind

# Interface: ComponentDefinitionWithKind

## Extends

- `Omit`\<`ComponentDefinition`, `"dependencies"` \| `"moduleHashes"`\>

## Extended by

- [`TypedComponent`](TypedComponent.md)

## Properties

### args?

> `readonly` `optional` **args?**: `JSONValue`

#### Inherited from

`Omit.args`

***

### componentId

> `readonly` **componentId**: `string`

#### Inherited from

`Omit.componentId`

***

### dependencies?

> `optional` **dependencies?**: `ComponentDefinitionWithKind`[]

***

### env?

> `readonly` `optional` **env?**: `Record`\<`string`, `Readonly`\<\{ `name?`: `string`; `type`: `"value"` \| `"envVar"`; `value?`: `string`; \}\>\>

#### Inherited from

`Omit.env`

***

### kind

> **kind**: `"component"`

***

### modulePaths

> `readonly` **modulePaths**: `string`[]

#### Inherited from

`Omit.modulePaths`

***

### schema?

> `readonly` `optional` **schema?**: `Readonly`\<\{ `tables`: `Readonly`\<\{ `fields`: `Record`\<`string`, `JSONValue`\>; `indexes?`: `Readonly`\<\{ `fields`: ...[]; `name`: `string`; \}\>[]; `tableName`: `string`; \}\>[]; \}\>

#### Inherited from

`Omit.schema`

***

### sourceModulePath?

> `optional` **sourceModulePath?**: `string`
