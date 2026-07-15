[pbvex](../../index.md) / [component](../index.md) / buildComponentGraph

# Function: buildComponentGraph()

> **buildComponentGraph**(`components`, `app`, `functionModulePaths`, `modules`, `bundleSha`): `Promise`\<`Readonly`\<\{ `definitions`: `Readonly`\<\{ `args?`: `JSONValue`; `componentId`: `string`; `dependencies?`: `string`[]; `env?`: `Record`\<`string`, `Readonly`\<\{ `name?`: ...; `type`: ...; `value?`: ...; \}\>\>; `moduleHashes?`: `Record`\<`string`, `string`\>; `modulePaths`: `string`[]; `schema?`: `Readonly`\<\{ `tables`: ...[]; \}\>; \}\>[]; `mounts`: `Readonly`\<\{ `args?`: `JSONValue`; `children?`: Readonly\<\{ name: string; componentId: string; args?: JSONValue \| undefined; children?: Readonly\<...\>\[\] \| undefined; \}\>\[\]; `componentId`: `string`; `name`: `string`; \}\>[]; \}\> \| `undefined`\>

## Parameters

### components

[`ComponentDefinitionWithKind`](../interfaces/ComponentDefinitionWithKind.md)[]

### app

[`AppDefinition`](../interfaces/AppDefinition.md) \| `undefined`

### functionModulePaths

`string`[]

### modules

[`ComponentModule`](../interfaces/ComponentModule.md)[]

### bundleSha

`string`

## Returns

`Promise`\<`Readonly`\<\{ `definitions`: `Readonly`\<\{ `args?`: `JSONValue`; `componentId`: `string`; `dependencies?`: `string`[]; `env?`: `Record`\<`string`, `Readonly`\<\{ `name?`: ...; `type`: ...; `value?`: ...; \}\>\>; `moduleHashes?`: `Record`\<`string`, `string`\>; `modulePaths`: `string`[]; `schema?`: `Readonly`\<\{ `tables`: ...[]; \}\>; \}\>[]; `mounts`: `Readonly`\<\{ `args?`: `JSONValue`; `children?`: Readonly\<\{ name: string; componentId: string; args?: JSONValue \| undefined; children?: Readonly\<...\>\[\] \| undefined; \}\>\[\]; `componentId`: `string`; `name`: `string`; \}\>[]; \}\> \| `undefined`\>
