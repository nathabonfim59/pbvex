# Svelte app slice

A complete Svelte app slice with `setClient`, routing, query, mutation, and action.

## Layout

```svelte
<!-- +layout.svelte -->
<script lang="ts">
  import { Client } from '@pbvex/sdk-core';
  import { setClient } from '@pbvex/sdk-svelte';

  const client = new Client('http://localhost:8090');
  setClient(client);

  const token = localStorage.getItem('pb_token');
  if (token) client.setAuth(token);
</script>

<slot />
```

## Login

```svelte
<!-- Login.svelte -->
<script lang="ts">
  import { getClient } from '@pbvex/sdk-svelte';
  import { api } from './pbvex/_generated/api.js';

  const client = getClient();

  let email = '';
  let password = '';
  let error: string | null = null;

  async function login() {
    try {
      const { token, userId } = await client.mutation(api.auth.login, { email, password });
      localStorage.setItem('pb_token', token);
      client.setAuth(token);
      dispatch('login', { userId });
    } catch (e) {
      error = e instanceof Error ? e.message : 'Login failed';
    }
  }

  import { createEventDispatcher } from 'svelte';
  const dispatch = createEventDispatcher<{ login: { userId: string } }>();
</script>

{#if error}
  <p class="error">{error}</p>
{/if}

<form on:submit|preventDefault={login}>
  <input type="email" bind:value={email} placeholder="Email" />
  <input type="password" bind:value={password} placeholder="Password" />
  <button type="submit">Log in</button>
</form>
```

## Message list

```svelte
<!-- MessageView.svelte -->
<script lang="ts">
  import { useQuery, useMutation, useAction } from '@pbvex/sdk-svelte';
  import { api } from './pbvex/_generated/api.js';

  export let channel: string;

  const messages = useQuery(api.messages.list, { channel });
  const send = useMutation(api.messages.send);
  const notify = useAction(api.messages.notify);

  let body = '';

  async function handleSubmit() {
    const sent = await send({ body, channel });
    body = '';
    await notify({ messageId: sent.id });
  }
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

<form on:submit|preventDefault={handleSubmit}>
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

  let userId: string | null = null;

  function onLogin(event: CustomEvent<{ userId: string }>) {
    userId = event.detail.userId;
  }
</script>

<h1>PBVex Messages</h1>

{#if userId}
  <MessageView channel="general" />
{:else}
  <Login on:login={onLogin} />
{/if}
```

## Error handling

Use `useQueryResult` for explicit error states:

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
