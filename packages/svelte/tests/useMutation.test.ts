import { describe, it, expect, beforeEach } from 'vitest';
import { useMutation, useAction } from '../src/useMutation.js';
import { MockClient, emptyRef } from './mockClient.js';
import { type FunctionReference } from '@pbvex/client';

const addRef = {
  _path: 'math:add',
  _type: 'mutation',
} as FunctionReference<'mutation', { a: number; b: number }, { sum: number }, 'public'>;

const greetRef = {
  _path: 'greet:hello',
  _type: 'action',
} as FunctionReference<'action', { name: string }, string, 'public'>;

const emptyMutRef = {
  _path: 'util:empty',
  _type: 'mutation',
} as FunctionReference<'mutation', void, number, 'public'>;

describe('useMutation and useAction', () => {
  let client: MockClient;

  beforeEach(() => {
    client = new MockClient();
    client.mutation.mockResolvedValue({ sum: 3 });
    client.action.mockResolvedValue('hello, world');
  });

  it('useMutation returns a stable callable that invokes client.mutation', async () => {
    const mutate = useMutation(addRef, client);
    const result = await mutate({ a: 1, b: 2 });
    expect(client.mutation).toHaveBeenCalledWith(addRef, { a: 1, b: 2 });
    expect(result).toEqual({ sum: 3 });
  });

  it('useAction returns a stable callable that invokes client.action', async () => {
    const act = useAction(greetRef, client);
    const result = await act({ name: 'world' });
    expect(client.action).toHaveBeenCalledWith(greetRef, { name: 'world' });
    expect(result).toEqual('hello, world');
  });

  it('empty args mutation and action callables can be invoked without args', async () => {
    client.mutation.mockResolvedValue(42);
    const mutate = useMutation(emptyMutRef, client);
    const result = await mutate();
    expect(client.mutation).toHaveBeenCalledWith(emptyMutRef, undefined);
    expect(result).toEqual(42);
  });

  it('mutation and action callables are typed functions', () => {
    const mutate = useMutation(addRef, client);
    const act = useAction(greetRef, client);
    expect(typeof mutate).toBe('function');
    expect(typeof act).toBe('function');
  });
});
