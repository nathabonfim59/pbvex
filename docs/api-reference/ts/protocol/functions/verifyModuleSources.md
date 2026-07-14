[@pbvex/protocol](../index.md) / verifyModuleSources

# Function: verifyModuleSources()

> **verifyModuleSources**(`modules`, `graph?`): `Promise`\<`void`\>

## Parameters

### modules

`Readonly`\<\{ `bytes`: `string`; `path`: `string`; \}\>[]

### graph?

`Readonly`\<\{ `definitions`: `Readonly`\<\{ `args?`: [`JSONValue`](../type-aliases/JSONValue.md); `componentId`: `string`; `dependencies?`: `string`[]; `env?`: `Record`\<`string`, `Readonly`\<\{ `name?`: ... \| ...; `type`: ... \| ...; `value?`: ... \| ...; \}\>\>; `moduleHashes?`: `Record`\<`string`, `string`\>; `modulePaths`: `string`[]; `schema?`: `Readonly`\<\{ `tables`: `Readonly`\<...\>[]; \}\>; \}\>[]; `mounts`: `Readonly`\<\{ `args?`: [`JSONValue`](../type-aliases/JSONValue.md); `children?`: Readonly\<\{ name: string; componentId: string; args?: JSONValue \| undefined; children?: Readonly\<...\>\[\] \| undefined; \}\>\[\]; `componentId`: `string`; `name`: `string`; \}\>[]; \}\>

## Returns

`Promise`\<`void`\>
