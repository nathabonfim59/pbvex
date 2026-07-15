# @pbvex/svelte

Svelte 5 rune utilities for PBVex, backed by `@pbvex/client`.

## Install

```bash
npm install @pbvex/svelte @pbvex/client
```

Svelte 5 is a peer dependency.

## Usage

```svelte
<script lang="ts">
  import { Client } from '@pbvex/client';
  import { setClient, useMutation, useQuery } from '@pbvex/svelte';
  import { api } from './pbvex/_generated/api.js';

  setClient(new Client('/'));
  const sum = useQuery(api.math.add, { a: 1, b: 2 });
  const add = useMutation(api.math.add);

  async function handleClick() {
    await add({ a: 3, b: 4 });
  }
</script>

{#if sum.isLoading}
  <p>Loading…</p>
{:else if sum.error}
  <p>Error: {sum.error.message}</p>
{:else}
  <p>Result: {JSON.stringify(sum.data)}</p>
  <button onclick={handleClick}>Add</button>
{/if}
```

## API

### `useQuery(ref, args?)`

Returns rune-backed `QueryState<ReturnOf<Ref>>`: `{ data, error, isLoading }`. For reactive arguments, pass a getter: `useQuery(api.messages.list, () => ({ channel }))`. The watch automatically changes with the getter and is cleaned up when the component is destroyed.

### `useMutation(ref)` / `useAction(ref)`

Return typed async callables.

### `setClient(client)` / `getClient()`

Svelte context helpers. Call `setClient` during component initialization; `getClient` returns the closest client or throws.

### `skip`

Return the literal `skip` from an args getter to disable a query without opening a watch.

### Compatibility aliases

`useQueryResult`, `useSubscription`, and `createQuery` remain deprecated aliases of `useQuery`. They now return rune-backed query state rather than Svelte stores.

## Breaking change from the store API

Svelte 5 is required. Query results are read as `query.data`, `query.error`, and `query.isLoading`; legacy `$query` auto-subscription and `Readable` arguments are intentionally unsupported. Use an args getter for reactive inputs.

## License

MIT
