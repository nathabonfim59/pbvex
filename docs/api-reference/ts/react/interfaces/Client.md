[@pbvex/react](../index.md) / Client

# Interface: Client

## Extended by

- [`PBVexClient`](PBVexClient.md)

## Properties

### auth

> `readonly` **auth**: `AuthClient`

***

### authStore

> `readonly` **authStore**: `AuthStore`

## Accessors

### connectionState

#### Get Signature

> **get** **connectionState**(): `ConnectionState`

##### Returns

`ConnectionState`

## Methods

### action()

#### Call Signature

> **action**\<`Ref`\>(`ref`, ...`argsAndOptions`): `Promise`\<`FunctionReturnType`\<`Ref`\>\>

##### Type Parameters

###### Ref

`Ref` *extends* [`FunctionReference`](../type-aliases/FunctionReference.md)\<`"action"`, `any`, `any`, `"public"`\>

##### Parameters

###### ref

`Ref`

###### argsAndOptions

...`ArgsAndOptions`\<`Ref`, `CallOptions`\>

##### Returns

`Promise`\<`FunctionReturnType`\<`Ref`\>\>

#### Call Signature

> **action**\<`Args`, `Return`\>(`name`, `args?`, `options?`): `Promise`\<`Return`\>

##### Type Parameters

###### Args

`Args` = `any`

###### Return

`Return` = `any`

##### Parameters

###### name

`string`

###### args?

`Args`

###### options?

`CallOptions`

##### Returns

`Promise`\<`Return`\>

***

### clearAuth()

> **clearAuth**(): `void`

#### Returns

`void`

***

### close()

> **close**(): `void`

#### Returns

`void`

***

### mutation()

#### Call Signature

> **mutation**\<`Ref`\>(`ref`, ...`argsAndOptions`): `Promise`\<`FunctionReturnType`\<`Ref`\>\>

##### Type Parameters

###### Ref

`Ref` *extends* [`FunctionReference`](../type-aliases/FunctionReference.md)\<`"mutation"`, `any`, `any`, `"public"`\>

##### Parameters

###### ref

`Ref`

###### argsAndOptions

...`ArgsAndOptions`\<`Ref`, `CallOptions`\>

##### Returns

`Promise`\<`FunctionReturnType`\<`Ref`\>\>

#### Call Signature

> **mutation**\<`Args`, `Return`\>(`name`, `args?`, `options?`): `Promise`\<`Return`\>

##### Type Parameters

###### Args

`Args` = `any`

###### Return

`Return` = `any`

##### Parameters

###### name

`string`

###### args?

`Args`

###### options?

`CallOptions`

##### Returns

`Promise`\<`Return`\>

***

### query()

#### Call Signature

> **query**\<`Ref`\>(`ref`, ...`argsAndOptions`): `Promise`\<`FunctionReturnType`\<`Ref`\>\>

##### Type Parameters

###### Ref

`Ref` *extends* [`FunctionReference`](../type-aliases/FunctionReference.md)\<`"query"`, `any`, `any`, `"public"`\>

##### Parameters

###### ref

`Ref`

###### argsAndOptions

...`ArgsAndOptions`\<`Ref`, `CallOptions`\>

##### Returns

`Promise`\<`FunctionReturnType`\<`Ref`\>\>

#### Call Signature

> **query**\<`Args`, `Return`\>(`name`, `args?`, `options?`): `Promise`\<`Return`\>

##### Type Parameters

###### Args

`Args` = `any`

###### Return

`Return` = `any`

##### Parameters

###### name

`string`

###### args?

`Args`

###### options?

`CallOptions`

##### Returns

`Promise`\<`Return`\>

***

### resolveAuth()

> **resolveAuth**(`authOverride?`): `Promise`\<`string` \| `undefined`\>

#### Parameters

##### authOverride?

`string` \| `AuthProvider`

#### Returns

`Promise`\<`string` \| `undefined`\>

***

### setAuth()

> **setAuth**(`value`): `void`

#### Parameters

##### value

`string` \| `AuthProvider`

#### Returns

`void`

***

### watch()

#### Call Signature

> **watch**\<`Ref`\>(`ref`, ...`argsAndOptions`): `Unsubscribe`

##### Type Parameters

###### Ref

`Ref` *extends* [`FunctionReference`](../type-aliases/FunctionReference.md)\<`"query"`, `any`, `any`, `"public"`\>

##### Parameters

###### ref

`Ref`

###### argsAndOptions

...`ArgsAndOptions`\<`Ref`, `WatchOptions`\<`FunctionReturnType`\<`Ref`\>\>\>

##### Returns

`Unsubscribe`

#### Call Signature

> **watch**\<`Return`\>(`name`, `args?`, `options?`): `Unsubscribe`

##### Type Parameters

###### Return

`Return` = `any`

##### Parameters

###### name

`string`

###### args?

`unknown`

###### options?

`WatchOptions`\<`Return`\>

##### Returns

`Unsubscribe`
