[@pbvex/protocol](../index.md) / ArgsAndOptions

# Type Alias: ArgsAndOptions\<FuncRef, Options\>

> **ArgsAndOptions**\<`FuncRef`, `Options`\> = `IsAny`\<[`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\>\> *extends* `true` ? \[`any`, `Options`\] : `IsEmptyArgs`\<[`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\>\> *extends* `true` ? \[`Options`\] : `object` *extends* [`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\> ? \[[`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\>, `Options`\] : \[[`FunctionArgs`](FunctionArgs.md)\<`FuncRef`\>, `Options`\]

Tuple of function args and call options for typed overloads.

## Type Parameters

### FuncRef

`FuncRef` *extends* [`FunctionReference`](FunctionReference.md)\<`any`, `any`, `any`, `any`\>

### Options

`Options` = `Record`\<`string`, `never`\>
