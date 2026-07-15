# Data types and validation

PBVex validators describe both a TypeScript type and a runtime boundary. Import the validator namespace from `pbvex/values`:

```ts
import { v } from 'pbvex/values';
```

Use the same validators for function arguments and returns, schema fields, and component arguments. Their descriptors are included in the deployment manifest, checked when a deployment is activated, and evaluated again when data crosses a PBVex boundary. This guide describes behavior implemented by the current PBVex source; configurable and fixed limits are called out separately.

## Quick example

```ts
import { mutation } from './_generated/server';
import { v } from 'pbvex/values';

export const create = mutation({
  args: {
    title: v.string(),
    priority: v.defaulted(v.number(), 0),
    labels: v.optional(v.array(v.string())),
  },
  returns: v.object({ id: v.id('tasks') }),
  handler: async (ctx, args) => {
    const id = await ctx.db.insert('tasks', args);
    return { id };
  },
});
```

Here `args` is inferred as `{ title: string; priority: number; labels?: string[] }`, and the return is inferred as `{ id: Id<'tasks'> }`. `priority` may be omitted by the caller but is always a `number` inside the handler because the default is applied. `labels` remains optional.

## Validator reference

All entries below are methods of `v` imported from `pbvex/values`. `Validator<Out, In>` is the public validator type: `Out` is the normalized/output type and `In` is the accepted TypeScript input type. Calling `validator.validate(value)` either returns `Out` or throws `ValidationError`.

| API | TypeScript value / inferred type | Runtime and wire constraints |
| --- | --- | --- |
| `v.string()` | `string` | Any JavaScript string. |
| `v.number()` | `number` | A finite JavaScript number. Integers and fractional values are both allowed; `NaN`, `Infinity`, and `-Infinity` are rejected. |
| `v.float64()` | `number` | Also a finite JavaScript number. It has the same runtime acceptance as `v.number()` and emits a distinct `float64` descriptor. |
| `v.int64()` | `bigint` | A `bigint` from `-9223372036854775808n` through `9223372036854775807n`, inclusive. It is encoded as the PBVex `$integer` wire form. |
| `v.bigint()` | `bigint` | Exact alias for `v.int64()`; use it when the TypeScript name is clearer. |
| `v.boolean()` | `boolean` | Only `true` or `false`. |
| `v.null()` | `null` | Only `null`; it does not accept `undefined`. |
| `v.bytes()` | `ArrayBuffer` | Only an `ArrayBuffer`, encoded as PBVex `$bytes` base64 on the wire. A `Uint8Array` is not itself accepted; pass its `.buffer` when that is the intended value. |
| `v.id('table')` | `GenericId<'table'>` / server `Id<'table'>` | An authenticated opaque ID for exactly that logical table. The table name must be a valid identifier. It is not an arbitrary string and cannot be created client-side. |
| `v.literal(value)` | the literal type of `value` | `value` may be a `string`, finite `number`, `bigint`, or `boolean`. Equality is exact; non-finite numeric literals are rejected. |
| `v.array(item)` | `Out[]` | A JavaScript array whose every item validates with `item`. An omitted/`undefined` item is not allowed unless the item validator accepts it. |
| `v.object(shape)` | object mapped from `shape` | A constrained, non-array object with the declared fields. Required fields must be present; unknown fields are not retained locally and are rejected at the persisted/runtime document boundary. |
| `v.record(key, value)` | `Record<KeyOut, ValueOut>` | A non-array object with arbitrary declared keys and values. The serialized key validator must be `v.string()`, a string `v.literal(...)`, or a union of those; keys must normalize to strings. |
| `v.union(a, b, ...)` | union of each branch's types | Tries validators in declaration order and succeeds on the first match. A deployable union needs at least one branch and has a fixed maximum of 64 branches. |
| `v.optional(child)` | `Out \| undefined` | Accepts omitted/`undefined`; in an object shape it makes that property optional. It does not mean `null`. |
| `v.defaulted(child, value)` | `Out` (input `In \| undefined`) | Accepts omission and substitutes a child-valid default. The default is eagerly validated and normalized, then serialized in the descriptor. |
| `v.recursive(name, factory)` | `T` | A named, serializable recursive validator. `name` must be an identifier; recursion is represented by a descriptor reference and remains subject to value/depth budgets. |
| `v.delayed(factory)` | `T` | Defers construction for local `validate` calls, but cannot be serialized into a deployment manifest. Do not use it in function args/returns, schemas, or component args. |
| `v.any()` | `any` | Accepts any *PBVex wire value* at a runtime boundary. It is not an escape hatch for `Date`, functions, `undefined`, cyclic objects, or non-finite numbers. |

