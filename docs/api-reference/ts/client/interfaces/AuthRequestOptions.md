[@pbvex/client](../index.md) / AuthRequestOptions

# Interface: AuthRequestOptions

## Extended by

- [`OAuth2Options`](OAuth2Options.md)

## Properties

### body?

> `optional` **body?**: `Record`\<`string`, `unknown`\>

***

### headers?

> `optional` **headers?**: `Record`\<`string`, `string`\>

***

### mfaId?

> `optional` **mfaId?**: `string`

PocketBase MFA session id returned by the first successful auth method.

***

### query?

> `optional` **query?**: `Record`\<`string`, `string` \| `number` \| `boolean` \| `undefined`\>

***

### signal?

> `optional` **signal?**: `AbortSignal`
