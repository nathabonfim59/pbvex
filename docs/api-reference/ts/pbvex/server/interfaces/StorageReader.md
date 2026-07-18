[pbvex](../../index.md) / [server](../index.md) / StorageReader

# Interface: StorageReader

## Extended by

- [`StorageContext`](StorageContext.md)

## Properties

### getMetadata

> **getMetadata**: (`id`) => `Promise`\<`Readonly`\<\{ `contentType`: `string`; `createdBy`: `string`; `extension`: `string`; `filename`: `string`; `kind`: `"file"`; `sha256`: `string`; `size`: `number`; `storageId`: [`StorageId`](../../index/type-aliases/StorageId.md); \}\> \| [`StorageImageMetadata`](../../index/type-aliases/StorageImageMetadata.md) \| `null`\>

#### Parameters

##### id

[`StorageId`](../../index/type-aliases/StorageId.md)

#### Returns

`Promise`\<`Readonly`\<\{ `contentType`: `string`; `createdBy`: `string`; `extension`: `string`; `filename`: `string`; `kind`: `"file"`; `sha256`: `string`; `size`: `number`; `storageId`: [`StorageId`](../../index/type-aliases/StorageId.md); \}\> \| [`StorageImageMetadata`](../../index/type-aliases/StorageImageMetadata.md) \| `null`\>

***

### getUrl

> **getUrl**: (`id`, `options?`) => `Promise`\<`string` \| `null`\>

#### Parameters

##### id

[`StorageId`](../../index/type-aliases/StorageId.md)

##### options?

[`StorageGetUrlOptions`](StorageGetUrlOptions.md)

#### Returns

`Promise`\<`string` \| `null`\>
