[pbvex](../../index.md) / [server](../index.md) / StorageContext

# Interface: StorageContext

## Extends

- [`StorageReader`](StorageReader.md)

## Properties

### delete

> **delete**: (`id`) => `Promise`\<`void`\>

#### Parameters

##### id

[`StorageId`](../../index/type-aliases/StorageId.md)

#### Returns

`Promise`\<`void`\>

***

### generateUploadUrl

> **generateUploadUrl**: (`options?`) => `Promise`\<`string`\>

#### Parameters

##### options?

[`StorageImageUploadOptions`](StorageImageUploadOptions.md)

#### Returns

`Promise`\<`string`\>

***

### getMetadata

> **getMetadata**: (`id`) => `Promise`\<`Readonly`\<\{ `contentType`: `string`; `createdBy`: `string`; `extension`: `string`; `filename`: `string`; `kind`: `"file"`; `sha256`: `string`; `size`: `number`; `storageId`: [`StorageId`](../../index/type-aliases/StorageId.md); \}\> \| [`StorageImageMetadata`](../../index/type-aliases/StorageImageMetadata.md) \| `null`\>

#### Parameters

##### id

[`StorageId`](../../index/type-aliases/StorageId.md)

#### Returns

`Promise`\<`Readonly`\<\{ `contentType`: `string`; `createdBy`: `string`; `extension`: `string`; `filename`: `string`; `kind`: `"file"`; `sha256`: `string`; `size`: `number`; `storageId`: [`StorageId`](../../index/type-aliases/StorageId.md); \}\> \| [`StorageImageMetadata`](../../index/type-aliases/StorageImageMetadata.md) \| `null`\>

#### Inherited from

[`StorageReader`](StorageReader.md).[`getMetadata`](StorageReader.md#getmetadata)

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

#### Inherited from

[`StorageReader`](StorageReader.md).[`getUrl`](StorageReader.md#geturl)
