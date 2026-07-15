[@pbvex/client](../index.md) / AuthApiError

# Class: AuthApiError

## Extends

- `Error`

## Constructors

### Constructor

> **new AuthApiError**(`status`, `response`, `url`): `AuthApiError`

#### Parameters

##### status

`number`

##### response

[`PocketBaseErrorBody`](../interfaces/PocketBaseErrorBody.md)

##### url

`string`

#### Returns

`AuthApiError`

#### Overrides

`Error.constructor`

## Properties

### response

> `readonly` **response**: [`PocketBaseErrorBody`](../interfaces/PocketBaseErrorBody.md)

***

### status

> `readonly` **status**: `number`

***

### url

> `readonly` **url**: `string`
