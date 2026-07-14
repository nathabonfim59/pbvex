[pbvex](../../index.md) / [component](../index.md) / mount

# Function: mount()

> **mount**\<`C`\>(`component`, `name`, ...`rest`): [`TypedMount`](../interfaces/TypedMount.md)

mount creates a TypedMount with compile-time args validation. The component
type C is matched exactly (not widened to a generic), and In is extracted
via conditional infer — so [In] extends [undefined] resolves correctly and
excess-property checks fire for no-args components.

## Type Parameters

### C

`C` *extends* [`TypedComponent`](../interfaces/TypedComponent.md)\<`any`, `any`, `any`\>

## Parameters

### component

`C`

### name

`string`

### rest

...`C` *extends* [`TypedComponent`](../interfaces/TypedComponent.md)\<`any`, `any`, `I`\> ? `MountRest`\<`I`\> : \[\]

## Returns

[`TypedMount`](../interfaces/TypedMount.md)
