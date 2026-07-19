[pbvex](../../index.md) / [server](../index.md) / ApplicationError

# Class: ApplicationError\<Data\>

A safe, handler-authored error whose category determines its HTTP status.

## Extends

- `Error`

## Type Parameters

### Data

`Data` *extends* `PbvexValue` = `PbvexValue`

## Constructors

### Constructor

> **new ApplicationError**\<`Data`\>(`category`, `data?`): `ApplicationError`\<`Data`\>

#### Parameters

##### category

[`ApplicationErrorCategory`](../type-aliases/ApplicationErrorCategory.md)

##### data?

`Data`

#### Returns

`ApplicationError`\<`Data`\>

#### Overrides

`Error.constructor`

## Properties

### category

> `readonly` **category**: [`ApplicationErrorCategory`](../type-aliases/ApplicationErrorCategory.md)

***

### data

> `readonly` **data**: `Data` \| `undefined`
