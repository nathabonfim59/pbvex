[@pbvex/protocol](../index.md) / validateComponentFunctionBinding

# Function: validateComponentFunctionBinding()

> **validateComponentFunctionBinding**(`functions`, `graph?`): `void`

## Parameters

### functions

`Readonly`\<\{ `args?`: [`JSONValue`](../type-aliases/JSONValue.md); `exportName`: `string`; `modulePath`: `string`; `name`: `string`; `returns?`: [`JSONValue`](../type-aliases/JSONValue.md); `route?`: `Readonly`\<\{ `method`: `string`; `path?`: `string`; `pathPrefix?`: `string`; \}\>; `type`: [`FunctionType`](../type-aliases/FunctionType.md); `visibility`: [`FunctionVisibility`](../type-aliases/FunctionVisibility.md); \}\>[]

### graph?

`Readonly`\<\{ `definitions`: `Readonly`\<\{ `args?`: [`JSONValue`](../type-aliases/JSONValue.md); `componentId`: `string`; `dependencies?`: `string`[]; `env?`: `Record`\<`string`, `Readonly`\<\{ `name?`: ... \| ...; `type`: ... \| ...; `value?`: ... \| ...; \}\>\>; `moduleHashes?`: `Record`\<`string`, `string`\>; `modulePaths`: `string`[]; `schema?`: `Readonly`\<\{ `tables`: `Readonly`\<...\>[]; \}\>; \}\>[]; `mounts`: `Readonly`\<\{ `args?`: [`JSONValue`](../type-aliases/JSONValue.md); `children?`: Readonly\<\{ name: string; componentId: string; args?: JSONValue \| undefined; children?: Readonly\<...\>\[\] \| undefined; \}\>\[\]; `componentId`: `string`; `name`: `string`; \}\>[]; \}\>

## Returns

`void`
