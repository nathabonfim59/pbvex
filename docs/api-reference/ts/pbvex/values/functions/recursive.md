[pbvex](../../index.md) / [values](../index.md) / recursive

# Function: recursive()

> **recursive**\<`T`\>(`name`, `factory`): [`Validator`](../type-aliases/Validator.md)\<`T`\>

Declares a named, serializable recursive validator. The descriptor is
`{type:'recursive', name, validator}` where `validator` is the full inner
descriptor; cycle points inside it emit `{type:'ref', name}`. The backend
resolves refs against the enclosing recursive declaration, so recursive
types are genuinely executable (document insert/patch validation, not just
manifest acceptance).

## Type Parameters

### T

`T`

## Parameters

### name

`string`

### factory

() => [`Validator`](../type-aliases/Validator.md)\<`T`\>

## Returns

[`Validator`](../type-aliases/Validator.md)\<`T`\>
