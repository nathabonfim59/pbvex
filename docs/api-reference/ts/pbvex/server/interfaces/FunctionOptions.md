[pbvex](../../index.md) / [server](../index.md) / FunctionOptions

# Interface: FunctionOptions\<Args, Returns, Ctx\>

## Type Parameters

### Args

`Args`

### Returns

`Returns`

### Ctx

`Ctx`

## Properties

### args?

> `optional` **args?**: [`ArgDefinition`](../type-aliases/ArgDefinition.md)\<`Args`\>

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

### returns?

> `optional` **returns?**: [`ReturnDefinition`](../type-aliases/ReturnDefinition.md)\<`Returns`\>

***

### route?

> `optional` **route?**: `Readonly`\<\{ `method`: `string`; `path?`: `string`; `pathPrefix?`: `string`; \}\>
