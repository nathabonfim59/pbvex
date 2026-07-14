[pbvex](../../index.md) / [server](../index.md) / defineComponentFns

# Function: defineComponentFns()

> **defineComponentFns**\<`C`\>(`component`): `object`

Creates function factories whose contexts are typed from a component definition.

## Type Parameters

### C

`C` *extends* [`TypedComponent`](../../component/interfaces/TypedComponent.md)\<`any`, `any`, `any`\>

## Parameters

### component

`C`

## Returns

`object`

### action

> **action**: \<`Returns`\>(`options`) => [`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`ActionCtx`](../interfaces/ActionCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Type Parameters

##### Returns

`Returns` = `any`

#### Parameters

##### options

[`FunctionOptions`](../interfaces/FunctionOptions.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`ActionCtx`](../interfaces/ActionCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Returns

[`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`ActionCtx`](../interfaces/ActionCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

### internalAction

> **internalAction**: \<`Returns`\>(`options`) => [`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`ActionCtx`](../interfaces/ActionCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Type Parameters

##### Returns

`Returns` = `any`

#### Parameters

##### options

[`FunctionOptions`](../interfaces/FunctionOptions.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`ActionCtx`](../interfaces/ActionCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Returns

[`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`ActionCtx`](../interfaces/ActionCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

### internalMutation

> **internalMutation**: \<`Returns`\>(`options`) => [`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`MutationCtx`](../interfaces/MutationCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Type Parameters

##### Returns

`Returns` = `any`

#### Parameters

##### options

[`FunctionOptions`](../interfaces/FunctionOptions.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`MutationCtx`](../interfaces/MutationCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Returns

[`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`MutationCtx`](../interfaces/MutationCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

### internalQuery

> **internalQuery**: \<`Returns`\>(`options`) => [`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`QueryCtx`](../interfaces/QueryCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Type Parameters

##### Returns

`Returns` = `any`

#### Parameters

##### options

[`FunctionOptions`](../interfaces/FunctionOptions.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`QueryCtx`](../interfaces/QueryCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Returns

[`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`QueryCtx`](../interfaces/QueryCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

### mutation

> **mutation**: \<`Returns`\>(`options`) => [`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`MutationCtx`](../interfaces/MutationCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Type Parameters

##### Returns

`Returns` = `any`

#### Parameters

##### options

[`FunctionOptions`](../interfaces/FunctionOptions.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`MutationCtx`](../interfaces/MutationCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Returns

[`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`MutationCtx`](../interfaces/MutationCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

### query

> **query**: \<`Returns`\>(`options`) => [`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`QueryCtx`](../interfaces/QueryCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Type Parameters

##### Returns

`Returns` = `any`

#### Parameters

##### options

[`FunctionOptions`](../interfaces/FunctionOptions.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`QueryCtx`](../interfaces/QueryCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>

#### Returns

[`FunctionDefinition`](../interfaces/FunctionDefinition.md)\<`Record`\<`string`, `never`\>, `Returns`, `ComponentCtx`\<`C`, [`QueryCtx`](../interfaces/QueryCtx.md)\<[`GenericDataModel`](../type-aliases/GenericDataModel.md)\>\>\>
