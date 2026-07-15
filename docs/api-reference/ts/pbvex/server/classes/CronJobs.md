[pbvex](../../index.md) / [server](../index.md) / CronJobs

# Class: CronJobs

## Implements

- [`CronJobsDefinition`](../interfaces/CronJobsDefinition.md)

## Constructors

### Constructor

> **new CronJobs**(): `CronJobs`

#### Returns

`CronJobs`

## Properties

### kind

> `readonly` **kind**: `"pbvex.cronJobs"`

#### Implementation of

[`CronJobsDefinition`](../interfaces/CronJobsDefinition.md).[`kind`](../interfaces/CronJobsDefinition.md#kind)

## Accessors

### jobs

#### Get Signature

> **get** **jobs**(): readonly [`CronJobDefinition`](../interfaces/CronJobDefinition.md)[]

##### Returns

readonly [`CronJobDefinition`](../interfaces/CronJobDefinition.md)[]

#### Implementation of

[`CronJobsDefinition`](../interfaces/CronJobsDefinition.md).[`jobs`](../interfaces/CronJobsDefinition.md#jobs)

## Methods

### cron()

> **cron**\<`Ref`\>(`name`, `schedule`, `func`, ...`args`): `this`

#### Type Parameters

##### Ref

`Ref` *extends* [`SchedulableFunctionReference`](../type-aliases/SchedulableFunctionReference.md)\<`any`, `any`\>

#### Parameters

##### name

`string`

##### schedule

`string`

##### func

`Ref`

##### args

...[`OptionalRestArgs`](../../index/type-aliases/OptionalRestArgs.md)\<`Ref`\>

#### Returns

`this`
