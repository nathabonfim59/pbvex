import { describe, it, expect, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import type { FunctionReference } from '@pbvex/client';
import { useMutation, useAction, useSubscription } from '../src/index.js';
import { createClient, createWrapper, MockRealtimeTransport } from './helpers.js';

describe('useMutation', () => {
  const mutationRef = { _path: 'tasks:update', _type: 'mutation' } as FunctionReference<
    'mutation',
    { id: string; done: boolean },
    { id: string },
    'public'
  >;

  it('returns a stable callable that invokes client.mutation', async () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const mutationSpy = vi.spyOn(client, 'mutation').mockResolvedValue({ id: 't1' });

    const { result, rerender } = renderHook(() => useMutation(mutationRef), {
      wrapper: createWrapper(client),
    });

    const first = result.current;
    rerender();
    expect(result.current).toBe(first);

    const returned = await result.current({ id: 't1', done: true });
    expect(returned).toEqual({ id: 't1' });
    expect(mutationSpy).toHaveBeenCalledWith(mutationRef, { id: 't1', done: true });
  });
});

describe('useAction', () => {
  const actionRef = { _path: 'tasks:notify', _type: 'action' } as FunctionReference<
    'action',
    { taskId: string },
    { sent: boolean },
    'public'
  >;

  it('returns a stable callable that invokes client.action', async () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const actionSpy = vi.spyOn(client, 'action').mockResolvedValue({ sent: true });

    const { result } = renderHook(() => useAction(actionRef), {
      wrapper: createWrapper(client),
    });

    const returned = await result.current({ taskId: 't1' });
    expect(returned).toEqual({ sent: true });
    expect(actionSpy).toHaveBeenCalledWith(actionRef, { taskId: 't1' });
  });
});

describe('useSubscription', () => {
  const queryRef = { _path: 'tasks:list', _type: 'query' } as FunctionReference<
    'query',
    { userId: string },
    { tasks: string[] },
    'public'
  >;

  it('behaves as a useQuery alias', () => {
    const transport = new MockRealtimeTransport();
    const client = createClient(transport);
    const { result } = renderHook(() => useSubscription(queryRef, { userId: 'u1' }), {
      wrapper: createWrapper(client),
    });

    expect(result.current).toBeUndefined();

    act(() => transport.trigger({ tasks: ['a'] }));
    expect(result.current).toEqual({ tasks: ['a'] });
  });
});
