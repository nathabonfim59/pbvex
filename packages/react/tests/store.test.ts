import { describe, it, expect, vi } from 'vitest';
import type { QueryResult } from '@pbvex/client';
import { QueryStore } from '../src/store.js';
import { createClient, MockRealtimeTransport } from './helpers.js';

describe('QueryStore', () => {
  it('ignores stale callbacks from a torn-down watch after resubscribe', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const store = new QueryStore<{ userId: string }, { tasks: string[] }>(
      client,
      'tasks:list',
      { userId: 'u1' },
    );

    const listener = vi.fn();

    const unsubscribe1 = store.subscribe(listener);
    expect(transport.watchCount).toBe(1);
    expect(transport.watchCalls[0].args).toEqual({ userId: 'u1' });

    // Bring the first watch to a loaded state.
    transport.watchCalls[0].options.onUpdate({
      data: { tasks: ['first'] },
      error: null,
      isLoading: false,
    } as QueryResult<unknown>);
    expect(store.getSnapshot()).toEqual({
      data: { tasks: ['first'] },
      error: null,
      isLoading: false,
    });

    // Simulate StrictMode-style unsubscribe/resubscribe on the same store.
    unsubscribe1();
    expect(transport.unsubscribeCount).toBe(1);

    const unsubscribe2 = store.subscribe(listener);
    expect(transport.watchCount).toBe(2);
    expect(transport.watchCalls[1].args).toEqual({ userId: 'u1' });

    // The new watch starts with a loading state (the mock emits it synchronously).
    expect(store.getSnapshot()).toEqual({
      data: undefined,
      error: null,
      isLoading: true,
    });

    // A stale callback from the first watch must not mutate the current snapshot.
    transport.watchCalls[0].options.onUpdate({
      data: { tasks: ['stale'] },
      error: null,
      isLoading: false,
    } as QueryResult<unknown>);
    expect(store.getSnapshot()).toEqual({
      data: undefined,
      error: null,
      isLoading: true,
    });

    // A stale error from the first watch must also be ignored.
    transport.watchCalls[0].options.onError?.(new Error('stale error'));
    expect(store.getSnapshot()).toEqual({
      data: undefined,
      error: null,
      isLoading: true,
    });

    // The current watch can still update the snapshot.
    transport.watchCalls[1].options.onUpdate({
      data: { tasks: ['second'] },
      error: null,
      isLoading: false,
    } as QueryResult<unknown>);
    expect(store.getSnapshot()).toEqual({
      data: { tasks: ['second'] },
      error: null,
      isLoading: false,
    });

    // After the current snapshot is set, stale callbacks cannot overwrite it.
    transport.watchCalls[0].options.onUpdate({
      data: { tasks: ['stale-again'] },
      error: null,
      isLoading: false,
    } as QueryResult<unknown>);
    expect(store.getSnapshot()).toEqual({
      data: { tasks: ['second'] },
      error: null,
      isLoading: false,
    });

    unsubscribe2();
  });

  it('surfaces transport errors via onError as QueryResult errors', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const store = new QueryStore<{ userId: string }, { tasks: string[] }>(
      client,
      'tasks:list',
      { userId: 'u1' },
    );

    const listener = vi.fn();
    const unsubscribe = store.subscribe(listener);

    transport.watchCalls[0].options.onError?.(new Error('stream failed'));
    expect(store.getSnapshot()).toEqual({
      data: undefined,
      error: new Error('stream failed'),
      isLoading: false,
    });

    unsubscribe();
  });
});
