# Testing Svelte stores

`@pbvex/sdk-svelte` returns Svelte `Readable` stores. Tests subscribe with `get` or a custom subscriber and assert emitted values.

## Mock client

The package tests extend `Client` and mock `watch`, `mutation`, and `action`:

```ts
import { vi } from 'vitest';
import { Client, type FunctionReference, type QueryResult, type Unsubscribe } from '@pbvex/sdk-core';
import { encodeValue, canonicalJson, type PbvexValue } from '@pbvex/protocol';

function defaultEncodeArgs(args: unknown): ReturnType<typeof encodeValue> {
  return args === undefined
    ? ({} as ReturnType<typeof encodeValue>)
    : (encodeValue(args as PbvexValue) as ReturnType<typeof encodeValue>);
}

function argsKey(args: unknown): string {
  return canonicalJson(defaultEncodeArgs(args));
}

export interface WatchCall {
  path: string;
  args: unknown;
  options: {
    onUpdate: (result: QueryResult<unknown>) => void;
    onError?: (error: Error) => void;
  };
}

export class MockClient extends Client {
  watch = vi.fn(this._watch.bind(this));
  mutation = vi.fn(this._mutation.bind(this));
  action = vi.fn(this._action.bind(this));

  watchCalls: WatchCall[] = [];
  subscriptions = new Map<string, { calls: WatchCall[]; latest: QueryResult<unknown> }>();

  constructor() {
    super('http://localhost:8090', { fetch: vi.fn() as unknown as typeof fetch });
  }

  private _watch(ref: unknown, args?: unknown, options?: unknown): Unsubscribe {
    const path = typeof ref === 'string' ? ref : (ref as { _path: string })._path;
    const key = `${path}:${argsKey(args)}`;
    const call = { path, args, options: options as WatchCall['options'] };
    this.watchCalls.push(call);

    let subscription = this.subscriptions.get(key);
    if (!subscription) {
      subscription = { calls: [], latest: { data: undefined, error: null, isLoading: true } };
      this.subscriptions.set(key, subscription);
    }
    subscription.calls.push(call);
    call.options.onUpdate(subscription.latest);

    return () => {
      const index = subscription!.calls.indexOf(call);
      if (index !== -1) subscription!.calls.splice(index, 1);
      if (subscription!.calls.length === 0) this.subscriptions.delete(key);
    };
  }

  private _mutation(): Promise<unknown> {
    return Promise.resolve(undefined);
  }

  private _action(): Promise<unknown> {
    return Promise.resolve(undefined);
  }

  push(result: QueryResult<unknown>, ref?: string, args?: unknown): void {
    const key = ref === undefined
      ? this.subscriptions.keys().next().value
      : `${ref}:${argsKey(args)}`;
    const subscription = this.subscriptions.get(key);
    if (subscription) {
      subscription.latest = result;
      for (const call of subscription.calls) {
        call.options.onUpdate(result);
      }
    }
  }

  pushError(error: Error, ref?: string, args?: unknown): void {
    const key = ref === undefined
      ? this.subscriptions.keys().next().value
      : `${ref}:${argsKey(args)}`;
    const subscription = this.subscriptions.get(key);
    if (subscription) {
      subscription.latest = { data: undefined, error, isLoading: false };
      for (const call of subscription.calls) {
        call.options.onUpdate(subscription.latest);
        call.options.onError?.(error);
      }
    }
  }
}
```

## Testing useQuery

```ts
import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { useQuery, useQueryResult, useMutation, useAction, skip } from '@pbvex/sdk-svelte';
import { MockClient } from './mockClient.js';
import type { FunctionReference } from '@pbvex/sdk-core';

const addRef = {
  _path: 'math:add',
  _type: 'query',
} as FunctionReference<'query', { a: number; b: number }, { sum: number }, 'public'>;

const sendRef = {
  _path: 'messages:send',
  _type: 'mutation',
} as FunctionReference<'mutation', { body: string }, { id: string }, 'public'>;

describe('useQuery', () => {
  let client: MockClient;

  beforeEach(() => {
    client = new MockClient();
  });

  it('emits undefined while loading and then the value', () => {
    const store = useQuery(addRef, { a: 1, b: 2 }, client);
    const values: ({ sum: number } | undefined)[] = [];
    const unsubscribe = store.subscribe((v) => values.push(v));

    expect(values).toEqual([undefined]);
    expect(client.watchCalls.length).toBe(1);

    client.push({ data: { sum: 3 }, error: null, isLoading: false }, 'math:add', { a: 1, b: 2 });
    expect(values).toEqual([undefined, { sum: 3 }]);

    unsubscribe();
  });

  it('supports skip', () => {
    const result = useQueryResult(addRef, skip, client);
    expect(get(result).isLoading).toBe(false);
    expect(client.watchCalls.length).toBe(0);
  });

  it('calls client.mutation', async () => {
    client.mutation.mockResolvedValue({ id: 'm1' });
    const send = useMutation(sendRef, client);
    const returned = await send({ body: 'hello' });
    expect(returned).toEqual({ id: 'm1' });
    expect(client.mutation).toHaveBeenCalledWith(sendRef, { body: 'hello' });
  });
});
```

## Context tests

Svelte context is mocked in unit tests:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { getClient, setClient } from '@pbvex/sdk-svelte';

vi.mock('svelte', () => ({
  getContext: vi.fn(),
  setContext: vi.fn(),
}));

import { getContext, setContext } from 'svelte';

describe('client context', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('stores and retrieves the client', () => {
    const client = new MockClient();
    setClient(client);
    expect(setContext).toHaveBeenCalledTimes(1);
    (getContext as ReturnType<typeof vi.fn>).mockReturnValue(client);
    expect(getClient()).toBe(client);
  });
});
```
