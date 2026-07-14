[@pbvex/sdk-core](../index.md) / IsOptionalArgs

# Type Alias: IsOptionalArgs\<T\>

> **IsOptionalArgs**\<`T`\> = [`IsAny`](IsAny.md)\<`T`\> *extends* `true` ? `false` : \[`T`\] *extends* \[`undefined` \| `void`\] ? `true` : `T` *extends* [`EmptyObject`](EmptyObject.md) ? `true` : `object` *extends* `T` ? `true` : `false`

## Type Parameters

### T

`T`
