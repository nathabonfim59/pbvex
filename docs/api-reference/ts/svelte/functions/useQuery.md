[@pbvex/svelte](../index.md) / useQuery

# Function: useQuery()

> **useQuery**\<`Ref`\>(`ref`, ...`args`): [`QueryState`](../type-aliases/QueryState.md)\<`ReturnOf`\<`Ref`\>\>

Creates component-scoped reactive query state.

Pass a getter when arguments depend on reactive component state:
`useQuery(api.messages.list, () => ({ channel }))`.
The watch is replaced when the getter's value changes and is automatically
unsubscribed when the owning component is destroyed.

## Type Parameters

### Ref

`Ref` *extends* `FunctionReference`\<`"query"`, `any`, `any`\>

## Parameters

### ref

`Ref`

### args

...`UseQueryArgs`\<`ArgsOf`\<`Ref`\>\>

## Returns

[`QueryState`](../type-aliases/QueryState.md)\<`ReturnOf`\<`Ref`\>\>
