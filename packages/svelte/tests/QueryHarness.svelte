<script lang="ts">
  import type { Client, FunctionReference } from '@pbvex/client';
  import { useQuery, type QueryArgs } from '@pbvex/svelte';

  type Props = {
    client: Client;
    ref: FunctionReference<'query', any, any>;
    initialArgs: QueryArgs<any>;
  };

  let { client, ref, initialArgs }: Props = $props();
  let args = $state(initialArgs);
  const query = useQuery(ref, () => args, client);

  export { query };

  export function setArgs(next: QueryArgs<any>) {
    args = next;
  }
</script>
