# Svelte app slice

A Svelte 5 app slice with context, rune-backed queries, mutations, and actions.

## Layout

```svelte
<!-- +layout.svelte -->
<script lang="ts">
  import { Client } from '@pbvex/client';
  import { setClient } from '@pbvex/svelte';

  let { children } = $props();
  const client = new Client('http://localhost:8090');
  setClient(client);

  const token = localStorage.getItem('pb_token');
  if (token) client.setAuth(token);
</script>

{@render children()}
```

## Login

```svelte
<!-- Login.svelte -->
<script lang="ts">
  import { getClient } from '@pbvex/svelte';
  import { api } from './pbvex/_generated/api.js';

  let { onlogin }: { onlogin: (userId: string) => void } = $props();
  const client = getClient();
  let email = $state('');
  let password = $state('');
  let error = $state<string | null>(null);

  async function login() {
    try {
      const { token, userId } = await client.mutation(api.auth.login, { email, password });
      localStorage.setItem('pb_token', token);
      client.setAuth(token);
      onlogin(userId);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Login failed';
    }
  }
</script>

{#if error}
  <p class="error">{error}</p>
{/if}

<form onsubmit={(event) => { event.preventDefault(); login(); }}>
  <input type="email" bind:value={email} placeholder="Email" />
  <input type="password" bind:value={password} placeholder="Password" />
  <button type="submit">Log in</button>
</form>
```

## Message list

```svelte
<!-- MessageView.svelte -->
<script lang="ts">
  import { useAction, useMutation, useQuery } from '@pbvex/svelte';
  import { api } from './pbvex/_generated/api.js';

  let { channel }: { channel: string } = $props();
  const messages = useQuery(api.messages.list, () => ({ channel }));
  const send = useMutation(api.messages.send);
  const notify = useAction(api.messages.notify);
  let body = $state('');

  async function handleSubmit() {
    const sent = await send({ body, channel });
    body = '';
    await notify({ messageId: sent.id });
  }
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

<form onsubmit={(event) => { event.preventDefault(); handleSubmit(); }}>
  <input bind:value={body} placeholder="Message" />
  <button type="submit">Send</button>
</form>
```

## App

```svelte
<!-- App.svelte -->
<script lang="ts">
  import Login from './Login.svelte';
  import MessageView from './MessageView.svelte';

  let userId = $state<string | null>(null);
</script>

<h1>PBVex Messages</h1>

{#if userId}
  <MessageView channel="general" />
{:else}
  <Login onlogin={(id) => userId = id} />
{/if}
```
