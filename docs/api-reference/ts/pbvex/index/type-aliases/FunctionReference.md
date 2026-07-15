[pbvex](../../index.md) / [index](../index.md) / FunctionReference

# Type Alias: FunctionReference\<Type, Args, Return, Visibility\>

> **FunctionReference**\<`Type`, `Args`, `Return`, `Visibility`\> = `object` & `NoArgsDiscriminator`\<`Args`\>

A shared reference to a registered PBVex function.

## Type Declaration

### \_\_args?

> `optional` **\_\_args?**: `Args`

### \_\_return?

> `optional` **\_\_return?**: `Return`

### \_path

> **\_path**: `string`

### \_type

> **\_type**: `Type`

### \_visibility

> **\_visibility**: `Visibility`

## Type Parameters

### Type

`Type` *extends* [`FunctionType`](FunctionType.md)

The function kind (`query`, `mutation`, `action`, `httpAction`).

### Args

`Args` = `any`

The (object) arguments to the function.

### Return

`Return` = `any`

The return type of the function.

### Visibility

`Visibility` *extends* [`FunctionVisibility`](FunctionVisibility.md) = `"public"`

Whether the function is `public` or `internal`.
