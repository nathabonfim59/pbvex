[pbvex](../../index.md) / [server](../index.md) / Response

# Interface: Response

## Properties

### body

> `readonly` **body**: `Uint8Array`\<`ArrayBufferLike`\> \| `null`

***

### bodyUsed

> `readonly` **bodyUsed**: `boolean`

***

### headers

> `readonly` **headers**: [`Headers`](Headers.md)

***

### ok

> `readonly` **ok**: `boolean`

***

### status

> `readonly` **status**: `number`

***

### statusText

> `readonly` **statusText**: `string`

***

### type

> `readonly` **type**: `string`

***

### url

> `readonly` **url**: `string`

## Methods

### arrayBuffer()

> **arrayBuffer**(): `Promise`\<`ArrayBuffer`\>

#### Returns

`Promise`\<`ArrayBuffer`\>

***

### json()

> **json**(): `Promise`\<`any`\>

#### Returns

`Promise`\<`any`\>

***

### text()

> **text**(): `Promise`\<`string`\>

#### Returns

`Promise`\<`string`\>