There is no exported `v.unknown()` validator. Use a precise validator whenever possible; use `v.any()` only when the wire-value contract is intentionally unconstrained.

## Primitive and opaque values

### Numbers, integers, and floats

`v.number()` and `v.float64()` both require `Number.isFinite(value)`. Neither is an integer-only validator:

```ts
v.number().validate(2);       // number
v.number().validate(2.5);     // number
v.float64().validate(-0.25);  // number

v.number().validate(NaN);       // throws ValidationError
v.float64().validate(Infinity); // throws ValidationError
```

Use `v.int64()`/`v.bigint()` for an exact signed 64-bit integer, not a JavaScript `number`:

```ts
const balance = v.int64().validate(9007199254740993n);
// balance: bigint

v.int64().validate(1);                    // throws: number is not bigint
v.int64().validate(9223372036854775808n); // throws: out of range
```

PBVex intentionally does not make `NaN` or either infinity a data value. It does preserve finite JavaScript numbers, and represents `bigint` using a tagged wire object rather than ordinary JSON's nonexistent bigint representation.

### Bytes and IDs

```ts
const image = v.bytes().validate(new Uint8Array([137, 80, 78, 71]).buffer);

const taskId = v.id('tasks');
// In a handler after receiving a real ID:
const id = taskId.validate(someIdString); // GenericId<'tasks'>
```

`v.id('tasks')` checks the ID's authenticated encoding and table binding. Its TypeScript brand prevents accidentally passing an `Id<'users'>` where `Id<'tasks'>` is required, and its runtime check prevents accepting a syntactically plausible ID for another table or namespace. Treat IDs as opaque capabilities: store and pass them unchanged. Do not parse, derive, or forge them. `ctx.db.normalizeId('tasks', raw)` is the database API for converting an untyped string to a typed ID or `null`; see [schema and database](./schema-and-database.md#crud-and-queries).

### Literals, null, and booleans

Literal validators are especially useful for discriminated data:

```ts
const status = v.union(
  v.literal('draft'),
  v.literal('published'),
  v.literal('archived'),
);
// 'draft' | 'published' | 'archived'

const published = v.object({
  state: v.literal('published'),
  publishedAt: v.number(),
});

const maybeDeleted = v.union(v.null(), v.object({ reason: v.string() }));
```

Use `v.null()` for a present null value and `v.optional(...)` for absence. They have different TypeScript and runtime meaning.

## Structured values

### Objects and optional/defaulted fields

`v.object` maps a field-validator shape to an object type. A child `v.optional` makes an input property optional. A `v.defaulted` property is optional to supply, but required after validation:

```ts
const settings = v.object({
  theme: v.union(v.literal('light'), v.literal('dark')),
  locale: v.optional(v.string()),
  pageSize: v.defaulted(v.number(), 20),
});

const value = settings.validate({ theme: 'dark' });
// { theme: 'light' | 'dark'; locale?: string; pageSize: number }
// value.pageSize is 20
```

An object validator validates the declared shape. Do not rely on excess properties being preserved: its local implementation returns the declared keys, and server document validation rejects unknown fields. Model open-ended maps with `v.record`, not `v.object`.

`v.optional` only special-cases `undefined`. It does not validate `undefined` inside an array as a wire value:

```ts
v.object({ note: v.optional(v.string()) }).validate({}); // valid
v.array(v.optional(v.string())).validate(['a', undefined]); // local validator accepts it
// But undefined is not a PBVex wire value; do not send this across a boundary.
```

In normal function argument objects and schema documents, omitted optional fields are the portable representation. A defaulted field is normalized to its default when omitted; an optional field remains absent on the wire. Default values must already satisfy their child validator, including the `int64` range and bytes requirements.

