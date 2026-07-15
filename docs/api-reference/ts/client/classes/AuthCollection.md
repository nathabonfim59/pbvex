[@pbvex/client](../index.md) / AuthCollection

# Class: AuthCollection\<T\>

## Type Parameters

### T

`T` *extends* [`AuthRecord`](../type-aliases/AuthRecord.md) = [`AuthRecord`](../type-aliases/AuthRecord.md)

## Constructors

### Constructor

> **new AuthCollection**\<`T`\>(`name`, `request`, `store`, `oauth`): `AuthCollection`\<`T`\>

#### Parameters

##### name

`string`

##### request

[`AuthRequest`](../type-aliases/AuthRequest.md)

##### store

[`AuthStore`](AuthStore.md)\<`T`\>

##### oauth

(`collection`, `options`) => `Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

#### Returns

`AuthCollection`\<`T`\>

## Methods

### authRefresh()

> **authRefresh**(`options?`): `Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

#### Parameters

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

***

### authWithOAuth2()

> **authWithOAuth2**(`options`): `Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

#### Parameters

##### options

[`OAuth2Options`](../interfaces/OAuth2Options.md)

#### Returns

`Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

***

### authWithOAuth2Code()

> **authWithOAuth2Code**(`provider`, `code`, `codeVerifier`, `redirectURL`, `createData?`, `options?`): `Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

#### Parameters

##### provider

`string`

##### code

`string`

##### codeVerifier

`string`

##### redirectURL

`string`

##### createData?

`Record`\<`string`, `unknown`\>

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

***

### authWithOTP()

> **authWithOTP**(`otpId`, `password`, `options?`): `Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

#### Parameters

##### otpId

`string`

##### password

`string`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

***

### authWithPassword()

> **authWithPassword**(`identity`, `password`, `options?`): `Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

#### Parameters

##### identity

`string`

##### password

`string`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

***

### confirmEmailChange()

> **confirmEmailChange**(`token`, `password`, `options?`): `Promise`\<`true`\>

#### Parameters

##### token

`string`

##### password

`string`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<`true`\>

***

### confirmPasswordReset()

> **confirmPasswordReset**(`token`, `password`, `passwordConfirm`, `options?`): `Promise`\<`true`\>

#### Parameters

##### token

`string`

##### password

`string`

##### passwordConfirm

`string`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<`true`\>

***

### confirmVerification()

> **confirmVerification**(`token`, `options?`): `Promise`\<`true`\>

#### Parameters

##### token

`string`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<`true`\>

***

### impersonate()

> **impersonate**(`recordId`, `duration?`, `options?`): `Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

Returns impersonated auth data without replacing the current (usually superuser) store.

#### Parameters

##### recordId

`string`

##### duration?

`number` = `0`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<[`AuthResponse`](../interfaces/AuthResponse.md)\<`T`\>\>

***

### listAuthMethods()

> **listAuthMethods**(`options?`): `Promise`\<[`AuthMethodsList`](../interfaces/AuthMethodsList.md)\>

#### Parameters

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<[`AuthMethodsList`](../interfaces/AuthMethodsList.md)\>

***

### requestEmailChange()

> **requestEmailChange**(`newEmail`, `options?`): `Promise`\<`true`\>

#### Parameters

##### newEmail

`string`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<`true`\>

***

### requestOTP()

> **requestOTP**(`email`, `options?`): `Promise`\<[`OtpResponse`](../interfaces/OtpResponse.md)\>

#### Parameters

##### email

`string`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<[`OtpResponse`](../interfaces/OtpResponse.md)\>

***

### requestPasswordReset()

> **requestPasswordReset**(`email`, `options?`): `Promise`\<`true`\>

#### Parameters

##### email

`string`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<`true`\>

***

### requestVerification()

> **requestVerification**(`email`, `options?`): `Promise`\<`true`\>

#### Parameters

##### email

`string`

##### options?

[`AuthRequestOptions`](../interfaces/AuthRequestOptions.md)

#### Returns

`Promise`\<`true`\>
