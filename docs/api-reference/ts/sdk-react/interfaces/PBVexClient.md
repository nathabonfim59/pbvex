[@pbvex/sdk-react](../index.md) / PBVexClient

# Interface: PBVexClient

## Extends

- [`Client`](Client.md)

## Accessors

### connectionState

#### Get Signature

> **get** **connectionState**(): `ConnectionState`

##### Returns

`ConnectionState`

#### Inherited from

[`Client`](Client.md).[`connectionState`](Client.md#connectionstate)

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

##### Inherited from

[`Client`](Client.md).[`action`](Client.md#action)

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

##### Inherited from

[`Client`](Client.md).[`action`](Client.md#action)

***

### clearAuth()

> **clearAuth**(): `void`

#### Returns

`void`

#### Inherited from

[`Client`](Client.md).[`clearAuth`](Client.md#clearauth)

***

### close()

> **close**(): `void`

#### Returns

`void`

#### Inherited from

[`Client`](Client.md).[`close`](Client.md#close)

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

##### Inherited from

[`Client`](Client.md).[`mutation`](Client.md#mutation)

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

##### Inherited from

[`Client`](Client.md).[`mutation`](Client.md#mutation)

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

##### Inherited from

[`Client`](Client.md).[`query`](Client.md#query)

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

##### Inherited from

[`Client`](Client.md).[`query`](Client.md#query)

***

### resolveAuth()

> **resolveAuth**(`authOverride?`): `Promise`\<`string` \| `undefined`\>

#### Parameters

##### authOverride?

`string` \| `AuthProvider`

#### Returns

`Promise`\<`string` \| `undefined`\>

#### Inherited from

[`Client`](Client.md).[`resolveAuth`](Client.md#resolveauth)

***

### setAuth()

> **setAuth**(`value`): `void`

#### Parameters

##### value

`string` \| `AuthProvider`

#### Returns

`void`

#### Inherited from

[`Client`](Client.md).[`setAuth`](Client.md#setauth)

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

##### Inherited from

[`Client`](Client.md).[`watch`](Client.md#watch)

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

##### Inherited from

[`Client`](Client.md).[`watch`](Client.md#watch)