### Arrays, records, and unions

```ts
const task = v.object({
  title: v.string(),
  tags: v.array(v.string()),
  counters: v.record(v.string(), v.int64()),
  visibility: v.union(v.literal('team'), v.literal('private')),
});

task.validate({
  title: 'Write guide',
  tags: ['docs', 'types'],
  counters: { views: 3n },
  visibility: 'team',
});
```

Records are objects, not `Map`s. Their keys arrive as strings. At deployment time, PBVex limits deployable record-key validators to string/string-literal unions so that serialized object keys remain meaningful. A union reports a combined validation failure when no branch accepts the value; place specific branches before broad ones such as `v.any()`.

### Recursive values

Use `v.recursive` for deployable trees and other self-referential shapes:

```ts
import type { Validator } from 'pbvex/values';

type Node = { name: string; children: Node[] };

let node!: Validator<Node>;
node = v.recursive<Node>('Node', () =>
  v.object({
    name: v.string(),
    children: v.array(node),
  }),
);
```

For practical TypeScript code, it is often clearer to declare the recursive TypeScript type first and cast the recursive binding only as needed by TypeScript's initialization rules. The important runtime distinction is that `v.recursive` emits `{ type: 'recursive', ... }` and internal `{ type: 'ref', ... }` descriptors that the backend evaluates. `v.delayed` also breaks an initialization cycle locally, but its closure has no deployment representation and `toJSON()` throws.

## Inference and function boundaries

Function factories accept either an object of field validators or a single validator. An object shorthand is normalized to `v.object(...)`; omitted `args` becomes an empty object validator, while omitted `returns` becomes `v.any()`.

```ts
import { query } from './_generated/server';
import { v } from 'pbvex/values';

export const find = query({
  args: v.object({
    id: v.id('tasks'),
    includeHistory: v.defaulted(v.boolean(), false),
  }),
  returns: v.union(v.null(), v.object({ title: v.string() })),
  handler: async (ctx, args) => {
    // args.id: Id<'tasks'>
    // args.includeHistory: boolean
    const doc = await ctx.db.get(args.id);
    return doc === null ? null : { title: doc.title };
  },
});
```

The declared validators are not merely compile-time hints:

