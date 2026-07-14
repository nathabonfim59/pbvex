# Public functions

## useQuery

Returns a `Readable<ReturnOf<Ref> | undefined>`.

```svelte
<script lang="ts">
  import { useQuery } from '@pbvex/sdk-svelte';
  import { api } from './pbvex/_generated/api.js';

  export let channel: string;

  const messages = useQuery(api.messages.list, { channel });
</script>

{#if $messages === undefined}
  <p>Loading…</p>
{:else}
  <ul>
    {#each $messages as message (message.id)}
      <li>{message.body}</li>
    {/each}
  </ul>
{/if}
```

## useQueryResult

Returns a `Readable<QueryResult<ReturnOf<Ref>>>`:

```svelte
<script lang="ts">
  import { useQueryResult } from '@pbvex/sdk-svelte';
  import { api } from './pbvex/_generated/api.js';

  export let channel: string;

  const result = useQueryResult(api.messages.list, { channel });
</script>

{#if $result.isLoading}
  <p>Loading…</p>
{:else if $result.error}
  <p>Error: {$result.error.message}</p>
{:else}
  <ul>
    {#each $result.data as message (message.id)}
      <li>{message.body}</li>
    {/each}
  </ul>
{/if}
```

## useSubscription and createQuery

`useSubscription` is an alias for `useQueryResult`. `createQuery` is an alias for `useQuery`.

```svelte
<script lang="ts">
  import { useSubscription, createQuery } from '@pbvex/sdk-svelte';

  const result = useSubscription(api.messages.list, { channel: 'general' });
  const sum = createQuery(api.math.add, { a: 1, b: 2 });
</script>
```

## useMutation

Returns a typed callable:

```svelte
<script lang="ts">
  import { useMutation } from '@pbvex/sdk-svelte';
  import { api } from './pbvex/_generated/api.js';

  const send = useMutation(api.messages.send);

  let body = '';

  async function handleSubmit() {
    await send({ body, channel: 'general' });
    body = '';
  }
</script>

<form on:submit|preventDefault={handleSubmit}>
  <input bind:value={body} />
  <button type="submit">Send</button>
</form>
```

## useAction

Identical to `useMutation` but invokes `client.action`:

```svelte
<script lang="ts">
  import { useAction } from '@pbvex/sdk-svelte';
  import { api } from './pbvex/_generated/api.js';

  export let messageId: string;
  const notify = useAction(api.messages.notify);
</script>

<button on:click={() => notify({ messageId })}>Notify</button>
```

## skip

The exported `skip` string literal skips a query and sets `isLoading` to `false`.

```svelte
<script lang="ts">
  import { useQueryResult, skip } from '@pbvex/sdk-svelte';
  import { writable } from 'svelte/store';
  import { api } from './pbvex/_generated/api.js';

  export let userId: string | undefined;

  const args = writable<typeof skip | { userId: string }>(
    userId ? { userId } : skip,
  );

  const profile = useQueryResult(api.users.get, args);
</script>

{#if $profile.isLoading}
  <p>Loading…</p>
{:else if $profile.error}
  <p>Error: {$profile.error.message}</p>
{:else if $profile.data}
  <p>{$profile.data.name}</p>
{:else}
  <p>No user selected.</p>
{/if}
```

## Type signatures

```ts
function useQuery<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  ...args: UseQueryArgs<ArgsOf<Ref>>
): Readable<ReturnOf<Ref> | undefined>;

function useQueryResult<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  ...args: UseQueryArgs<ArgsOf<Ref>>
): Readable<QueryResult<ReturnOf<Ref>>>;

function useMutation<Ref extends FunctionReference<'mutation', any, any>>(
  ref: Ref,
  client?: Client,
): MutationCallable<ArgsOf<Ref>, ReturnOf<Ref>>;

function useAction<Ref extends FunctionReference<'action', any, any>>(
  ref: Ref,
  client?: Client,
): MutationCallable<ArgsOf<Ref>, ReturnOf<Ref>>;
```
