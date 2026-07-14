# @pbvex/sdk-svelte

Svelte SDK for PBVex. Provides reactive, Convex-like stores for queries, mutations, and actions backed by the browser-neutral `@pbvex/sdk-core` `Client`.

## Install

```bash
pnpm add @pbvex/sdk-svelte
```

`@pbvex/sdk-core` and `svelte` are peer dependencies.

## Usage

```svelte
<script lang="ts">
  import { Client } from '@pbvex/sdk-core';
  import { setClient, useQuery, useMutation } from '@pbvex/sdk-svelte';
  import { api } from './pbvex/_generated/api.js';

  const client = new Client('/');
  setClient(client);

  const sum = useQuery(api.math.add, { a: 1, b: 2 });
  const add = useMutation(api.math.add);

  async function handleClick() {
    const result = await add({ a: 3, b: 4 });
    console.log(result);
  }
</script>

{#if $sum === undefined}
  <p>Loading...</p>
{:else}
  <p>Result: {JSON.stringify($sum)}</p>
  <button on:click={handleClick}>Add</button>
{/if}
```

## API

### `setClient(client)` / `getClient()`

Svelte context helpers. `setClient` must be called during component initialization, and `getClient` returns the closest client set by a parent component. Explicit `client` parameters can be used outside components.

### `useQuery(ref, args?)` / `createQuery(ref, args?)`

Returns a `Readable` of the query result value (`ReturnOf<Ref> | undefined`). The value is `undefined` while loading, on error, or when skipped.

`args` can be:

- a static argument object
- a `Readable` whose value is `ArgsOf<Ref>` or `'skip'`
- the exported `skip` string literal to skip the query

### `useQueryResult(ref, args?)` / `useSubscription(ref, args?)`

Returns a `Readable<QueryResult<ReturnOf<Ref>>>` with the full `{ data, error, isLoading }` state. `useSubscription` is a compatible alias.

### `useMutation(ref)` / `useAction(ref)`

Returns stable typed async callables. `useMutation` calls `client.mutation` and `useAction` calls `client.action`.

### `skip`

The literal string `'skip'` that can be passed as args or as a `Readable` value to skip a query without making a request.

## License

MIT
