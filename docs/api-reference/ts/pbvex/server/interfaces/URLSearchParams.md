[pbvex](../../index.md) / [server](../index.md) / URLSearchParams

# Interface: URLSearchParams

## Methods

### \[iterator\]()

> **\[iterator\]**(): `IterableIterator`\<\[`string`, `string`\]\>

#### Returns

`IterableIterator`\<\[`string`, `string`\]\>

***

### append()

> **append**(`name`, `value`): `void`

#### Parameters

##### name

`string`

##### value

`string`

#### Returns

`void`

***

### delete()

> **delete**(`name`): `void`

#### Parameters

##### name

`string`

#### Returns

`void`

***

### entries()

> **entries**(): `IterableIterator`\<\[`string`, `string`\]\>

#### Returns

`IterableIterator`\<\[`string`, `string`\]\>

***

### forEach()

> **forEach**(`callback`, `thisArg?`): `void`

#### Parameters

##### callback

(`value`, `name`, `params`) => `void`

##### thisArg?

`any`

#### Returns

`void`

***

### get()

> **get**(`name`): `string` \| `null`

#### Parameters

##### name

`string`

#### Returns

`string` \| `null`

***

### getAll()

> **getAll**(`name`): `string`[]

#### Parameters

##### name

`string`

#### Returns

`string`[]

***

### has()

> **has**(`name`): `boolean`

#### Parameters

##### name

`string`

#### Returns

`boolean`

***

### keys()

> **keys**(): `IterableIterator`\<`string`\>

#### Returns

`IterableIterator`\<`string`\>

***

### set()

> **set**(`name`, `value`): `void`

#### Parameters

##### name

`string`

##### value

`string`

#### Returns

`void`

***

### toString()

> **toString**(): `string`

#### Returns

`string`

***

### values()

> **values**(): `IterableIterator`\<`string`\>

#### Returns

`IterableIterator`\<`string`\>
