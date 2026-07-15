[pbvex](../../index.md) / [server](../index.md) / SchedulerContext

# Interface: SchedulerContext

## Methods

### cancel()

> **cancel**(`id`): `Promise`\<`void`\>

#### Parameters

##### id

[`JobId`](../type-aliases/JobId.md)

#### Returns

`Promise`\<`void`\>

***

### runAfter()

> **runAfter**\<`Ref`\>(`delayMs`, `func`, ...`args`): `Promise`\<[`JobId`](../type-aliases/JobId.md)\>

#### Type Parameters

##### Ref

`Ref` *extends* [`SchedulableFunctionReference`](../type-aliases/SchedulableFunctionReference.md)\<`any`, `any`\>

#### Parameters

##### delayMs

`number`

##### func

`Ref`

##### args

...[`OptionalRestArgs`](../../index/type-aliases/OptionalRestArgs.md)\<`Ref`\>

#### Returns

`Promise`\<[`JobId`](../type-aliases/JobId.md)\>

***

### runAt()

> **runAt**\<`Ref`\>(`when`, `func`, ...`args`): `Promise`\<[`JobId`](../type-aliases/JobId.md)\>

#### Type Parameters

##### Ref

`Ref` *extends* [`SchedulableFunctionReference`](../type-aliases/SchedulableFunctionReference.md)\<`any`, `any`\>

#### Parameters

##### when

`number` \| `Date`

##### func

`Ref`

##### args

...[`OptionalRestArgs`](../../index/type-aliases/OptionalRestArgs.md)\<`Ref`\>

#### Returns

`Promise`\<[`JobId`](../type-aliases/JobId.md)\>
