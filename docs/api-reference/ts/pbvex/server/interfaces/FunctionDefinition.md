[pbvex](../../index.md) / [server](../index.md) / FunctionDefinition

# Interface: FunctionDefinition\<Args, Returns, Ctx\>

## Extended by

- [`QueryDef`](QueryDef.md)
- [`MutationDef`](MutationDef.md)
- [`ActionDef`](ActionDef.md)
- [`HttpActionDef`](HttpActionDef.md)

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

***

### exportName

> **exportName**: `string`

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

***

### modulePath

> **modulePath**: `string`

***

### name?

> `optional` **name?**: `string`

***

### returns

> **returns**: [`Validator`](../../values/type-aliases/Validator.md)\<`Returns`\>

***

### route?

> `optional` **route?**: `Readonly`\<\{ `method`: `string`; `path?`: `string`; `pathPrefix?`: `string`; \}\>

***

### type

> **type**: [`FunctionType`](../../index/type-aliases/FunctionType.md)

***

### visibility

> **visibility**: [`Visibility`](../type-aliases/Visibility.md)
