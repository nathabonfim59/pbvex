[pbvex](../../index.md) / [server](../index.md) / Request

# Interface: Request

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

### method

> `readonly` **method**: `string`

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
