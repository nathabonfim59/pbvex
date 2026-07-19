[pbvex](../../index.md) / [server](../index.md) / paginationResultValidator

# Function: paginationResultValidator()

> **paginationResultValidator**\<`Item`\>(`itemValidator`): [`ObjectValidatorFor`](../../values/type-aliases/ObjectValidatorFor.md)\<\{ `continueCursor`: [`Validator`](../../values/type-aliases/Validator.md)\<`string`, `string`\>; `isDone`: [`Validator`](../../values/type-aliases/Validator.md)\<`boolean`, `boolean`\>; `page`: [`Validator`](../../values/type-aliases/Validator.md)\<`OutputOf`\<`Item`\>[], `Exclude`\<`InputOf`\<`Item`\>, `undefined`\>[]\>; \}\>

Builds the canonical closed validator for a PBVex pagination result.

## Type Parameters

### Item

`Item` *extends* [`Validator`](../../values/type-aliases/Validator.md)\<`any`, `any`\>

## Parameters

### itemValidator

`Item`

## Returns

[`ObjectValidatorFor`](../../values/type-aliases/ObjectValidatorFor.md)\<\{ `continueCursor`: [`Validator`](../../values/type-aliases/Validator.md)\<`string`, `string`\>; `isDone`: [`Validator`](../../values/type-aliases/Validator.md)\<`boolean`, `boolean`\>; `page`: [`Validator`](../../values/type-aliases/Validator.md)\<`OutputOf`\<`Item`\>[], `Exclude`\<`InputOf`\<`Item`\>, `undefined`\>[]\>; \}\>
