# Testing rune queries

`useQuery` must run during Svelte component initialization because it uses `$effect`. Test it by mounting a small Svelte component, then inspect its exported query state.

```svelte
<!-- QueryHarness.svelte -->
<script lang="ts">
  import { useQuery } from '@pbvex/svelte';
  import { api } from './pbvex/_generated/api.js';

  let { client, initialArgs } = $props();
  let args = $state(initialArgs);
  const query = useQuery(api.math.add, () => args, client);

  export { query };
  export function setArgs(next) {
    args = next;
  }
</script>
```

```ts
import { expect, it } from 'vitest';
import { flushSync, mount, tick, unmount } from 'svelte';
import QueryHarness from './QueryHarness.svelte';

it('updates state, follows args, and cleans up', async () => {
  const client = new MockClient();
  const harness = mount(QueryHarness, {
    target: document.body,
    props: { client, initialArgs: { a: 1, b: 2 } },
  });
  await tick();

  expect(harness.query).toMatchObject({ data: undefined, error: null, isLoading: true });
  client.push({ data: { sum: 3 }, error: null, isLoading: false });
  expect(harness.query.data).toEqual({ sum: 3 });

  flushSync(() => harness.setArgs({ a: 2, b: 3 }));
  expect(client.watchCalls.at(-1)?.args).toEqual({ a: 2, b: 3 });

  unmount(harness);
  expect(client.subscriptions.size).toBe(0);
});
```

The package test suite uses this pattern with a `MockClient` that records each `watch` call and invokes its callbacks. Do not test rune query state by subscribing with `get` from `svelte/store`: queries are no longer stores.
