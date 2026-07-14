[@pbvex/protocol](../index.md) / OptionalRestArgs

# Type Alias: OptionalRestArgs\<FuncRef\>

> **OptionalRestArgs**\<`FuncRef`\> = `IsAny`\<[`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\>\> *extends* `true` ? \[`any`\] : \[[`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\>\] *extends* \[`undefined` \| `void`\] ? \[\] : [`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\> *extends* [`EmptyObject`](EmptyObject.md) ? \[[`EmptyObject`](EmptyObject.md)\] : `object` *extends* [`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\> ? \[[`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\>\] : \[[`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\>\]

Tuple of arguments for a FunctionReference suitable for a rest parameter.

- void/undefined args take no slot.
- Empty/all-optional object args may be omitted or supplied as an object.
- Required args must be supplied exactly once.

## Type Parameters

### FuncRef

`FuncRef` *extends* [`FunctionReference`](FunctionReference.md)\<`any`, `any`, `any`, `any`\>
