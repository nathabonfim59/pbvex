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

> **generateUploadUrl**: () => `Promise`\<`string`\>

#### Returns

`Promise`\<`string`\>

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
