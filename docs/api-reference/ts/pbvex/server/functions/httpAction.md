[pbvex](../../index.md) / [server](../index.md) / httpAction

# Function: httpAction()

> **httpAction**\<`Args`, `Returns`, `DataModel`\>(`options`): [`HttpActionDef`](../interfaces/HttpActionDef.md)\<`Args`, `Returns`, [`HttpActionCtx`](../interfaces/HttpActionCtx.md)\<`DataModel`\>\>

## Type Parameters

### Args

`Args` *extends* [`Request`](../interfaces/Request.md) = [`Request`](../interfaces/Request.md)

### Returns

`Returns` *extends* [`Response`](../interfaces/Response.md) = [`Response`](../interfaces/Response.md)

### DataModel

`DataModel` *extends* [`GenericDataModel`](../type-aliases/GenericDataModel.md) = [`GenericDataModel`](../type-aliases/GenericDataModel.md)

## Parameters

### options

[`FunctionOptions`](../interfaces/FunctionOptions.md)\<`Args`, `Returns`, [`HttpActionCtx`](../interfaces/HttpActionCtx.md)\<`DataModel`\>\>

## Returns

[`HttpActionDef`](../interfaces/HttpActionDef.md)\<`Args`, `Returns`, [`HttpActionCtx`](../interfaces/HttpActionCtx.md)\<`DataModel`\>\>
