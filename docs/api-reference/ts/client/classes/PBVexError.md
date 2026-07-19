[@pbvex/client](../index.md) / PBVexError

# Class: PBVexError

## Extends

- `Error`

## Constructors

### Constructor

> **new PBVexError**(`structuredError`): `PBVexError`

#### Parameters

##### structuredError

`StructuredError`

#### Returns

`PBVexError`

#### Overrides

`Error.constructor`

## Properties

### code

> `readonly` **code**: `ErrorCode`

***

### data?

> `readonly` `optional` **data?**: [`PbvexValue`](../type-aliases/PbvexValue.md)

***

### details?

> `readonly` `optional` **details?**: `unknown`[]

***

### error

> `readonly` **error**: `true`

***

### message

> `readonly` **message**: `string`

#### Overrides

`Error.message`

***

### requestId?

> `readonly` `optional` **requestId?**: `string`

***

### structuredError

> `readonly` **structuredError**: `StructuredError`
