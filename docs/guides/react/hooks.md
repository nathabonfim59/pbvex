# Hooks

`@pbvex/react` exports five hooks. All hooks require a `PBVexProvider` ancestor.

## useQuery

Returns the latest query value or `undefined` while loading.

```tsx
import { useQuery } from '@pbvex/react';
import { api } from '../pbvex/_generated/api.js';

function MessageList({ channel }: { channel: string }) {
  const messages = useQuery(api.messages.list, { channel });

  if (messages === undefined) return <p>Loading…</p>;

  return (
    <ul>
      {messages.map((message) => (
        <li key={message.id}>{message.body}</li>
      ))}
    </ul>
  );
}
```

`useQuery` throws query errors so they can be caught by a React error boundary. For explicit error handling, use `useQueryResult`.

## useQueryResult

Returns the full `QueryResult<T>`:

```tsx
import { useQueryResult } from '@pbvex/react';
import { api } from '../pbvex/_generated/api.js';

function MessageList({ channel }: { channel: string }) {
  const { data, error, isLoading } = useQueryResult(api.messages.list, { channel });

  if (isLoading) return <p>Loading…</p>;
  if (error) return <p>Error: {error.message}</p>;

  return (
    <ul>
      {data!.map((message) => (
        <li key={message.id}>{message.body}</li>
      ))}
    </ul>
  );
}
```

## useMutation

Returns a stable callable that invokes `client.mutation`:

```tsx
import { useMutation } from '@pbvex/react';
import { api } from '../pbvex/_generated/api.js';

function SendForm({ channel }: { channel: string }) {
  const send = useMutation(api.messages.send);

  async function handleSubmit(formData: FormData) {
    const body = formData.get('body') as string;
    await send({ body, channel });
  }

  return (
    <form action={handleSubmit}>
      <input name="body" />
      <button type="submit">Send</button>
    </form>
  );
}
```

The callable is memoized and does not change between renders.

## useAction

Identical to `useMutation` but invokes `client.action`:

```tsx
import { useAction } from '@pbvex/react';
import { api } from '../pbvex/_generated/api.js';

function NotifyButton({ messageId }: { messageId: string }) {
  const notify = useAction(api.messages.notify);

  return <button onClick={() => notify({ messageId })}>Notify</button>;
}
```

## useSubscription

Backwards-compatible alias for `useQuery`.

```tsx
const messages = useSubscription(api.messages.list, { channel });
```

## usePBVexClient

Access the client directly:

```tsx
import { usePBVexClient } from '@pbvex/react';

function Status() {
  const client = usePBVexClient();
  return <p>Connection: {client.connectionState}</p>;
}
```

## Type signatures

```ts
function useQuery<Ref extends FunctionReference<'query', any, any, 'public'>>(
  ref: Ref,
  ...args: UseArgs<Ref>
): ReturnOf<Ref> | undefined;

function useQueryResult<Ref extends FunctionReference<'query', any, any, 'public'>>(
  ref: Ref,
  ...args: UseArgs<Ref>
): QueryResult<ReturnOf<Ref>>;

function useMutation<Ref extends FunctionReference<'mutation', any, any>>(
  ref: Ref,
): UseCallable<Ref>;

function useAction<Ref extends FunctionReference<'action', any, any>>(
  ref: Ref,
): UseCallable<Ref>;

function useSubscription<Ref extends FunctionReference<'query', any, any, 'public'>>(
  ref: Ref,
  ...args: UseArgs<Ref>
): ReturnOf<Ref> | undefined;

function usePBVexClient(): Client;
```

## Args and skip

`useQuery` and `useQueryResult` accept the args value or the literal string `'skip'`. Passing `'skip'` avoids subscribing.

```tsx
const userId = useUserId();
const profile = useQuery(api.users.get, userId ? { userId } : 'skip');
```

Required args must be supplied; `void`, `undefined`, empty object, or all-optional args may be omitted.
