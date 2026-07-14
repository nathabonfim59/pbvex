import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Client, type FunctionReference } from '@pbvex/sdk-core';
import { getClient, setClient } from '../src/client.js';
import { useQuery } from '../src/useQuery.js';
import { MockClient } from './mockClient.js';

vi.mock('svelte', () => ({
  getContext: vi.fn(),
  setContext: vi.fn(),
}));

import { getContext, setContext } from 'svelte';

describe('client context', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('setClient stores the client in Svelte context', () => {
    const client = new MockClient();
    const result = setClient(client);
    expect(result).toBe(client);
    expect(setContext).toHaveBeenCalledTimes(1);
  });

  it('getClient returns the client from Svelte context', () => {
    const client = new MockClient();
    (getContext as ReturnType<typeof vi.fn>).mockReturnValue(client);
    expect(getClient()).toBe(client);
    expect(getContext).toHaveBeenCalledTimes(1);
  });

  it('getClient throws when no client is in context', () => {
    (getContext as ReturnType<typeof vi.fn>).mockReturnValue(undefined);
    expect(() => getClient()).toThrow('No PBVex client found in Svelte context');
  });

  it('useQuery can use a client from context without explicit client', () => {
    const client = new MockClient();
    (getContext as ReturnType<typeof vi.fn>).mockReturnValue(client);
    const ref = { _path: 'test:get', _type: 'query' } as FunctionReference<'query', {}, string, 'public'>;
    const store = useQuery(ref);
    const values: (string | undefined)[] = [];
    const unsubscribe = store.subscribe((v) => values.push(v));
    expect(client.watch).toHaveBeenCalledWith(ref, undefined, expect.any(Object));
    unsubscribe();
  });
});
