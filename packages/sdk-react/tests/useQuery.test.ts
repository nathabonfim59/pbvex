import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import type { FunctionReference, QueryResult } from '@pbvex/sdk-core';
import { useQuery, useQueryResult } from '../src/index.js';
import { createClient, createWrapper, MockRealtimeTransport } from './helpers.js';

describe('useQuery', () => {
  const queryRef = { _path: 'tasks:list', _type: 'query' } as FunctionReference<
    'query',
    { userId: string },
    { tasks: string[] },
    'public'
  >;

  it('returns undefined while loading', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result } = renderHook(() => useQuery(queryRef, { userId: 'u1' }), {
      wrapper: createWrapper(client),
    });

    expect(transport.watchCount).toBe(1);
    expect(result.current).toBeUndefined();
  });

  it('returns realtime updates', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result } = renderHook(() => useQuery(queryRef, { userId: 'u1' }), {
      wrapper: createWrapper(client),
    });

    expect(result.current).toBeUndefined();

    act(() => transport.trigger({ tasks: ['a', 'b'] }));

    expect(result.current).toEqual({ tasks: ['a', 'b'] });
  });

  it('throws query errors through React error boundaries', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result } = renderHook(() => useQuery(queryRef, { userId: 'u1' }), {
      wrapper: createWrapper(client),
    });

    expect(result.current).toBeUndefined();

    expect(() => {
      act(() => transport.triggerError(new Error('query failed')));
    }).toThrow('query failed');
  });

  it('does not subscribe when skipped', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result } = renderHook(() => useQuery(queryRef, 'skip'), {
      wrapper: createWrapper(client),
    });

    expect(transport.watchCount).toBe(0);
    expect(result.current).toBeUndefined();
  });

  it('resubscribes on semantic args changes but not object identity churn', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { rerender } = renderHook(
      ({ args }) => useQuery(queryRef, args),
      {
        initialProps: { args: { userId: 'u1' } as { userId: string } | 'skip' },
        wrapper: createWrapper(client),
      },
    );

    expect(transport.watchCount).toBe(1);

    rerender({ args: { userId: 'u1' } });
    expect(transport.watchCount).toBe(1);

    rerender({ args: { userId: 'u2' } });
    expect(transport.watchCount).toBe(2);
    expect(transport.unsubscribeCount).toBe(1);
  });

  it('does not reflect stale callbacks from a replaced store', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result, rerender } = renderHook(
      ({ args }) => useQuery(queryRef, args),
      {
        initialProps: { args: { userId: 'u1' } },
        wrapper: createWrapper(client),
      },
    );

    expect(transport.watchCount).toBe(1);
    expect(transport.watchCalls[0].args).toEqual({ userId: 'u1' });

    act(() => {
      transport.watchCalls[0].options.onUpdate({
        data: { tasks: ['first'] },
        error: null,
        isLoading: false,
      } as QueryResult<unknown>);
    });
    expect(result.current).toEqual({ tasks: ['first'] });

    rerender({ args: { userId: 'u2' } });
    expect(transport.watchCount).toBe(2);
    expect(transport.unsubscribeCount).toBe(1);
    expect(transport.watchCalls[1].args).toEqual({ userId: 'u2' });

    // A stale callback from the first store should not affect the current hook.
    act(() => {
      transport.watchCalls[0].options.onUpdate({
        data: { tasks: ['stale'] },
        error: null,
        isLoading: false,
      } as QueryResult<unknown>);
    });
    expect(result.current).toBeUndefined();

    act(() => {
      transport.watchCalls[1].options.onUpdate({
        data: { tasks: ['second'] },
        error: null,
        isLoading: false,
      } as QueryResult<unknown>);
    });
    expect(result.current).toEqual({ tasks: ['second'] });

    // Even after the current store has data, stale callbacks cannot overwrite it.
    act(() => {
      transport.watchCalls[0].options.onUpdate({
        data: { tasks: ['stale-again'] },
        error: null,
        isLoading: false,
      } as QueryResult<unknown>);
    });
    expect(result.current).toEqual({ tasks: ['second'] });
  });

  it('returns undefined after skip and re-subscribes when unskipped', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result, rerender } = renderHook(
      ({ args }) => useQuery(queryRef, args),
      {
        initialProps: { args: 'skip' as { userId: string } | 'skip' },
        wrapper: createWrapper(client),
      },
    );

    expect(transport.watchCount).toBe(0);
    expect(result.current).toBeUndefined();

    rerender({ args: { userId: 'u1' } });
    expect(transport.watchCount).toBe(1);
    expect(result.current).toBeUndefined();

    act(() => transport.trigger({ tasks: ['x'] }));
    expect(result.current).toEqual({ tasks: ['x'] });

    rerender({ args: 'skip' });
    expect(transport.watchCount).toBe(1);
    expect(transport.unsubscribeCount).toBe(1);
    expect(result.current).toBeUndefined();
  });
});

describe('useQueryResult', () => {
  const queryRef = { _path: 'tasks:list', _type: 'query' } as FunctionReference<
    'query',
    { userId: string },
    { tasks: string[] },
    'public'
  >;

  it('returns the full QueryResult including loading state', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result } = renderHook(() => useQueryResult(queryRef, { userId: 'u1' }), {
      wrapper: createWrapper(client),
    });

    expect(result.current).toEqual({ data: undefined, error: null, isLoading: true });
  });

  it('updates to reflect errors and data', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result } = renderHook(() => useQueryResult(queryRef, { userId: 'u1' }), {
      wrapper: createWrapper(client),
    });

    act(() => transport.trigger({ tasks: ['a'] }));
    expect(result.current).toEqual({ data: { tasks: ['a'] }, error: null, isLoading: false });

    act(() => transport.triggerError(new Error('bad')));
    expect(result.current).toEqual({ data: undefined, error: new Error('bad'), isLoading: false });
  });
});
