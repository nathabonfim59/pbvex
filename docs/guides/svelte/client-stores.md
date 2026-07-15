# Client and query lifecycle

`@pbvex/svelte` uses Svelte context to provide a `Client` to rune utilities.

## setClient and getClient

```svelte
<script lang="ts">
  import { Client } from '@pbvex/client';
  import { setClient } from '@pbvex/svelte';

  let { children } = $props();
  const client = new Client('http://localhost:8090');
  setClient(client);
</script>

{@render children()}
```

Use this in a layout with `let { children } = $props()` when it has child content. Child components can call `getClient()` during initialization. It throws if no client exists in context.

## Explicit client

Pass a client as the final argument when testing a component or when context is unavailable:

```ts
const sum = useQuery(api.math.add, { a: 1, b: 2 }, client);
```

## Lifecycle

`useQuery` is component-scoped. It starts a `client.watch` from a `$effect`, updates its rune-backed state as data arrives, and calls the watch unsubscribe function whenever arguments change or the component is destroyed.

```svelte
<script lang="ts">
  import { useQuery } from '@pbvex/svelte';
  import { api } from './pbvex/_generated/api.js';

  const sum = useQuery(api.math.add, { a: 1, b: 2 });
</script>

{#if sum.isLoading}
  <p>Loading…</p>
{:else if sum.error}
  <p>Error: {sum.error.message}</p>
{:else}
  <p>Sum: {sum.data?.sum}</p>
{/if}
```

For changing arguments, use a getter: `useQuery(api.messages.list, () => ({ channel }))`. The getter is tracked by Svelte, so the query follows rune state and `$props()` values without stores or manual unsubscription.

## Empty args

References with `void`, `undefined`, or `{}` args do not need an argument object:

```svelte
<script lang="ts">
  import { useQuery } from '@pbvex/svelte';
  import { api } from './pbvex/_generated/api.js';

  const status = useQuery(api.health.ping);
</script>

{#if status.data}
  <p>OK: {status.data}</p>
{/if}
```
