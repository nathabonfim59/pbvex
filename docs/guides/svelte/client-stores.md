# Client and context

`@pbvex/sdk-svelte` uses Svelte context to provide a `Client` to components.

## setClient and getClient

```svelte
<script lang="ts">
  import { Client } from '@pbvex/sdk-core';
  import { setClient } from '@pbvex/sdk-svelte';

  const client = new Client('http://localhost:8090');
  setClient(client);
</script>

<slot />
```

`setClient` must be called during component initialization. It returns the client.

Child components retrieve the client with `getClient`:

```svelte
<script lang="ts">
  import { getClient } from '@pbvex/sdk-svelte';

  const client = getClient();
</script>

<p>State: {client.connectionState}</p>
```

`getClient` throws if no client has been set in context.

## Explicit client

All `useQuery`, `useQueryResult`, `useMutation`, and `useAction` functions accept an optional explicit client as the last argument. This is useful outside Svelte components or in tests.

```ts
import { Client } from '@pbvex/sdk-core';
import { useQuery } from '@pbvex/sdk-svelte';
import { api } from './pbvex/_generated/api.js';

const client = new Client('http://localhost:8090');
const sum = useQuery(api.math.add, { a: 1, b: 2 }, client);
```

## Store lifecycle

`useQuery` and `useQueryResult` return Svelte `Readable` stores. The underlying `watch` is started when the first subscriber subscribes and stopped when the last subscriber unsubscribes.

```svelte
<script lang="ts">
  import { useQuery } from '@pbvex/sdk-svelte';
  import { api } from './pbvex/_generated/api.js';

  const sum = useQuery(api.math.add, { a: 1, b: 2 });
</script>

{#if $sum === undefined}
  <p>Loading…</p>
{:else}
  <p>Sum: {$sum.sum}</p>
{/if}
```

## Cleanup

The store handles cleanup automatically. When the component is destroyed and no other subscribers exist, the `watch` unsubscribe function is called. If the last subscriber leaves and a new one later joins, the subscription restarts from a loading state.

## Empty args

For references with `void`, `undefined`, or `{}` args, the client can be passed as the first argument:

```svelte
<script lang="ts">
  import { useQueryResult } from '@pbvex/sdk-svelte';
  import { api } from './pbvex/_generated/api.js';

  const status = useQueryResult(api.health.ping);
</script>

{#if $status.isLoading}
  <p>Loading…</p>
{:else if $status.error}
  <p>Error: {$status.error.message}</p>
{:else}
  <p>OK: {$status.data}</p>
{/if}
```
