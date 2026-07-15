# Patterns

## Loading and error states

`useQuery` returns `undefined` while loading and on error. Errors are thrown as React errors and can be caught by an error boundary:

```tsx
import { useQuery } from '@pbvex/react';
import { ErrorBoundary } from 'react-error-boundary';

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

function SafeMessageList({ channel }: { channel: string }) {
  return (
    <ErrorBoundary fallback={<p>Could not load messages.</p>}>
      <MessageList channel={channel} />
    </ErrorBoundary>
  );
}
```

For explicit loading and error UI, use `useQueryResult`.

## Stable args

The hook stores args in a `QueryStore` keyed by canonical JSON. Object key order does not matter, but the values must be stable across renders to avoid unnecessary resubscription.

```tsx
// Stable
const args = useMemo(() => ({ channel }), [channel]);
const messages = useQuery(api.messages.list, args);

// Also stable: inline object with same values resubscribes only when channel changes
const messages = useQuery(api.messages.list, { channel });
```

The `QueryStore` dedupes by canonical wire value, so `BigInt`, `ArrayBuffer`, and object ordering do not cause extra watches if the values are semantically equal.

## Conditional queries

Pass `'skip'` to avoid subscribing until a condition is met:

```tsx
const userId = useUserId();
const profile = useQuery(api.users.get, userId ? { userId } : 'skip');
```

When unskipped, the store resubscribes. When skipped again, it unsubscribes.

## Cleanup

Unsubscribing happens automatically when the component unmounts. `useSyncExternalStore` handles StrictMode double subscription and cleanup. The returned `Unsubscribe` function is stored in the `QueryStore` and is called when the last listener is removed.

## SSR limitations

`useQuery` uses `useSyncExternalStore` with `getServerSnapshot` that returns the initial loading state. During `renderToString`, the store does not subscribe to the client, so `useQuery` always returns `undefined` on the server. The same component will load on the client.

```tsx
// Server: messages === undefined
// Client: messages === undefined initially, then updates
const messages = useQuery(api.messages.list, { channel: 'general' });
```

## Mutations with optimistic updates

`@pbvex/react` does not include a built-in optimistic-update layer. The client does not expose local cache mutation methods. For optimistic UI, maintain local state around the mutation call:

```tsx
import { useMutation, useQuery } from '@pbvex/react';
import { useState } from 'react';

function LikeButton({ postId }: { postId: string }) {
  const sendLike = useMutation(api.posts.like);
  const [optimisticLiked, setOptimisticLiked] = useState(false);

  async function handleClick() {
    setOptimisticLiked(true);
    try {
      await sendLike({ postId });
    } catch {
      setOptimisticLiked(false);
    }
  }

  return (
    <button onClick={handleClick}>
      {optimisticLiked ? 'Liked' : 'Like'}
    </button>
  );
}
```

The realtime query subscription will reflect the authoritative backend state after the mutation completes.
