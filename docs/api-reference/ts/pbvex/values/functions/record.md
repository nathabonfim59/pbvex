[pbvex](../../index.md) / [values](../index.md) / record

# Function: record()

> **record**\<`KOut`, `KIn`, `Value`\>(`key`, `value`): [`Validator`](../type-aliases/Validator.md)\<`Record`\<`KOut`, `OutputOf`\<`Value`\>\>, `Record`\<`KIn`, `Exclude`\<`InputOf`\<`Value`\>, `undefined`\>\>\>

## Type Parameters

### KOut

`KOut` *extends* `string`

### KIn

`KIn` *extends* `string`

### Value

`Value` *extends* [`Validator`](../type-aliases/Validator.md)\<`any`, `any`\>

## Parameters

### key

[`Validator`](../type-aliases/Validator.md)\<`KOut`, `KIn`\>

### value

`Value`

## Returns

[`Validator`](../type-aliases/Validator.md)\<`Record`\<`KOut`, `OutputOf`\<`Value`\>\>, `Record`\<`KIn`, `Exclude`\<`InputOf`\<`Value`\>, `undefined`\>\>\>