- The bundler serializes their descriptors into the function manifest. Code generation uses those descriptors to produce typed public and internal references.
- Deployment validation rejects malformed or non-serializable descriptors before activation. `v.delayed` therefore fails during bundling/deployment rather than becoming a server-side surprise.
- A public client call, a nested `ctx.runQuery`/`ctx.runMutation`/`ctx.runAction` call, and a scheduled invocation all cross a wire boundary. The backend decodes and validates arguments before running the handler, applying defaults.
- The handler result is encoded and validated against `returns`. An invalid return rejects the invocation; for a top-level mutation, an error/timeout/cancellation/invalid return rolls its database transaction back. See [backend functions](./functions.md#validators-and-returns).

Generated references carry the inferred argument and return types. Use them with `@pbvex/client`; `@pbvex/react` hooks and `@pbvex/svelte` rune utilities consume the same references. Passing a raw string function path bypasses TypeScript inference, not server validation.

An argument object whose every property is optional can be omitted in a typed client or nested call. A function with no declared args accepts `{}`; PBVex uses generated reference metadata to distinguish omitted arguments from client call options.

## Schemas, documents, and indexes

Define schema fields with the same validators:

```ts
// pbvex/schema.ts
import { defineSchema, defineTable } from 'pbvex/server';
import { v } from 'pbvex/values';

export default defineSchema({
  tasks: defineTable({
    title: v.string(),
    ownerId: v.id('users'),
    state: v.defaulted(v.union(v.literal('open'), v.literal('done')), 'open'),
    metadata: v.optional(v.object({ project: v.string(), rank: v.number() })),
  }).index('by_owner_state', ['ownerId', 'state'])
    .index('by_project', ['metadata.project']),
});
```

A document has two server-provided system fields in addition to schema fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `_id` | `Id<'table'>` | Authenticated opaque ID bound to the document's logical table and namespace. |
| `_creationTime` | `number` | Server-set creation timestamp. |

Do not declare, insert, patch, or replace `_id` or `_creationTime`; PBVex treats top-level system fields as immutable. `_pbvex_`-prefixed top-level schema field names are also rejected by the server. Nested object keys are ordinary data keys, subject to safe field-name rules.

Database write types reflect this rule:

- `ctx.db.insert('tasks', value)` accepts all user insert fields and returns `Id<'tasks'>`. Required fields must be provided; omitted `v.defaulted` fields are materialized; omitted `v.optional` fields remain absent.
- `ctx.db.patch(id, value)` accepts a partial set of user fields. It validates only supplied fields and does not use a default for a field that was omitted from the patch.
- `ctx.db.replace(id, value)` accepts the insert shape, so required fields must be supplied and defaults may be applied. It replaces user fields, never system fields.
- `ctx.db.get(id)` and queries return documents with `_id` and `_creationTime`, and IDs supplied to database operations are authenticated for their table/namespace.

At schema construction, table and index names must be identifiers. Field names must be safe printable names: non-empty, at most the protocol field-name limit, not `$`-prefixed, and not `__proto__`, `constructor`, or `prototype`. Indexes need a non-empty, unique field list and names unique within their table. A field path may be a top-level field or a dotted path through a constrained `v.object`; optional/defaulted object wrappers are unwrapped for that purpose. Arrays, IDs, primitives, records, `v.any()`, and unions are leaves, so paths such as `tags.value` or `state.name` are not index-compatible.

Run `pbvex codegen` after schema changes. It produces the document, insert, ID, and index-name types used by `ctx.db`; schema deployment and activation validate the serialized field descriptors as well. The [schema and database guide](./schema-and-database.md) covers CRUD, queries, and schema activation behavior.

## PBVex wire values versus ordinary JSON

PBVex transport is JSON-shaped but is not simply `JSON.stringify` applied to arbitrary JavaScript. Its value codec accepts these application values:

```ts
type PbvexValue =
  | null | boolean | number | string | bigint | ArrayBuffer | Id
  | PbvexValue[]
  | Record<string, undefined | PbvexValue>;
```

It encodes `bigint` as an eight-byte `$integer` object and `ArrayBuffer` as a base64 `$bytes` object. Those are PBVex wire tags, not user-defined JSON conventions. Finite numbers, strings, booleans, null, arrays, plain objects, and opaque IDs use their JSON-shaped representation.

| JavaScript value or condition | PBVex codec behavior |
| --- | --- |
| `undefined` at the root or in an array | Rejected. |
| `undefined` object property | Omitted during value encoding. Prefer omission for optional fields. |
| `undefined` function return | Encoded as `null`. A declared non-null return validator can still reject it. |
| `bigint` | Accepted only in signed int64 range and encoded as `$integer`; ordinary `JSON.stringify` would throw. |
| `ArrayBuffer` | Encoded as `$bytes`; ordinary JSON does not preserve its bytes. |
| `Date`, `Error`, `Map`, `Set`, typed-array object, class instance | Rejected as unsupported object prototypes. Convert to an explicit supported value, such as an ISO string, a plain error object, an array, or an `ArrayBuffer`. |
| Function, symbol | Rejected. |
| `NaN`, `Infinity`, `-Infinity` | Rejected. PBVex does not use JSON's `null` conversion for them. |
| Cyclic array/object graph | Rejected. |
| Plain object keys | Must satisfy PBVex safe-field-name rules; invalid/reserved keys are rejected. |

`canonicalJson` is stricter still: it operates on already-JSON values, sorts every object key by code-unit order, emits no insignificant whitespace, and rejects `undefined`, bigint, functions, symbols, non-finite numbers, cycles, and non-plain objects. It is used where PBVex needs stable bytes, such as hashes and client argument-size accounting. Two objects with the same content but different insertion order produce the same canonical JSON:

```ts
canonicalJson({ b: 2, a: 1 }); // '{"a":1,"b":2}'
```

Do not put `Date` or `Error` directly in an args object even though ordinary `JSON.stringify` may call `toJSON` or serialize some of their properties. Encode the application meaning yourself:

```ts
const safeArgs = {
  occurredAt: new Date().toISOString(),
  failure: { name: error.name, message: error.message },
};
```

## Timing, activation, and limits

These are separate stages; success at one does not remove checks at the next:

1. **Authoring and TypeScript.** Validator generic types infer handler args, returns, schema shapes, and generated API references. This catches many mistakes during type checking but does not validate values from an untyped caller.
2. **Build/code generation.** The bundler calls `toJSON()` for declared validators and code generation derives reference types. A closure-based `v.delayed` cannot pass this stage for a deployed descriptor.
3. **Deployment and activation.** The protocol and backend validate the manifest descriptor graph, schema/index definitions, default values, component definitions, and component mount args. Failed activation leaves the prior deployment active.
4. **Invocation and transport.** `@pbvex/client`, `@pbvex/react`, and `@pbvex/svelte` encode calls using the PBVex codec; the server validates public arguments, nested-call arguments, scheduler arguments, and returned values at their respective boundaries. Realtime subscriptions use the same bounded argument/return transport.
5. **Database and components.** Inserts, patches, replacements, reads, and schema migrations use the schema descriptors; component mount arguments are validated in their component namespace. Component IDs and table IDs are namespace-bound, so a root ID is not interchangeable with a component-mounted ID.

The source-guaranteed codec/validator budgets are maximum depth 128, at most 16,384 accounting nodes, a 4 MiB internal wire-byte budget, and at most 1,024 array entries or object fields in the backend validator. A union has at most 64 deployable branches. Function-argument and return byte limits are deployment configuration: they default to 1 MiB and may be lowered or raised only up to the 16 MiB protocol ceiling. See [limits](./limits.md) for the full, current configuration matrix. Do not treat limits not documented there as a stable application contract.

Scheduled calls accept the generated mutation/action reference and its typed args; scheduler time input (`Date | number`) is a scheduler API parameter, not a PBVex value validator type. Storage APIs use their separate opaque `StorageId` type. Neither changes the set of values accepted by `v`.

## Patterns and common failures

### Prefer a discriminated union to an open `any`

```ts
const event = v.union(
  v.object({ kind: v.literal('created'), title: v.string() }),
  v.object({ kind: v.literal('completed'), completedAt: v.number() }),
);
```

This gives generated clients and handlers useful narrowing. `v.any()` preserves no type safety and still only admits wire-safe values.

### Express nullability and absence separately

```ts
const profile = v.object({
  // Can be missing, null, or a string:
  nickname: v.optional(v.union(v.null(), v.string())),
});
```

Use this instead of assuming `v.optional(v.string())` accepts `null`, or assuming `v.null()` allows a missing field.

### Model a dynamic dictionary with `record`

```ts
const votes = v.record(v.string(), v.int64());
// { "user_123": 1n, "user_456": -1n }
```

Do not use `v.object({})` for a dynamic map: it is a constrained empty shape. Do not use numeric record keys; object keys are strings and deployable record keys are deliberately limited.

### Return only declared, serializable data

```ts
returns: v.object({
  data: v.array(v.string()),
  generatedAt: v.string(),
}),
handler: async () => ({
  data: ['ok'],
  generatedAt: new Date().toISOString(),
})
```

Returning `new Date()`, `new Error()`, a class instance, `undefined` for a required field, a cyclic structure, or a non-finite number fails encoding and/or return validation. Convert these values before returning.

### Diagnose a table-ID mismatch

If `v.id('tasks')` rejects a value, it may be an ID for another table or component namespace even if it looks like a string ID. Pass the original typed ID through your API, or call `ctx.db.normalizeId('tasks', raw)` when you truly start with an untyped string. Never repair it by casting with `as Id<'tasks'>`; a cast changes no runtime authentication.

### Keep defaults and patches intentional

Use `v.defaulted` for creation-time defaults that handlers should always see. A patch that omits the field leaves its stored value alone; it does not reapply the default. To reset a value, patch it with an explicit child-valid value (or with `null` only if the schema allows `v.null()`).

For related operational and client details, see [backend functions](./functions.md), [schema and database](./schema-and-database.md), [components](./components.md), and the [client call guide](./client/queries-mutations-actions.md).
