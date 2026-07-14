[pbvex](../../index.md) / [server](../index.md) / MutationDef

# Interface: MutationDef\<Args, Returns, Ctx\>

## Extends

- [`FunctionDefinition`](FunctionDefinition.md)\<`Args`, `Returns`, `Ctx`\>

## Type Parameters

### Args

`Args`

### Returns

`Returns`

### Ctx

`Ctx`

## Properties

### args

> **args**: [`Validator`](../../values/type-aliases/Validator.md)\<`Args`\>

#### Inherited from

[`FunctionDefinition`](FunctionDefinition.md).[`args`](FunctionDefinition.md#args-1)

***

### exportName

> **exportName**: `string`

#### Inherited from

[`FunctionDefinition`](FunctionDefinition.md).[`exportName`](FunctionDefinition.md#exportname)

***

### handler

> **handler**: (`ctx`, `args`) => `Returns` \| `Promise`\<`Returns`\>

#### Parameters

##### ctx

`Ctx`

##### args

`Args`

#### Returns

`Returns` \| `Promise`\<`Returns`\>

#### Inherited from

[`FunctionDefinition`](FunctionDefinition.md).[`handler`](FunctionDefinition.md#handler)

***

### modulePath

> **modulePath**: `string`

#### Inherited from

[`FunctionDefinition`](FunctionDefinition.md).[`modulePath`](FunctionDefinition.md#modulepath)

***

### name?

> `optional` **name?**: `string`

#### Inherited from

[`FunctionDefinition`](FunctionDefinition.md).[`name`](FunctionDefinition.md#name)

***

### returns

> **returns**: [`Validator`](../../values/type-aliases/Validator.md)\<`Returns`\>

#### Inherited from

[`FunctionDefinition`](FunctionDefinition.md).[`returns`](FunctionDefinition.md#returns-1)

***

### route?

> `optional` **route?**: `Readonly`\<\{ `method`: `string`; `path?`: `string`; `pathPrefix?`: `string`; \}\>

#### Inherited from

[`FunctionDefinition`](FunctionDefinition.md).[`route`](FunctionDefinition.md#route)

***

### type

> **type**: `"mutation"`

#### Overrides

[`FunctionDefinition`](FunctionDefinition.md).[`type`](FunctionDefinition.md#type)

***

### visibility

> **visibility**: [`Visibility`](../type-aliases/Visibility.md)

#### Inherited from

[`FunctionDefinition`](FunctionDefinition.md).[`visibility`](FunctionDefinition.md#visibility)
