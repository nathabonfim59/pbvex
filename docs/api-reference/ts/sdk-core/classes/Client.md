[@pbvex/sdk-core](../index.md) / Client

# Class: Client

## Extended by

- [`PBVexClient`](PBVexClient.md)

## Constructors

### Constructor

> **new Client**(`url`, `options?`): `Client`

#### Parameters

##### url

`string` \| `URL`

##### options?

[`ClientOptions`](../interfaces/ClientOptions.md) = `{}`

#### Returns

`Client`

## Accessors

### connectionState

#### Get Signature

> **get** **connectionState**(): [`ConnectionState`](../type-aliases/ConnectionState.md)

##### Returns

[`ConnectionState`](../type-aliases/ConnectionState.md)

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

...`ArgsAndOptions`\<`Ref`, [`CallOptions`](../interfaces/CallOptions.md)\>

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

[`CallOptions`](../interfaces/CallOptions.md)

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

...`ArgsAndOptions`\<`Ref`, [`CallOptions`](../interfaces/CallOptions.md)\>

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

[`CallOptions`](../interfaces/CallOptions.md)

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

...`ArgsAndOptions`\<`Ref`, [`CallOptions`](../interfaces/CallOptions.md)\>

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

[`CallOptions`](../interfaces/CallOptions.md)

##### Returns

`Promise`\<`Return`\>

***

### resolveAuth()

> **resolveAuth**(`authOverride?`): `Promise`\<`string` \| `undefined`\>

#### Parameters

##### authOverride?

`string` \| [`AuthProvider`](../type-aliases/AuthProvider.md)

#### Returns

`Promise`\<`string` \| `undefined`\>

***

### setAuth()

> **setAuth**(`value`): `void`

#### Parameters

##### value

`string` \| [`AuthProvider`](../type-aliases/AuthProvider.md)

#### Returns

`void`

***

### watch()

#### Call Signature

> **watch**\<`Ref`\>(`ref`, ...`argsAndOptions`): [`Unsubscribe`](../type-aliases/Unsubscribe.md)

##### Type Parameters

###### Ref

`Ref` *extends* [`FunctionReference`](../type-aliases/FunctionReference.md)\<`"query"`, `any`, `any`, `"public"`\>

##### Parameters

###### ref

`Ref`

###### argsAndOptions

...`ArgsAndOptions`\<`Ref`, [`WatchOptions`](../interfaces/WatchOptions.md)\<`FunctionReturnType`\<`Ref`\>\>\>

##### Returns

[`Unsubscribe`](../type-aliases/Unsubscribe.md)

#### Call Signature

> **watch**\<`Return`\>(`name`, `args?`, `options?`): [`Unsubscribe`](../type-aliases/Unsubscribe.md)

##### Type Parameters

###### Return

`Return` = `any`

##### Parameters

###### name

`string`

###### args?

`unknown`

###### options?

[`WatchOptions`](../interfaces/WatchOptions.md)\<`Return`\>

##### Returns

[`Unsubscribe`](../type-aliases/Unsubscribe.md)
