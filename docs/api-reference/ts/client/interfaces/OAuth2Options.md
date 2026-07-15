[@pbvex/client](../index.md) / OAuth2Options

# Interface: OAuth2Options

## Extends

- [`AuthRequestOptions`](AuthRequestOptions.md)

## Properties

### body?

> `optional` **body?**: `Record`\<`string`, `unknown`\>

#### Inherited from

[`AuthRequestOptions`](AuthRequestOptions.md).[`body`](AuthRequestOptions.md#body)

***

### createData?

> `optional` **createData?**: `Record`\<`string`, `unknown`\>

***

### headers?

> `optional` **headers?**: `Record`\<`string`, `string`\>

#### Inherited from

[`AuthRequestOptions`](AuthRequestOptions.md).[`headers`](AuthRequestOptions.md#headers)

***

### mfaId?

> `optional` **mfaId?**: `string`

PocketBase MFA session id returned by the first successful auth method.

#### Inherited from

[`AuthRequestOptions`](AuthRequestOptions.md).[`mfaId`](AuthRequestOptions.md#mfaid)

***

### provider

> **provider**: `string`

***

### query?

> `optional` **query?**: `Record`\<`string`, `string` \| `number` \| `boolean` \| `undefined`\>

#### Inherited from

[`AuthRequestOptions`](AuthRequestOptions.md).[`query`](AuthRequestOptions.md#query)

***

### scopes?

> `optional` **scopes?**: `string`[]

***

### signal?

> `optional` **signal?**: `AbortSignal`

#### Inherited from

[`AuthRequestOptions`](AuthRequestOptions.md).[`signal`](AuthRequestOptions.md#signal)

***

### urlCallback?

> `optional` **urlCallback?**: (`url`) => `void` \| `Promise`\<`void`\>

#### Parameters

##### url

`string`

#### Returns

`void` \| `Promise`\<`void`\>
