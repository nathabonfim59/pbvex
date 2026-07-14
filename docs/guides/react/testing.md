# Testing React hooks

`@pbvex/sdk-react` hooks are tested with `renderHook` and `PBVexProvider`. The core package tests use a mock `RealtimeTransport`.

## MockRealtimeTransport

A mock transport implements the `RealtimeTransport` interface and emits updates synchronously:

```ts
import type { ConnectionState, QueryResult, RealtimeTransport, Unsubscribe, WatchOptions } from '@pbvex/sdk-core';

class MockRealtimeTransport implements RealtimeTransport {
  connectionState: ConnectionState = 'disconnected';
  watchCount = 0;
  unsubscribeCount = 0;
  watchCalls: { path: string; args: unknown; options: WatchOptions<unknown> }[] = [];
  private watchers = new Set<WatchOptions<unknown>>();

  watch<Args, Return>(path: string, args: Args, options: WatchOptions<Return>): Unsubscribe {
    this.watchCount += 1;
    this.watchers.add(options as WatchOptions<unknown>);
    this.connectionState = 'connected';
    options.onUpdate({ data: undefined, error: null, isLoading: true } as QueryResult<Return>);

    const unsubscribe = () => {
      this.watchers.delete(options as WatchOptions<unknown>);
      this.unsubscribeCount += 1;
      if (this.watchers.size === 0) this.connectionState = 'disconnected';
    };

    this.watchCalls.push({ path, args, options: options as WatchOptions<unknown> });
    return unsubscribe;
  }

  trigger(data: unknown): void {
    for (const watcher of this.watchers) {
      watcher.onUpdate({ data, error: null, isLoading: false } as QueryResult<unknown>);
    }
  }

  triggerError(error: Error): void {
    for (const watcher of this.watchers) {
      watcher.onError?.(error);
    }
  }

  close(): void {
    this.watchers.clear();
    this.connectionState = 'disconnected';
  }
}
```

## renderHook wrapper

```tsx
import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { Client } from '@pbvex/sdk-core';
import { PBVexProvider, useQuery } from '@pbvex/sdk-react';
import type { FunctionReference } from '@pbvex/sdk-core';

function createClient(transport: MockRealtimeTransport): Client {
  return new Client('http://localhost:8090', {
    fetch: vi.fn() as unknown as typeof fetch,
    realtimeTransport: transport,
  });
}

function createWrapper(client: Client) {
  return function Wrapper({ children }: { children?: React.ReactNode }) {
    return <PBVexProvider client={client}>{children}</PBVexProvider>;
  };
}

const queryRef = { _path: 'tasks:list', _type: 'query' } as FunctionReference<
  'query',
  { userId: string },
  { tasks: string[] },
  'public'
>;

describe('useQuery', () => {
  it('returns data from the transport', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);

    const { result } = renderHook(
      () => useQuery(queryRef, { userId: 'u1' }),
      { wrapper: createWrapper(client) },
    );

    expect(result.current).toBeUndefined();
    expect(transport.watchCount).toBe(1);

    act(() => transport.trigger({ tasks: ['a', 'b'] }));

    expect(result.current).toEqual({ tasks: ['a', 'b'] });
  });
});
```

## StrictMode

The `QueryStore` handles `StrictMode` double subscription by tracking listener count and unsubscribing only when the last listener leaves. Tests should verify that `unsubscribeCount` equals `watchCount` after unmount.

## Testing mutations

```tsx
import { renderHook, act } from '@testing-library/react';
import { useMutation } from '@pbvex/sdk-react';

const mutationRef = { _path: 'tasks:update', _type: 'mutation' } as FunctionReference<
  'mutation',
  { id: string; done: boolean },
  { id: string },
  'public'
>;

describe('useMutation', () => {
  it('calls client.mutation', async () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const spy = vi.spyOn(client, 'mutation').mockResolvedValue({ id: 't1' });

    const { result } = renderHook(
      () => useMutation(mutationRef),
      { wrapper: createWrapper(client) },
    );

    const first = result.current;
    act(() => {});
    expect(result.current).toBe(first);

    const returned = await result.current({ id: 't1', done: true });
    expect(returned).toEqual({ id: 't1' });
    expect(spy).toHaveBeenCalledWith(mutationRef, { id: 't1', done: true });
  });
});
```

## Provider test

```tsx
import { renderHook } from '@testing-library/react';
import { usePBVexClient } from '@pbvex/sdk-react';

describe('PBVexProvider', () => {
  it('returns the client inside a provider', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result } = renderHook(() => usePBVexClient(), {
      wrapper: createWrapper(client),
    });
    expect(result.current).toBe(client);
  });
});
```
