# Public functions

`@pbvex/svelte` is a Svelte 5 rune API. Query state is a reactive object, not a Svelte store: read it directly in markup without `$` auto-subscriptions.

## useQuery

`useQuery` returns `QueryState<ReturnOf<Ref>>`, with `data`, `error`, and `isLoading` fields.

```svelte
<script lang="ts">
  import { useQuery } from '@pbvex/svelte';
  import { api } from './pbvex/_generated/api.js';

  let { channel }: { channel: string } = $props();
  const messages = useQuery(api.messages.list, () => ({ channel }));
</script>

{#if messages.isLoading}
  <p>Loading…</p>
{:else if messages.error}
  <p>Error: {messages.error.message}</p>
{:else}
  <ul>
    {#each messages.data ?? [] as message (message.id)}
      <li>{message.body}</li>
    {/each}
  </ul>
{/if}
```

Pass a getter whenever arguments depend on reactive state. PBVex replaces the underlying watch when the getter's result changes and automatically cleans it up with the component.

## useMutation and useAction

Both return typed async callables. `useMutation` calls `client.mutation`; `useAction` calls `client.action`.

```svelte
<script lang="ts">
  import { useMutation, useAction } from '@pbvex/svelte';
  import { api } from './pbvex/_generated/api.js';

  const send = useMutation(api.messages.send);
  const notify = useAction(api.messages.notify);
  let body = $state('');

  async function handleSubmit() {
    const sent = await send({ body, channel: 'general' });
    body = '';
    await notify({ messageId: sent.id });
  }
</script>

<form onsubmit={(event) => { event.preventDefault(); handleSubmit(); }}>
  <input bind:value={body} />
  <button type="submit">Send</button>
</form>
```

## skip

Return `skip` from an args getter to conditionally disable a query. Its state becomes `{ data: undefined, error: null, isLoading: false }` and no watch is opened.

```svelte
<script lang="ts">
  import { skip, useQuery } from '@pbvex/svelte';
  import { api } from './pbvex/_generated/api.js';

  let { userId }: { userId?: string } = $props();
  const profile = useQuery(api.users.get, () => userId ? { userId } : skip);
</script>

{#if profile.data}
  <p>{profile.data.name}</p>
{:else if !profile.isLoading}
  <p>No user selected.</p>
{/if}
```

## Compatibility aliases

`useQueryResult`, `useSubscription`, and `createQuery` remain aliases of `useQuery` for a transition period. They return the same rune-backed `QueryState`; they no longer return `Readable` stores. New code should use `useQuery`.

## Type signatures

```ts
type QueryState<Data> = Readonly<{
  data: Data | undefined;
  error: Error | null;
  isLoading: boolean;
}>;

function useQuery<Ref extends FunctionReference<'query', any, any>>(
  ref: Ref,
  args: ArgsOf<Ref> | Skip | (() => ArgsOf<Ref> | Skip),
  client?: Client,
): QueryState<ReturnOf<Ref>>;
```
